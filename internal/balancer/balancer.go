// Package balancer provides load balancing for multiple Windsurf instances.
package balancer

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"windsurf-proxy-go/internal/accounts"
	"windsurf-proxy-go/internal/auth"
	"windsurf-proxy-go/internal/config"
	"windsurf-proxy-go/internal/core/grpc"
	"windsurf-proxy-go/internal/standalone"
)

const unhealthyThreshold = 3

var discoverLocalCredentials = auth.DiscoverCredentials

// Instance represents a Windsurf backend that can serve requests.
type Instance struct {
	Name               string
	Type               string
	AccountID          string
	BootstrapAccountID string
	Host               string
	Port               int
	CSRFToken          string
	APIKey             string
	APIServer          string
	ProxyURL           string
	ProxyKey           string
	Version            string
	Client             *grpc.WindsurfGrpcClient
	Weight             int
	Healthy            bool
	ActiveConns        int
	TotalRequests      int
	LastError          string
	LastUsed           time.Time
	ConsecutiveFails   int
	currentWeight      int // for smooth weighted round-robin

	// Standalone mode
	LSProcess *standalone.LanguageServerProcess
	Dynamic   bool
}

func (inst *Instance) CurrentAPIKey() string {
	if inst == nil {
		return ""
	}
	return inst.APIKey
}

func (inst *Instance) Clone() *Instance {
	if inst == nil {
		return nil
	}
	copy := *inst
	return &copy
}

func (inst *Instance) ProxyDisplay() string {
	if inst == nil {
		return ""
	}
	return standalone.MaskProxyForLog(inst.ProxyURL)
}

type pendingStandalone struct {
	done chan struct{}
	inst *Instance
	err  error
}

// LoadBalancer manages multiple Windsurf instances and distributes requests.
type LoadBalancer struct {
	config             *config.BalancingConfig
	accountMgr         *accounts.Manager
	instances          []*Instance
	rrIndex            int
	mu                 sync.Mutex
	ctx                context.Context
	cancel             context.CancelFunc
	lsProcesses        []*standalone.LanguageServerProcess
	standaloneTemplate *config.InstanceConfig
	pendingStandalone  map[string]*pendingStandalone
}

// New creates a new load balancer.
func New(cfg *config.BalancingConfig, accountMgr *accounts.Manager) *LoadBalancer {
	ctx, cancel := context.WithCancel(context.Background())
	return &LoadBalancer{
		config:            cfg,
		accountMgr:        accountMgr,
		ctx:               ctx,
		cancel:            cancel,
		pendingStandalone: make(map[string]*pendingStandalone),
	}
}

// AddInstance adds an instance to the balancer.
func (lb *LoadBalancer) AddInstance(inst *Instance) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.instances = append(lb.instances, inst)
	if inst.AccountID != "" && lb.accountMgr != nil {
		lb.accountMgr.MarkSuccess(inst.AccountID)
	}
	log.Printf("Added instance '%s' (host=%s, port=%d)", inst.Name, inst.Host, inst.Port)
}

// AddLocalInstance adds a local Windsurf instance with auto-discovered credentials.
func (lb *LoadBalancer) AddLocalInstance(instCfg config.InstanceConfig) (*Instance, error) {
	creds, err := discoverLocalCredentials()
	if err != nil {
		return nil, err
	}

	client := grpc.NewWindsurfGrpcClient("127.0.0.1", creds.GRPCPort, creds.CSRFToken)

	inst := &Instance{
		Name:      instCfg.Name,
		Type:      "local",
		Host:      "127.0.0.1",
		Port:      creds.GRPCPort,
		CSRFToken: creds.CSRFToken,
		APIKey:    creds.APIKey,
		ProxyURL:  strings.TrimSpace(instCfg.Proxy),
		ProxyKey:  normalizeProxyKey(instCfg.Proxy),
		Version:   creds.Version,
		Client:    client,
		Weight:    instCfg.Weight,
		Healthy:   true,
	}

	lb.AddInstance(inst)
	log.Printf("Added local instance '%s' with discovered credentials (port=%d)", instCfg.Name, creds.GRPCPort)
	return inst, nil
}

func (lb *LoadBalancer) RefreshLocalInstance(inst *Instance) error {
	if inst == nil {
		return fmt.Errorf("instance is nil")
	}
	if inst.Type != "local" {
		return fmt.Errorf("instance %q is not local", inst.Name)
	}

	creds, err := discoverLocalCredentials()
	if err != nil {
		return err
	}
	client := grpc.NewWindsurfGrpcClient("127.0.0.1", creds.GRPCPort, creds.CSRFToken)

	lb.mu.Lock()
	defer lb.mu.Unlock()

	if inst.Client != nil {
		_ = inst.Client.Close()
	}
	inst.Host = "127.0.0.1"
	inst.Port = creds.GRPCPort
	inst.CSRFToken = creds.CSRFToken
	inst.APIKey = creds.APIKey
	inst.Version = creds.Version
	inst.Client = client
	inst.Healthy = true
	inst.ConsecutiveFails = 0
	inst.LastError = ""

	log.Printf("Refreshed local instance '%s' credentials (port=%d)", inst.Name, creds.GRPCPort)
	return nil
}

// AddManualInstance adds a manually-configured instance.
func (lb *LoadBalancer) AddManualInstance(instCfg config.InstanceConfig) (*Instance, error) {
	if instCfg.Host == "" {
		instCfg.Host = "127.0.0.1"
	}

	client := grpc.NewWindsurfGrpcClient(instCfg.Host, instCfg.GRPCPort, instCfg.CSRFToken)

	inst := &Instance{
		Name:      instCfg.Name,
		Type:      "manual",
		Host:      instCfg.Host,
		Port:      instCfg.GRPCPort,
		CSRFToken: instCfg.CSRFToken,
		APIKey:    instCfg.APIKey,
		ProxyURL:  strings.TrimSpace(instCfg.Proxy),
		ProxyKey:  normalizeProxyKey(instCfg.Proxy),
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
// The optional account_id is only used to bootstrap metadata/API-server
// selection. Request routing injects the selected account's API key per call,
// so one standalone process can serve many accounts on the same API server.
func (lb *LoadBalancer) AddStandaloneInstance(instCfg config.InstanceConfig) (*Instance, error) {
	lb.rememberStandaloneTemplate(instCfg)

	apiServerURL := standalone.DefaultAPIServerURL
	bootstrapAccountID := strings.TrimSpace(instCfg.AccountID)
	apiKey := strings.TrimSpace(instCfg.APIKey)
	proxyURL := strings.TrimSpace(instCfg.Proxy)

	if bootstrapAccountID != "" {
		if lb.accountMgr == nil {
			log.Printf("[Balancer] Standalone instance '%s' has bootstrap account '%s' but account manager is not configured; starting shared worker without account metadata",
				instCfg.Name, bootstrapAccountID)
		} else {
			if account := lb.accountMgr.Get(bootstrapAccountID); account != nil {
				if strings.TrimSpace(account.APIServer) != "" {
					apiServerURL = strings.TrimSpace(account.APIServer)
				}
				if proxyURL == "" {
					proxyURL = strings.TrimSpace(account.Proxy)
				}
			}
			log.Printf("Resolving bootstrap account '%s' for standalone instance '%s'...", bootstrapAccountID, instCfg.Name)
			resolved, err := lb.accountMgr.ResolveForRequest(context.Background(), bootstrapAccountID)
			if err != nil {
				log.Printf("[Balancer] Bootstrap account '%s' for standalone instance '%s' could not be resolved: %v; starting shared worker with request-time account credentials",
					bootstrapAccountID, instCfg.Name, err)
			} else {
				apiKey = resolved.APIKey
				if resolved.APIServer != "" {
					apiServerURL = resolved.APIServer
				}
			}
		}
	}

	inst, ls, err := lb.startStandaloneWorker(instCfg, apiServerURL, apiKey, bootstrapAccountID, proxyURL, false)
	if err != nil {
		return nil, err
	}

	lb.mu.Lock()
	lb.lsProcesses = append(lb.lsProcesses, ls)
	lb.mu.Unlock()
	lb.AddInstance(inst)
	log.Printf("Added shared standalone instance '%s' (port=%d, api_server=%s, proxy=%s, bootstrap_account=%s)",
		instCfg.Name, inst.Port, apiServerURL, standalone.MaskProxyForLog(proxyURL), bootstrapAccountID)
	return inst, nil
}

func (lb *LoadBalancer) startStandaloneWorker(instCfg config.InstanceConfig, apiServerURL string, apiKey string, bootstrapAccountID string, proxyURL string, dynamic bool) (*Instance, *standalone.LanguageServerProcess, error) {
	// Start language server process. When no port is specified, pick a
	// free one so multiple standalone instances can coexist (the legacy
	// fallback to 42100 caused port collisions when running >1 account).
	serverPort := instCfg.ServerPort
	if serverPort == 0 {
		freePort, err := standalone.FindFreePortBlock()
		if err != nil {
			return nil, nil, fmt.Errorf("allocate server port for '%s': %w", instCfg.Name, err)
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
		proxyURL,
	)
	if err != nil {
		return nil, nil, err
	}

	err = ls.StartAndWait(30 * time.Second)
	if err != nil {
		return nil, nil, err
	}

	client := grpc.NewWindsurfGrpcClient("127.0.0.1", serverPort, ls.CSRFToken)

	inst := &Instance{
		Name:               instCfg.Name,
		Type:               "standalone",
		BootstrapAccountID: bootstrapAccountID,
		Host:               "127.0.0.1",
		Port:               serverPort,
		CSRFToken:          ls.CSRFToken,
		APIKey:             apiKey,
		APIServer:          apiServerURL,
		ProxyURL:           strings.TrimSpace(proxyURL),
		ProxyKey:           normalizeProxyKey(proxyURL),
		Version:            ls.Version,
		Client:             client,
		Weight:             instCfg.Weight,
		Healthy:            true,
		LSProcess:          ls,
		Dynamic:            dynamic,
	}

	return inst, ls, nil
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
	if lb.accountMgr != nil {
		activeAccounts := 0
		for _, account := range lb.accountMgr.List(configs) {
			if account.Status == "active" {
				activeAccounts++
			}
		}
		standaloneWorkers := lb.countInstancesByType("standalone")
		if activeAccounts > 0 && standaloneWorkers > 0 {
			log.Printf("[Balancer] %d standalone worker(s) available as shared transports for %d active account(s)", standaloneWorkers, activeAccounts)
		} else if activeAccounts > 0 {
			log.Printf("[Balancer] %d active account(s) configured but no standalone worker is configured; account traffic will rely on local/manual workers", activeAccounts)
		}
	}
}

func (lb *LoadBalancer) countInstancesByType(instanceType string) int {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	count := 0
	for _, inst := range lb.instances {
		if inst.Type == instanceType {
			count++
		}
	}
	return count
}

func (lb *LoadBalancer) rememberStandaloneTemplate(instCfg config.InstanceConfig) {
	if instCfg.Type != "standalone" {
		return
	}
	copy := instCfg
	copy.ServerPort = 0
	copy.AccountID = ""
	copy.APIKey = ""
	copy.Proxy = ""
	lb.mu.Lock()
	if lb.standaloneTemplate == nil {
		lb.standaloneTemplate = &copy
	}
	lb.mu.Unlock()
}

// EnsureStandaloneForRoute makes sure there is a managed standalone worker for
// the api_server + proxy route. It uses the first configured standalone worker
// as a template, so accounts can share one LS per egress route without requiring
// one configured instance per account.
func (lb *LoadBalancer) EnsureStandaloneForRoute(apiServer string, proxyURL string, bootstrapAPIKey string) (*Instance, error) {
	route := standaloneRouteKey(apiServer, proxyURL)

	for {
		lb.mu.Lock()
		if inst := lb.findStandaloneForRouteLocked(apiServer, proxyURL); inst != nil {
			lb.mu.Unlock()
			return inst, nil
		}
		if lb.pendingStandalone == nil {
			lb.pendingStandalone = make(map[string]*pendingStandalone)
		}
		if pending := lb.pendingStandalone[route]; pending != nil {
			done := pending.done
			lb.mu.Unlock()
			<-done
			if pending.err != nil {
				return nil, pending.err
			}
			continue
		}
		if lb.standaloneTemplate == nil {
			lb.mu.Unlock()
			return nil, ErrNoStandaloneTemplate
		}
		template := *lb.standaloneTemplate
		template.Name = dynamicStandaloneName(route)
		template.Proxy = strings.TrimSpace(proxyURL)
		pending := &pendingStandalone{done: make(chan struct{})}
		lb.pendingStandalone[route] = pending
		lb.mu.Unlock()

		inst, ls, err := lb.startStandaloneWorker(template, normalizeAPIServer(apiServer), strings.TrimSpace(bootstrapAPIKey), "", proxyURL, true)

		lb.mu.Lock()
		if err == nil {
			lb.instances = append(lb.instances, inst)
			lb.lsProcesses = append(lb.lsProcesses, ls)
			log.Printf("[Balancer] Added dynamic standalone worker '%s' (port=%d, api_server=%s, proxy=%s)",
				inst.Name, inst.Port, normalizeAPIServer(apiServer), standalone.MaskProxyForLog(proxyURL))
		}
		pending.inst = inst
		pending.err = err
		delete(lb.pendingStandalone, route)
		close(pending.done)
		lb.mu.Unlock()

		return inst, err
	}
}

func (lb *LoadBalancer) findStandaloneForRouteLocked(apiServer string, proxyURL string) *Instance {
	for _, inst := range lb.instances {
		if inst.Type != "standalone" || !inst.Healthy {
			continue
		}
		if !compatibleAPIServer(inst.APIServer, apiServer) {
			continue
		}
		if !compatibleProxy(inst.ProxyURL, proxyURL) {
			continue
		}
		return inst
	}
	return nil
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

	var selected *Instance
	if preferred := lb.selectStandaloneByAccount(healthy); preferred != nil {
		selected = preferred
	} else {
		selected = lb.selectByStrategy(healthy)
	}

	selected.ActiveConns++
	selected.TotalRequests++
	selected.LastUsed = time.Now()
	if selected.AccountID != "" && lb.accountMgr != nil {
		lb.accountMgr.MarkAcquire(selected.AccountID)
	}

	log.Printf("[Balancer] Selected instance '%s' (type=%s, connections=%d)",
		selected.Name, selected.Type, selected.ActiveConns)

	return selected, nil
}

// RouteKey identifies a request route by worker instance plus routed account.
func RouteKey(instanceName string, accountID string) string {
	if accountID == "" {
		return instanceName
	}
	return instanceName + "|" + accountID
}

// GetWorker selects a healthy transport worker. Account-backed requests prefer
// a compatible shared standalone worker. Requests without an account never use
// standalone, because those workers require per-request account credentials.
func (lb *LoadBalancer) GetWorker(exclude map[string]bool, accountID string, apiServer string, proxyURL string) (*Instance, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	preferred := make([]*Instance, 0)
	fallback := make([]*Instance, 0)
	for _, inst := range lb.instances {
		if !lb.workerEligible(inst, accountID, apiServer, proxyURL, exclude) {
			continue
		}
		if accountID != "" && inst.Type == "standalone" {
			preferred = append(preferred, inst)
			continue
		}
		fallback = append(fallback, inst)
	}

	pool := preferred
	if len(pool) == 0 {
		pool = fallback
	}
	if len(pool) == 0 {
		return nil, ErrNoHealthyInstances
	}

	selected := lb.selectByStrategy(pool)
	selected.ActiveConns++
	selected.TotalRequests++
	selected.LastUsed = time.Now()

	log.Printf("[Balancer] Selected worker '%s' (type=%s, account=%s, connections=%d)",
		selected.Name, selected.Type, accountID, selected.ActiveConns)

	return selected, nil
}

// ProbeWorker returns a healthy compatible worker without mutating runtime
// counters. It is used for account-status probes like GetUserStatus.
func (lb *LoadBalancer) ProbeWorker(accountID string, apiServer string, proxyURL string) *Instance {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	preferred := make([]*Instance, 0)
	fallback := make([]*Instance, 0)
	for _, inst := range lb.instances {
		if !lb.workerEligible(inst, accountID, apiServer, proxyURL, nil) {
			continue
		}
		if accountID != "" && inst.Type == "standalone" {
			preferred = append(preferred, inst)
			continue
		}
		fallback = append(fallback, inst)
	}

	pool := preferred
	if len(pool) == 0 {
		pool = fallback
	}
	if len(pool) == 0 {
		return nil
	}

	return selectLeastConnections(pool).Clone()
}

// TryAcquireWorkerByName reacquires a preferred worker if it is still healthy
// and compatible with the routed account.
func (lb *LoadBalancer) TryAcquireWorkerByName(name string, accountID string, apiServer string, proxyURL string, exclude map[string]bool) (*Instance, bool) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, inst := range lb.instances {
		if inst.Name != name {
			continue
		}
		if !lb.workerEligible(inst, accountID, apiServer, proxyURL, exclude) {
			return nil, false
		}
		inst.ActiveConns++
		inst.TotalRequests++
		inst.LastUsed = time.Now()
		log.Printf("[Balancer] Reused worker '%s' (account=%s, connections=%d)", inst.Name, accountID, inst.ActiveConns)
		return inst, true
	}

	return nil, false
}

func (lb *LoadBalancer) TryAcquireInstanceByName(name string, exclude map[string]bool) (*Instance, bool) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, inst := range lb.instances {
		if inst.Name != name {
			continue
		}
		if !inst.Healthy || (exclude != nil && exclude[inst.Name]) {
			return nil, false
		}
		if inst.AccountID != "" && lb.accountMgr != nil && !lb.accountMgr.IsSchedulable(inst.AccountID) {
			return nil, false
		}
		inst.ActiveConns++
		inst.TotalRequests++
		inst.LastUsed = time.Now()
		if inst.AccountID != "" && lb.accountMgr != nil {
			lb.accountMgr.MarkAcquire(inst.AccountID)
		}
		log.Printf("[Balancer] Selected preferred instance '%s' (connections=%d)", inst.Name, inst.ActiveConns)
		return inst, true
	}

	return nil, false
}

func (lb *LoadBalancer) workerEligible(inst *Instance, accountID string, apiServer string, proxyURL string, exclude map[string]bool) bool {
	if inst == nil || !inst.Healthy {
		return false
	}
	if exclude != nil && exclude[RouteKey(inst.Name, accountID)] {
		return false
	}
	if inst.AccountID != "" && inst.Type != "standalone" && lb.accountMgr != nil {
		if !lb.accountMgr.IsSchedulable(inst.AccountID) {
			return false
		}
		if accountID == "" {
			return false
		}
	}
	if !compatibleProxy(inst.ProxyURL, proxyURL) {
		return false
	}
	if inst.Type == "standalone" {
		if accountID == "" {
			return false
		}
		if !compatibleAPIServer(inst.APIServer, apiServer) {
			return false
		}
	}
	return true
}

func compatibleAPIServer(instanceServer string, accountServer string) bool {
	return normalizeAPIServer(instanceServer) == normalizeAPIServer(accountServer)
}

func normalizeAPIServer(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = standalone.DefaultAPIServerURL
	}
	return strings.TrimRight(strings.ToLower(value), "/")
}

func compatibleProxy(instanceProxy string, accountProxy string) bool {
	return normalizeProxyKey(instanceProxy) == normalizeProxyKey(accountProxy)
}

func normalizeProxyKey(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Host != "" {
		scheme := strings.ToLower(parsed.Scheme)
		host := strings.ToLower(parsed.Hostname())
		port := parsed.Port()
		if port == "" {
			switch scheme {
			case "http", "socks5", "socks5h":
				port = "8080"
			case "https":
				port = "443"
			}
		}
		auth := ""
		if parsed.User != nil {
			auth = parsed.User.String() + "@"
		}
		if port != "" {
			return scheme + "://" + auth + host + ":" + port
		}
		return scheme + "://" + auth + host
	}
	return strings.ToLower(value)
}

func standaloneRouteKey(apiServer string, proxyURL string) string {
	return normalizeAPIServer(apiServer) + "|" + normalizeProxyKey(proxyURL)
}

func dynamicStandaloneName(route string) string {
	sum := sha1.Sum([]byte(route))
	return "standalone-" + hex.EncodeToString(sum[:])[:10]
}

func (lb *LoadBalancer) selectByStrategy(instances []*Instance) *Instance {
	switch lb.config.Strategy {
	case "least_connections":
		return selectLeastConnections(instances)
	case "weighted_round_robin":
		return selectWeightedRoundRobin(instances)
	default:
		return selectRoundRobin(instances, &lb.rrIndex)
	}
}

func (lb *LoadBalancer) selectStandaloneByAccount(instances []*Instance) *Instance {
	if lb.accountMgr == nil {
		return nil
	}

	var best *Instance
	bestScore := -1 << 30
	for _, inst := range instances {
		if inst.Type != "standalone" || inst.AccountID == "" {
			continue
		}
		if !lb.accountMgr.IsSchedulable(inst.AccountID) {
			continue
		}
		score := lb.accountScore(inst)
		if best == nil || score > bestScore {
			best = inst
			bestScore = score
		}
	}
	return best
}

func (lb *LoadBalancer) accountScore(inst *Instance) int {
	state := lb.accountMgr.Runtime(inst.AccountID)
	score := 0
	if state.Healthy {
		score += 10000
	}
	score -= state.ActiveRequests * 100
	score -= state.ConsecutiveFails * 500
	score -= inst.ActiveConns * 50
	score += inst.Weight * 10
	if !state.LastUsed.IsZero() {
		score += int(time.Since(state.LastUsed).Seconds())
	}
	if !inst.LastUsed.IsZero() {
		score += int(time.Since(inst.LastUsed).Seconds() / 2)
	}
	return score
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

// MarkUnhealthy immediately removes a worker from rotation after an error that
// indicates the transport/session itself is likely poisoned rather than just a
// single account being exhausted or rate limited.
func (lb *LoadBalancer) MarkUnhealthy(inst *Instance, err string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	inst.ConsecutiveFails++
	inst.LastError = err
	if inst.Healthy {
		log.Printf("Instance '%s' marked unhealthy: %s", inst.Name, err)
	}
	inst.Healthy = false
}

// StartHealthChecks starts the background health check loop.
func (lb *LoadBalancer) StartHealthChecks() {
	go lb.healthCheckLoop()
	log.Printf("Health checks started (interval=%ds)", lb.config.HealthCheckInterval)
}

// Stop stops the load balancer.
func (lb *LoadBalancer) Stop() {
	lb.cancel()

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
	result := make([]*Instance, 0, len(lb.instances))
	for _, inst := range lb.instances {
		result = append(result, inst.Clone())
	}
	return result
}

// RemoveInstance removes an instance by name.
func (lb *LoadBalancer) RemoveInstance(name string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	newInstances := make([]*Instance, 0)
	for _, inst := range lb.instances {
		if inst.Name == name {
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
				if inst.AccountID != "" && lb.accountMgr != nil {
					if ok {
						lb.accountMgr.MarkSuccess(inst.AccountID)
					} else {
						lb.accountMgr.MarkError(inst.AccountID, "health check failed", unhealthyThreshold)
					}
				}
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
var ErrNoStandaloneTemplate = fmt.Errorf("no standalone template configured")

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
