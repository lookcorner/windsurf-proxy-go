// Package balancer provides load balancing for multiple Windsurf instances.
package balancer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"windsurf-proxy-go/internal/auth"
	"windsurf-proxy-go/internal/config"
	"windsurf-proxy-go/internal/core/grpc"
	"windsurf-proxy-go/internal/standalone"
)

const unhealthyThreshold = 3

// Instance represents a Windsurf backend that can serve requests.
type Instance struct {
	Name             string
	Host             string
	Port             int
	CSRFToken        string
	APIKey           string
	Version          string
	Client           *grpc.WindsurfGrpcClient
	Weight           int
	Healthy          bool
	ActiveConns      int
	TotalRequests    int
	LastError        string
	LastUsed         time.Time
	ConsecutiveFails int
	currentWeight    int // for smooth weighted round-robin

	// Standalone mode
	LSProcess    *standalone.LanguageServerProcess
	TokenManager *auth.TokenManager
}

// LoadBalancer manages multiple Windsurf instances and distributes requests.
type LoadBalancer struct {
	config        *config.BalancingConfig
	instances     []*Instance
	rrIndex       int
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	lsProcesses   []*standalone.LanguageServerProcess
	tokenManagers []*auth.TokenManager
}

// New creates a new load balancer.
func New(cfg *config.BalancingConfig) *LoadBalancer {
	ctx, cancel := context.WithCancel(context.Background())
	return &LoadBalancer{
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

// AddInstance adds an instance to the balancer.
func (lb *LoadBalancer) AddInstance(inst *Instance) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.instances = append(lb.instances, inst)
	log.Printf("Added instance '%s' (host=%s, port=%d)", inst.Name, inst.Host, inst.Port)
}

// AddLocalInstance adds a local Windsurf instance with auto-discovered credentials.
func (lb *LoadBalancer) AddLocalInstance(instCfg config.InstanceConfig) (*Instance, error) {
	creds, err := auth.DiscoverCredentials()
	if err != nil {
		return nil, err
	}

	client := grpc.NewWindsurfGrpcClient("127.0.0.1", creds.GRPCPort, creds.CSRFToken)

	inst := &Instance{
		Name:      instCfg.Name,
		Host:      "127.0.0.1",
		Port:      creds.GRPCPort,
		CSRFToken: creds.CSRFToken,
		APIKey:    creds.APIKey,
		Version:   creds.Version,
		Client:    client,
		Weight:    instCfg.Weight,
		Healthy:   true,
	}

	lb.AddInstance(inst)
	log.Printf("Added local instance '%s' with discovered credentials (port=%d)", instCfg.Name, creds.GRPCPort)
	return inst, nil
}

// AddManualInstance adds a manually-configured instance.
func (lb *LoadBalancer) AddManualInstance(instCfg config.InstanceConfig) (*Instance, error) {
	if instCfg.Host == "" {
		instCfg.Host = "127.0.0.1"
	}

	client := grpc.NewWindsurfGrpcClient(instCfg.Host, instCfg.GRPCPort, instCfg.CSRFToken)

	inst := &Instance{
		Name:      instCfg.Name,
		Host:      instCfg.Host,
		Port:      instCfg.GRPCPort,
		CSRFToken: instCfg.CSRFToken,
		APIKey:    instCfg.APIKey,
		Version:   "1.13.104",
		Client:    client,
		Weight:    instCfg.Weight,
		Healthy:   true,
	}

	lb.AddInstance(inst)
	log.Printf("Added manual instance '%s' (host=%s, port=%d)", instCfg.Name, instCfg.Host, instCfg.GRPCPort)
	return inst, nil
}

// AddStandaloneInstance adds a standalone instance (starts language_server process).
func (lb *LoadBalancer) AddStandaloneInstance(instCfg config.InstanceConfig) (*Instance, error) {
	apiKey := instCfg.APIKey
	apiServerURL := instCfg.APIServer
	if apiServerURL == "" {
		apiServerURL = standalone.DefaultAPIServerURL
	}

	var firebaseTokens *auth.FirebaseTokens
	var serviceAuth *auth.WindsurfServiceAuth
	var tm *auth.TokenManager

	// Priority 1: email + password login
	if instCfg.Email != "" && instCfg.Password != "" {
		log.Printf("Logging in via Firebase for '%s' (%s)...", instCfg.Name, instCfg.Email)
		sa, ft, err := auth.FullLogin(context.Background(), instCfg.Email, instCfg.Password)
		if err != nil {
			return nil, err
		}
		serviceAuth = sa
		firebaseTokens = ft
		apiKey = serviceAuth.APIKey
		apiServerURL = serviceAuth.APIServerURL
		log.Printf("Firebase login successful for '%s'", instCfg.Name)
	}

	// Priority 2/3: direct api_key or state.vscdb
	if apiKey == "" {
		log.Printf("No api_key in config, trying state.vscdb for '%s'...", instCfg.Name)
		key, err := auth.GetAPIKey()
		if err != nil {
			return nil, err
		}
		apiKey = key
	}

	// Start language server process. When no port is specified, pick a
	// free one so multiple standalone instances can coexist (the legacy
	// fallback to 42100 caused port collisions when running >1 account).
	serverPort := instCfg.ServerPort
	if serverPort == 0 {
		freePort, err := standalone.FindFreePort()
		if err != nil {
			return nil, fmt.Errorf("allocate server port for '%s': %w", instCfg.Name, err)
		}
		serverPort = freePort
		log.Printf("Auto-allocated server port %d for instance '%s'", serverPort, instCfg.Name)
	}

	version := instCfg.Version
	if version == "" {
		version = standalone.DefaultVersion
	}

	ls, err := standalone.NewLanguageServerProcess(
		apiKey,
		serverPort,
		instCfg.BinaryPath,
		version,
		apiServerURL,
	)
	if err != nil {
		return nil, err
	}

	err = ls.StartAndWait(30 * time.Second)
	if err != nil {
		return nil, err
	}

	lb.lsProcesses = append(lb.lsProcesses, ls)

	// Start token refresh if we have Firebase tokens
	if firebaseTokens != nil {
		tm = auth.NewTokenManager(firebaseTokens, serviceAuth, func(newKey string) {
			ls.UpdateAPIKey(newKey)
		})
		tm.Start()
		lb.tokenManagers = append(lb.tokenManagers, tm)
	}

	client := grpc.NewWindsurfGrpcClient("127.0.0.1", serverPort, ls.CSRFToken)

	inst := &Instance{
		Name:         instCfg.Name,
		Host:         "127.0.0.1",
		Port:         serverPort,
		CSRFToken:    ls.CSRFToken,
		APIKey:       apiKey,
		Version:      ls.Version,
		Client:       client,
		Weight:       instCfg.Weight,
		Healthy:      true,
		LSProcess:    ls,
		TokenManager: tm,
	}

	lb.AddInstance(inst)
	log.Printf("Added standalone instance '%s' (port=%d)", instCfg.Name, serverPort)
	return inst, nil
}

// InitFromConfigs initializes all instances from config entries.
func (lb *LoadBalancer) InitFromConfigs(configs []config.InstanceConfig) {
	for _, cfg := range configs {
		var err error
		switch cfg.Type {
		case "local":
			_, err = lb.AddLocalInstance(cfg)
		case "manual":
			_, err = lb.AddManualInstance(cfg)
		case "standalone":
			_, err = lb.AddStandaloneInstance(cfg)
		default:
			log.Printf("Unsupported instance type '%s' for '%s'", cfg.Type, cfg.Name)
			continue
		}
		if err != nil {
			log.Printf("Failed to initialize instance '%s': %v", cfg.Name, err)
		}
	}
}

// GetInstance selects a healthy instance based on the configured strategy.
func (lb *LoadBalancer) GetInstance(exclude map[string]bool) (*Instance, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	healthy := make([]*Instance, 0)
	for _, inst := range lb.instances {
		if inst.Healthy && (exclude == nil || !exclude[inst.Name]) {
			healthy = append(healthy, inst)
		}
	}

	if len(healthy) == 0 {
		return nil, ErrNoHealthyInstances
	}

	strategy := lb.config.Strategy

	var selected *Instance

	switch strategy {
	case "least_connections":
		selected = selectLeastConnections(healthy)
	case "weighted_round_robin":
		selected = selectWeightedRoundRobin(healthy)
	default: // round_robin
		selected = selectRoundRobin(healthy, &lb.rrIndex)
	}

	selected.ActiveConns++
	selected.TotalRequests++
	selected.LastUsed = time.Now()

	log.Printf("[Balancer] Selected instance '%s' (strategy=%s, connections=%d)",
		selected.Name, strategy, selected.ActiveConns)

	return selected, nil
}

// ReleaseInstance marks an instance as no longer in use.
func (lb *LoadBalancer) ReleaseInstance(inst *Instance) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if inst.ActiveConns > 0 {
		inst.ActiveConns--
	}
}

// MarkSuccess resets failure counter on success.
func (lb *LoadBalancer) MarkSuccess(inst *Instance) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	inst.ConsecutiveFails = 0
	inst.Healthy = true
	inst.LastError = ""
}

// MarkError records a request failure.
func (lb *LoadBalancer) MarkError(inst *Instance, err string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	inst.ConsecutiveFails++
	inst.LastError = err

	if inst.ConsecutiveFails >= unhealthyThreshold {
		if inst.Healthy {
			log.Printf("Instance '%s' marked unhealthy after %d failures: %s",
				inst.Name, inst.ConsecutiveFails, err)
		}
		inst.Healthy = false
	}
}

// StartHealthChecks starts the background health check loop.
func (lb *LoadBalancer) StartHealthChecks() {
	go lb.healthCheckLoop()
	log.Printf("Health checks started (interval=%ds)", lb.config.HealthCheckInterval)
}

// Stop stops the load balancer.
func (lb *LoadBalancer) Stop() {
	lb.cancel()

	// Stop token managers
	for _, tm := range lb.tokenManagers {
		tm.Stop()
	}
	lb.tokenManagers = nil

	// Close client connections
	for _, inst := range lb.instances {
		if inst.Client != nil {
			inst.Client.Close()
		}
	}

	// Shutdown language server processes
	for _, ls := range lb.lsProcesses {
		ls.Shutdown()
	}
	lb.lsProcesses = nil
}

// GetInstances returns all instances.
func (lb *LoadBalancer) GetInstances() []*Instance {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return append([]*Instance(nil), lb.instances...)
}

// RemoveInstance removes an instance by name.
func (lb *LoadBalancer) RemoveInstance(name string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	newInstances := make([]*Instance, 0)
	for _, inst := range lb.instances {
		if inst.Name == name {
			if inst.TokenManager != nil {
				inst.TokenManager.Stop()
				lb.tokenManagers = removeTokenManager(lb.tokenManagers, inst.TokenManager)
			}
			if inst.Client != nil {
				inst.Client.Close()
			}
			if inst.LSProcess != nil {
				inst.LSProcess.Shutdown()
				lb.lsProcesses = removeLSProcess(lb.lsProcesses, inst.LSProcess)
			}
			continue
		}
		newInstances = append(newInstances, inst)
	}
	lb.instances = newInstances
}

// GetInstanceByName returns an instance by name.
func (lb *LoadBalancer) GetInstanceByName(name string) *Instance {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, inst := range lb.instances {
		if inst.Name == name {
			return inst
		}
	}
	return nil
}

// Status returns a summary of all instances.
func (lb *LoadBalancer) Status() []map[string]interface{} {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	result := make([]map[string]interface{}, 0)
	for _, inst := range lb.instances {
		result = append(result, map[string]interface{}{
			"name":                 inst.Name,
			"healthy":              inst.Healthy,
			"active_connections":   inst.ActiveConns,
			"total_requests":       inst.TotalRequests,
			"consecutive_failures": inst.ConsecutiveFails,
			"weight":               inst.Weight,
			"last_error":           inst.LastError,
		})
	}
	return result
}

func (lb *LoadBalancer) healthCheckLoop() {
	ticker := time.NewTicker(time.Duration(lb.config.HealthCheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-lb.ctx.Done():
			return
		case <-ticker.C:
			for _, inst := range lb.instances {
				ok := inst.Client.Ping()
				lb.mu.Lock()
				if ok && !inst.Healthy {
					log.Printf("Instance '%s' recovered", inst.Name)
					inst.LastError = ""
				}
				inst.Healthy = ok
				lb.mu.Unlock()
			}
		}
	}
}

// Selection strategies

func selectLeastConnections(instances []*Instance) *Instance {
	min := instances[0]
	for _, inst := range instances {
		if inst.ActiveConns < min.ActiveConns {
			min = inst
		}
	}
	return min
}

func selectWeightedRoundRobin(instances []*Instance) *Instance {
	totalWeight := 0
	for _, inst := range instances {
		totalWeight += inst.Weight
	}

	best := instances[0]
	maxCW := 0

	for _, inst := range instances {
		cw := inst.currentWeight + inst.Weight
		inst.currentWeight = cw
		if cw > maxCW {
			maxCW = cw
			best = inst
		}
	}

	best.currentWeight -= totalWeight
	return best
}

func selectRoundRobin(instances []*Instance, index *int) *Instance {
	inst := instances[*index%len(instances)]
	*index++
	return inst
}

// Error definitions
var ErrNoHealthyInstances = &NoHealthyInstancesError{}

type NoHealthyInstancesError struct{}

func (e *NoHealthyInstancesError) Error() string {
	return "no healthy Windsurf instances available"
}

func removeLSProcess(processes []*standalone.LanguageServerProcess, target *standalone.LanguageServerProcess) []*standalone.LanguageServerProcess {
	result := processes[:0]
	for _, process := range processes {
		if process != target {
			result = append(result, process)
		}
	}
	return result
}

func removeTokenManager(managers []*auth.TokenManager, target *auth.TokenManager) []*auth.TokenManager {
	result := managers[:0]
	for _, manager := range managers {
		if manager != target {
			result = append(result, manager)
		}
	}
	return result
}
