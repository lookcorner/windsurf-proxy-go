// Package management provides management API endpoints for desktop GUI.
package management

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"windsurf-proxy-go/internal/accounts"
	"windsurf-proxy-go/internal/balancer"
	"windsurf-proxy-go/internal/config"
	"windsurf-proxy-go/internal/core"
	"windsurf-proxy-go/internal/keys"

	"github.com/gorilla/websocket"
)

// Handler holds management API dependencies.
type Handler struct {
	balancer              *balancer.LoadBalancer
	keys                  *keys.Manager
	accountMgr            *accounts.Manager
	config                *config.Config
	configPath            string
	startTime             time.Time
	onServerConfigChanged func(prev, next config.ServerConfig)
	onLoggingChanged      func(next config.LoggingConfig)
	refreshCtx            context.Context
	refreshCancel         context.CancelFunc

	// Request history (ring buffer)
	requestHistory *list.List
	maxHistory     int
	historyMu      sync.Mutex
	totalRequests  int

	// WebSocket for logs
	logClients   map[*websocket.Conn]bool
	logClientsMu sync.Mutex
}

// NewHandler creates a new management handler.
func NewHandler(
	bal *balancer.LoadBalancer,
	keyMgr *keys.Manager,
	accountMgr *accounts.Manager,
	cfg *config.Config,
	configPath string,
) *Handler {
	refreshCtx, refreshCancel := context.WithCancel(context.Background())
	h := &Handler{
		balancer:       bal,
		keys:           keyMgr,
		accountMgr:     accountMgr,
		config:         cfg,
		configPath:     configPath,
		startTime:      time.Now(),
		refreshCtx:     refreshCtx,
		refreshCancel:  refreshCancel,
		requestHistory: list.New(),
		maxHistory:     500,
		logClients:     make(map[*websocket.Conn]bool),
	}
	if accountMgr != nil {
		accountMgr.SetPersistHook(func() {
			if err := h.saveConfig(); err != nil {
				log.Printf("Failed to save config after account refresh: %v", err)
			}
		})
		h.startBackgroundAccountRefresh()
	}
	return h
}

// Stop shuts down background loops owned by the management handler.
func (h *Handler) Stop() {
	if h == nil || h.refreshCancel == nil {
		return
	}
	h.refreshCancel()
}

// RegisterRoutes registers management routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/accounts", h.localOnly(h.handleAccounts))
	mux.HandleFunc("/api/accounts/", h.localOnly(h.handleAccount))
	mux.HandleFunc("/api/instances", h.localOnly(h.handleInstances))
	mux.HandleFunc("/api/instances/", h.localOnly(h.handleInstance))
	mux.HandleFunc("/api/keys", h.localOnly(h.handleKeys))
	mux.HandleFunc("/api/keys/", h.localOnly(h.handleKey))
	mux.HandleFunc("/api/config", h.localOnly(h.handleConfig))
	mux.HandleFunc("/api/stats", h.localOnly(h.handleStats))
	mux.HandleFunc("/api/models", h.localOnly(h.handleModels))
	mux.HandleFunc("/api/requests", h.localOnly(h.handleRequests))
	mux.HandleFunc("/api/logs", h.localOnly(h.handleLogs))
}

// SetServerConfigChangedHandler registers a callback for host/port changes.
func (h *Handler) SetServerConfigChangedHandler(fn func(prev, next config.ServerConfig)) {
	h.onServerConfigChanged = fn
}

// SetLoggingChangedHandler registers a callback that fires whenever the
// logging block in config is updated, e.g. when the user toggles the
// audit log from the Settings page.
func (h *Handler) SetLoggingChangedHandler(fn func(next config.LoggingConfig)) {
	h.onLoggingChanged = fn
}

// ============================================================================
// Account management
// ============================================================================

type AccountCreate struct {
	Name                 string   `json:"name"`
	Email                string   `json:"email"`
	Password             string   `json:"password"`
	APIKey               string   `json:"api_key"`
	APIServer            string   `json:"api_server"`
	Proxy                string   `json:"proxy"`
	AvailableModels      []string `json:"available_models"`
	BlockedModels        []string `json:"blocked_models"`
	FirebaseRefreshToken string   `json:"firebase_refresh_token"`
}

type accountRefreshResult struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	AuthSource     string `json:"auth_source"`
	HasAPIKey      bool   `json:"has_api_key"`
	UsageStatus    string `json:"usage_status"`
	QuotaExhausted bool   `json:"quota_exhausted"`
	Error          string `json:"error,omitempty"`
}

const (
	accountRefreshTimeout          = 30 * time.Second
	accountRefreshRetryDelay       = 3 * time.Second
	accountBatchRefreshConcurrency = 4
	accountBackgroundRefreshDelay  = 15 * time.Second
	accountBackgroundRefreshPeriod = 5 * time.Minute
)

func (h *Handler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listAccounts(w, r)
	case http.MethodPost:
		h.createAccount(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleAccount(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "refresh-all" {
		h.refreshAllAccounts(w, r)
		return
	}

	id := parts[2]
	if r.Method == http.MethodPost && len(parts) >= 4 && parts[3] == "refresh" {
		h.refreshAccount(w, r, id)
		return
	}
	if r.Method == http.MethodDelete {
		h.deleteAccount(w, r, id)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	accountsList := []accounts.View{}
	if h.accountMgr != nil {
		accountsList = h.accountMgr.List(h.config.Instances)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"accounts": accountsList})
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	if h.accountMgr == nil {
		http.Error(w, "Account manager not available", http.StatusInternalServerError)
		return
	}

	var body AccountCreate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	id := "acct_" + generateHex(10)
	entry, err := h.accountMgr.Add(config.AccountConfig{
		ID:                   id,
		Name:                 strings.TrimSpace(body.Name),
		Email:                strings.TrimSpace(body.Email),
		Password:             body.Password,
		APIKey:               strings.TrimSpace(body.APIKey),
		APIServer:            strings.TrimSpace(body.APIServer),
		Proxy:                strings.TrimSpace(body.Proxy),
		AvailableModels:      body.AvailableModels,
		BlockedModels:        body.BlockedModels,
		FirebaseRefreshToken: strings.TrimSpace(body.FirebaseRefreshToken),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.saveConfig(); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"id":     entry.ID,
		"name":   entry.Name,
	})
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	if h.accountMgr == nil {
		http.Error(w, "Account manager not available", http.StatusInternalServerError)
		return
	}

	if err := h.accountMgr.Remove(id, h.config.Instances); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for i := range h.config.Instances {
		if h.config.Instances[i].Type == "standalone" && h.config.Instances[i].AccountID == id {
			h.config.Instances[i].AccountID = ""
		}
	}

	if err := h.saveConfig(); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "deleted": id})
}

func (h *Handler) refreshAccount(w http.ResponseWriter, r *http.Request, id string) {
	if h.accountMgr == nil {
		http.Error(w, "Account manager not available", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), accountRefreshTimeout)
	defer cancel()

	result, err := h.refreshOneAccount(ctx, id, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "result": result})
}

func (h *Handler) refreshAllAccounts(w http.ResponseWriter, r *http.Request) {
	if h.accountMgr == nil {
		http.Error(w, "Account manager not available", http.StatusInternalServerError)
		return
	}

	entries := make([]config.AccountConfig, 0, len(h.config.Accounts))
	for _, entry := range h.config.Accounts {
		if strings.TrimSpace(entry.ID) == "" {
			continue
		}
		entries = append(entries, entry)
	}

	results := h.refreshAccountsBatch(r.Context(), entries)

	succeeded := 0
	failed := 0
	exhausted := 0
	for _, result := range results {
		if result.Error != "" {
			failed++
			continue
		}
		succeeded++
		if result.QuotaExhausted {
			exhausted++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"total":     len(results),
		"succeeded": succeeded,
		"failed":    failed,
		"exhausted": exhausted,
		"results":   results,
	})
}

func (h *Handler) refreshOneAccount(ctx context.Context, id string, forceCredentials bool) (accountRefreshResult, error) {
	entry := h.accountMgr.Get(id)
	if entry == nil {
		return accountRefreshResult{}, fmt.Errorf("account %q not found", id)
	}

	resolved, err := h.resolveAccountWithRetry(ctx, id, forceCredentials)
	if err != nil {
		return accountRefreshResult{
			ID:          entry.ID,
			Name:        entry.Name,
			AuthSource:  accounts.AuthSource(*entry),
			UsageStatus: "unavailable",
			Error:       err.Error(),
		}, err
	}
	h.accountMgr.MarkSuccess(id)

	result := accountRefreshResult{
		ID:         resolved.Account.ID,
		Name:       resolved.Account.Name,
		AuthSource: resolved.AuthSource,
		HasAPIKey:  resolved.APIKey != "",
	}

	usageStatus, quotaExhausted, usageErr := h.refreshResolvedAccountUsage(ctx, id, resolved)
	result.UsageStatus = usageStatus
	result.QuotaExhausted = quotaExhausted
	if usageErr != nil {
		result.Error = usageErr.Error()
		if forceCredentials {
			result.Error = ""
			return result, nil
		}
		return result, usageErr
	}

	return result, nil
}

func (h *Handler) resolveAccountWithRetry(ctx context.Context, id string, forceCredentials bool) (*accounts.Resolved, error) {
	resolve := func() (*accounts.Resolved, error) {
		if forceCredentials {
			return h.accountMgr.ResolveStandalone(ctx, id)
		}
		return h.accountMgr.ResolveForRequest(ctx, id)
	}

	resolved, err := resolve()
	if err == nil || ctx.Err() != nil {
		return resolved, err
	}

	log.Printf("[Management] Account '%s' refresh failed, retrying once: %v", id, err)
	timer := time.NewTimer(accountRefreshRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}

	return resolve()
}

func (h *Handler) refreshResolvedAccountUsage(ctx context.Context, id string, resolved *accounts.Resolved) (string, bool, error) {
	if resolved == nil || resolved.APIKey == "" {
		return "unknown", false, nil
	}

	proxyURL := strings.TrimSpace(resolved.Account.Proxy)
	if _, err := h.balancer.EnsureStandaloneForRoute(resolved.APIServer, proxyURL, resolved.APIKey); err != nil && err != balancer.ErrNoStandaloneTemplate {
		log.Printf("[Management] Standalone worker route ensure failed for account '%s': %v", resolved.Account.Name, err)
	}
	probe := h.balancer.ProbeWorker(id, resolved.APIServer, proxyURL)
	if probe == nil || probe.Client == nil {
		err := fmt.Errorf("no healthy worker available for usage refresh")
		h.accountMgr.MarkUsageUnavailable(id, err.Error())
		return "unavailable", false, err
	}

	fetch := func() (*accounts.UsageSnapshot, error) {
		status, err := probe.Client.GetUserStatus(ctx, resolved.APIKey, probe.Version)
		if err != nil {
			return nil, err
		}
		h.accountMgr.SyncAllowedModels(id, status.AllowedModels)
		snapshot := accounts.UsageFromUserStatus(status)
		return &snapshot, nil
	}

	snapshot, err := fetch()
	if err != nil && ctx.Err() == nil {
		log.Printf("[Management] Account '%s' usage refresh failed, retrying once: %v", resolved.Account.Name, err)
		timer := time.NewTimer(accountRefreshRetryDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-timer.C:
			snapshot, err = fetch()
		}
	}
	if err != nil {
		h.accountMgr.MarkUsageUnavailable(id, err.Error())
		log.Printf("[Management] Account '%s' usage refresh failed: %v", resolved.Account.Name, err)
		return "unavailable", false, err
	}

	h.accountMgr.UpdateUsage(id, *snapshot, accounts.QuotaRetryCooldown)
	if snapshot.QuotaExhausted {
		return "exhausted", true, nil
	}
	return "ok", false, nil
}

func (h *Handler) startBackgroundAccountRefresh() {
	go func() {
		log.Printf("[Management] Background account quota refresh started (delay=%s interval=%s)", accountBackgroundRefreshDelay, accountBackgroundRefreshPeriod)

		timer := time.NewTimer(accountBackgroundRefreshDelay)
		defer timer.Stop()

		ticker := time.NewTicker(accountBackgroundRefreshPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-h.refreshCtx.Done():
				log.Printf("[Management] Background account quota refresh stopped")
				return
			case <-timer.C:
				h.refreshDueAccounts()
			case <-ticker.C:
				h.refreshDueAccounts()
			}
		}
	}()
}

func (h *Handler) refreshDueAccounts() {
	if h == nil || h.accountMgr == nil {
		return
	}

	due := make([]config.AccountConfig, 0, len(h.config.Accounts))
	for _, entry := range h.config.Accounts {
		if strings.TrimSpace(entry.ID) == "" || strings.EqualFold(strings.TrimSpace(entry.Status), "disabled") {
			continue
		}
		if h.accountMgr.ShouldRefreshUsage(entry.ID, accounts.UsageRefreshTTL) {
			due = append(due, entry)
		}
	}
	if len(due) == 0 {
		return
	}

	results := h.refreshAccountsBatch(h.refreshCtx, due)
	succeeded := 0
	failed := 0
	exhausted := 0
	for _, result := range results {
		if result.Error != "" {
			failed++
			continue
		}
		succeeded++
		if result.QuotaExhausted {
			exhausted++
		}
	}

	log.Printf("[Management] Background account quota refresh complete: total=%d success=%d failed=%d exhausted=%d", len(results), succeeded, failed, exhausted)
}

func (h *Handler) refreshAccountsBatch(parent context.Context, entries []config.AccountConfig) []accountRefreshResult {
	results := make([]accountRefreshResult, len(entries))
	sem := make(chan struct{}, accountBatchRefreshConcurrency)
	var wg sync.WaitGroup

	for i, entry := range entries {
		i := i
		entry := entry
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(parent, accountRefreshTimeout)
			defer cancel()

			result, err := h.refreshOneAccount(ctx, entry.ID, false)
			if err != nil {
				results[i] = accountRefreshResult{
					ID:          entry.ID,
					Name:        entry.Name,
					AuthSource:  accounts.AuthSource(entry),
					UsageStatus: "unavailable",
					Error:       err.Error(),
				}
				return
			}
			results[i] = result
		}()
	}

	wg.Wait()
	return results
}

// ============================================================================
// Instance management
// ============================================================================

// handleInstances handles GET/POST /api/instances.
func (h *Handler) handleInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.listInstances(w, r)
	} else if r.Method == http.MethodPost {
		h.addInstance(w, r)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listInstances returns all instances with their status.
func (h *Handler) listInstances(w http.ResponseWriter, r *http.Request) {
	instances := h.balancer.GetInstances()

	result := []map[string]interface{}{}
	for _, inst := range instances {
		instType := "unknown"
		authSource := ""
		email := ""
		accountID := ""
		accountName := ""
		if cfg := getInstanceConfig(inst.Name, h.config); cfg != nil {
			instType = cfg.Type
			authSource = authSourceForConfig(cfg, h.accountMgr)
			accountID = cfg.AccountID
			if account := getAccountConfig(cfg.AccountID, h.config); account != nil {
				accountName = account.Name
				email = account.Email
			}
		}

		result = append(result, map[string]interface{}{
			"name":                 inst.Name,
			"type":                 instType,
			"auth_source":          authSource,
			"account_id":           accountID,
			"account_name":         accountName,
			"healthy":              inst.Healthy,
			"active_connections":   inst.ActiveConns,
			"total_requests":       inst.TotalRequests,
			"consecutive_failures": inst.ConsecutiveFails,
			"weight":               inst.Weight,
			"last_error":           inst.LastError,
			"host":                 inst.Host,
			"port":                 inst.Port,
			"proxy":                inst.ProxyDisplay(),
			"email":                email,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"instances": result})
}

// InstanceCreate represents a new instance request.
type InstanceCreate struct {
	Name       string `json:"name"`
	Type       string `json:"type"` // local | manual | standalone
	Host       string `json:"host"`
	GRPCPort   int    `json:"grpc_port"`
	CSRFToken  string `json:"csrf_token"`
	APIKey     string `json:"api_key"`
	Proxy      string `json:"proxy"`
	Weight     int    `json:"weight"`
	AccountID  string `json:"account_id"`
	BinaryPath string `json:"binary_path"`
	ServerPort int    `json:"server_port"`
}

// addInstance adds a new instance dynamically.
func (h *Handler) addInstance(w http.ResponseWriter, r *http.Request) {
	var body InstanceCreate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Check name uniqueness
	for _, inst := range h.balancer.GetInstances() {
		if inst.Name == body.Name {
			http.Error(w, fmt.Sprintf("Instance '%s' already exists", body.Name), http.StatusConflict)
			return
		}
	}
	// Create config entry
	instCfg := config.InstanceConfig{
		Name:         body.Name,
		Type:         body.Type,
		Host:         body.Host,
		GRPCPort:     body.GRPCPort,
		CSRFToken:    body.CSRFToken,
		APIKey:       body.APIKey,
		Proxy:        strings.TrimSpace(body.Proxy),
		Weight:       body.Weight,
		AccountID:    strings.TrimSpace(body.AccountID),
		BinaryPath:   body.BinaryPath,
		ServerPort:   body.ServerPort,
		AutoDiscover: body.Type == "local",
	}

	// Add to balancer using the correct method per type
	var addErr error
	switch body.Type {
	case "local":
		_, addErr = h.balancer.AddLocalInstance(instCfg)
	case "manual":
		_, addErr = h.balancer.AddManualInstance(instCfg)
	case "standalone":
		_, addErr = h.balancer.AddStandaloneInstance(instCfg)
	default:
		http.Error(w, fmt.Sprintf("Unknown instance type: %s", body.Type), http.StatusBadRequest)
		return
	}

	if addErr != nil {
		log.Printf("Failed to add instance '%s' (type=%s): %v", body.Name, body.Type, addErr)
		http.Error(w, fmt.Sprintf("Failed to add instance: %v", addErr), http.StatusInternalServerError)
		return
	}

	// Persist to config
	h.config.Instances = append(h.config.Instances, instCfg)
	cfgPath := h.configPath
	if cfgPath == "" {
		cfgPath = filepath.Join(config.GetConfigDir(), "config.yaml")
	}
	if err := config.Save(h.config, cfgPath); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "name": body.Name})
}

// handleInstance handles DELETE /api/instances/{name} and POST /api/instances/{name}/restart.
func (h *Handler) handleInstance(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// Extract name from path: /api/instances/{name} or /api/instances/{name}/restart
	parts := splitPath(path)
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	name := parts[2]

	if r.Method == http.MethodDelete {
		h.deleteInstance(w, r, name)
	} else if r.Method == http.MethodPost && len(parts) >= 4 && parts[3] == "restart" {
		h.restartInstance(w, r, name)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// deleteInstance removes an instance.
func (h *Handler) deleteInstance(w http.ResponseWriter, r *http.Request, name string) {
	found := false
	instances := h.balancer.GetInstances()
	for _, inst := range instances {
		if inst.Name == name {
			h.balancer.RemoveInstance(name)
			found = true
			break
		}
	}

	if !found {
		http.Error(w, fmt.Sprintf("Instance '%s' not found", name), http.StatusNotFound)
		return
	}

	// Remove from config
	newInstances := []config.InstanceConfig{}
	for _, cfg := range h.config.Instances {
		if cfg.Name != name {
			newInstances = append(newInstances, cfg)
		}
	}
	h.config.Instances = newInstances

	if err := h.saveConfig(); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "deleted": name})
}

// restartInstance restarts an instance.
func (h *Handler) restartInstance(w http.ResponseWriter, r *http.Request, name string) {
	inst := h.balancer.GetInstanceByName(name)
	if inst == nil {
		http.Error(w, fmt.Sprintf("Instance '%s' not found", name), http.StatusNotFound)
		return
	}

	if inst.Type == "local" {
		if err := h.balancer.RefreshLocalInstance(inst); err != nil {
			http.Error(w, fmt.Sprintf("Failed to refresh local instance: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "name": name, "healthy": true})
		return
	}

	inst.Healthy = true
	inst.ActiveConns = 0

	// Ping to check connection
	if inst.Client != nil {
		inst.Healthy = inst.Client.Ping()
	}

	if !inst.Healthy {
		http.Error(w, "Instance failed to restart", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "name": name, "healthy": true})
}

// ============================================================================
// API Key management
// ============================================================================

// handleKeys handles GET/POST /api/keys.
func (h *Handler) handleKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.listKeys(w, r)
	} else if r.Method == http.MethodPost {
		h.createKey(w, r)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listKeys returns all API keys (masked).
func (h *Handler) listKeys(w http.ResponseWriter, r *http.Request) {
	result := []map[string]interface{}{}
	for _, entry := range h.config.APIKeys {
		key := entry.Key
		masked := "****"
		if len(key) > 16 {
			masked = key[:8] + "..." + key[len(key)-4:]
		}
		result = append(result, map[string]interface{}{
			"id":             keyEntryID(key),
			"name":           entry.Name,
			"key_masked":     masked,
			"rate_limit":     entry.RateLimit,
			"allowed_models": entry.AllowedModels,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"keys": result})
}

// ApiKeyCreate represents a new API key request.
type ApiKeyCreate struct {
	Name          string   `json:"name"`
	RateLimit     int      `json:"rate_limit"`
	AllowedModels []string `json:"allowed_models"`
}

// createKey creates a new API key.
func (h *Handler) createKey(w http.ResponseWriter, r *http.Request) {
	var body ApiKeyCreate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		http.Error(w, "Key name is required", http.StatusBadRequest)
		return
	}
	if body.RateLimit <= 0 {
		body.RateLimit = 60
	}
	if len(body.AllowedModels) == 0 {
		body.AllowedModels = []string{"*"}
	}

	// Generate new key
	newKey := fmt.Sprintf("sk-wp-%s", generateHex(24))

	entry := config.APIKeyEntry{
		Key:           newKey,
		Name:          body.Name,
		RateLimit:     body.RateLimit,
		AllowedModels: body.AllowedModels,
	}

	h.config.APIKeys = append(h.config.APIKeys, entry)
	h.keys.AddKey(entry)

	if err := h.saveConfig(); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "key": newKey, "name": body.Name})
}

// handleKey handles PUT/DELETE /api/keys/{key_id}.
func (h *Handler) handleKey(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	keyID := parts[2]

	if r.Method == http.MethodPut {
		h.updateKey(w, r, keyID)
	} else if r.Method == http.MethodDelete {
		h.deleteKey(w, r, keyID)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ApiKeyUpdate represents an API key update request.
type ApiKeyUpdate struct {
	Name          string   `json:"name"`
	RateLimit     int      `json:"rate_limit"`
	AllowedModels []string `json:"allowed_models"`
}

// updateKey updates an API key's settings.
func (h *Handler) updateKey(w http.ResponseWriter, r *http.Request, keyID string) {
	var body ApiKeyUpdate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	realKey := h.resolveKeyID(keyID)
	if realKey == "" {
		http.Error(w, "API key not found", http.StatusNotFound)
		return
	}

	entry := h.keys.GetEntry(realKey)
	if entry == nil {
		http.Error(w, "API key not found", http.StatusNotFound)
		return
	}

	// Update entry
	if body.Name != "" {
		entry.Name = body.Name
	}
	if body.RateLimit > 0 {
		entry.RateLimit = body.RateLimit
	}
	if len(body.AllowedModels) > 0 {
		entry.AllowedModels = body.AllowedModels
	}

	// Update config
	for i, cfg := range h.config.APIKeys {
		if cfg.Key == realKey {
			h.config.APIKeys[i] = *entry
			break
		}
	}

	if err := h.saveConfig(); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// deleteKey deletes an API key.
func (h *Handler) deleteKey(w http.ResponseWriter, r *http.Request, keyID string) {
	realKey := h.resolveKeyID(keyID)
	if realKey == "" || h.keys.GetEntry(realKey) == nil {
		http.Error(w, "API key not found", http.StatusNotFound)
		return
	}

	h.keys.RemoveKey(realKey)

	// Remove from config
	newKeys := []config.APIKeyEntry{}
	for _, cfg := range h.config.APIKeys {
		if cfg.Key != realKey {
			newKeys = append(newKeys, cfg)
		}
	}
	h.config.APIKeys = newKeys

	if err := h.saveConfig(); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "deleted": keyID})
}

// ============================================================================
// Config / Stats
// ============================================================================

// handleConfig handles GET/PUT /api/config.
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.getConfig(w, r)
	} else if r.Method == http.MethodPut {
		h.updateConfig(w, r)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getConfig returns current configuration.
func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"server": map[string]interface{}{
			"host":      h.config.Server.Host,
			"port":      h.config.Server.Port,
			"log_level": h.config.Server.LogLevel,
		},
		"balancing": map[string]interface{}{
			"strategy":              h.config.Balancing.Strategy,
			"health_check_interval": h.config.Balancing.HealthCheckInterval,
			"max_retries":           h.config.Balancing.MaxRetries,
			"retry_delay":           h.config.Balancing.RetryDelay,
		},
		"logging": map[string]interface{}{
			"audit": h.config.Logging.Audit,
		},
		"instance_count": len(h.config.Instances),
		"account_count":  len(h.config.Accounts),
		"api_key_count":  len(h.config.APIKeys),
	})
}

// updateConfig updates server/balancing configuration.
func (h *Handler) updateConfig(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	prevServer := h.config.Server
	prevLogging := h.config.Logging

	if server, ok := body["server"].(map[string]interface{}); ok {
		if host, ok := server["host"].(string); ok {
			h.config.Server.Host = host
		}
		if port, ok := server["port"].(float64); ok {
			h.config.Server.Port = int(port)
		}
		if logLevel, ok := server["log_level"].(string); ok {
			h.config.Server.LogLevel = logLevel
		}
	}

	if balancing, ok := body["balancing"].(map[string]interface{}); ok {
		if strategy, ok := balancing["strategy"].(string); ok {
			h.config.Balancing.Strategy = strategy
		}
		if interval, ok := balancing["health_check_interval"].(float64); ok {
			h.config.Balancing.HealthCheckInterval = int(interval)
		}
		if maxRetries, ok := balancing["max_retries"].(float64); ok {
			h.config.Balancing.MaxRetries = int(maxRetries)
		}
		if retryDelay, ok := balancing["retry_delay"].(float64); ok {
			h.config.Balancing.RetryDelay = retryDelay
		}
	}

	if logging, ok := body["logging"].(map[string]interface{}); ok {
		if v, ok := logging["audit"].(bool); ok {
			h.config.Logging.Audit = v
		}
	}

	if err := h.saveConfig(); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	if h.onServerConfigChanged != nil &&
		(prevServer.Host != h.config.Server.Host || prevServer.Port != h.config.Server.Port) {
		go h.onServerConfigChanged(prevServer, h.config.Server)
	}
	if h.onLoggingChanged != nil && prevLogging != h.config.Logging {
		go h.onLoggingChanged(h.config.Logging)
	}
}

// handleStats returns runtime statistics.
func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.startTime).Seconds()
	instances := h.balancer.GetInstances()

	totalConns := 0
	healthyCount := 0
	for _, inst := range instances {
		totalConns += inst.ActiveConns
		if inst.Healthy {
			healthyCount++
		}
	}

	h.historyMu.Lock()
	totalReq := h.totalRequests
	h.historyMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uptime_seconds":     int(uptime),
		"total_requests":     totalReq,
		"active_connections": totalConns,
		"instance_count":     len(instances),
		"account_count":      len(h.config.Accounts),
		"healthy_count":      healthyCount,
		"model_count":        len(core.GetSupportedModels()),
		"config_path":        h.configPath,
	})
}

// handleModels returns all supported model names.
func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	models := core.GetSupportedModels()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"models": models})
}

// ============================================================================
// Request history
// ============================================================================

// RequestRecord represents a logged request.
type RequestRecord struct {
	ID               string `json:"id"`
	Timestamp        int64  `json:"timestamp"`
	TimeStr          string `json:"time_str"`
	Model            string `json:"model"`
	Instance         string `json:"instance"`
	Account          string `json:"account"`
	Stream           bool   `json:"stream"`
	Status           string `json:"status"`
	DurationMs       int    `json:"duration_ms"`
	Turns            int    `json:"turns"`
	PromptChars      int    `json:"prompt_chars"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Error            string `json:"error,omitempty"`
}

// RecordRequest logs a request to history.
func (h *Handler) RecordRequest(
	model, instance, account string,
	stream bool,
	status string,
	durationMs, turns, promptChars, promptTokens, completionTokens, totalTokens int,
	err string,
) {
	rec := RequestRecord{
		ID:               generateHex(12),
		Timestamp:        time.Now().Unix(),
		TimeStr:          time.Now().Format("2006-01-02 15:04:05"),
		Model:            model,
		Instance:         instance,
		Account:          account,
		Stream:           stream,
		Status:           status,
		DurationMs:       durationMs,
		Turns:            turns,
		PromptChars:      promptChars,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		Error:            err,
	}

	h.historyMu.Lock()
	h.totalRequests++
	h.requestHistory.PushFront(rec)
	if h.requestHistory.Len() > h.maxHistory {
		h.requestHistory.Remove(h.requestHistory.Back())
	}
	h.historyMu.Unlock()
}

// handleRequests returns recent request history.
func (h *Handler) handleRequests(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := parseLimit(l); err == nil && n > 0 {
			limit = n
		}
	}

	h.historyMu.Lock()
	items := []RequestRecord{}
	for e := h.requestHistory.Front(); e != nil && len(items) < limit; e = e.Next() {
		items = append(items, e.Value.(RequestRecord))
	}
	h.historyMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"requests": items})
}

// ============================================================================
// WebSocket logs
// ============================================================================

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleLogs handles WebSocket /api/logs.
func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebSocket] Upgrade failed: %v", err)
		return
	}

	// Initialize log hook on first client connection
	InitLogHook(h)

	h.logClientsMu.Lock()
	h.logClients[conn] = true
	h.logClientsMu.Unlock()

	log.Printf("[WebSocket] 客户端已连接 - 实时日志监控已启用")

	// Send welcome message in Chinese
	welcome := map[string]interface{}{
		"time":    time.Now().Format("15:04:05"),
		"level":   "信息",
		"message": "Windsurf API Proxy - 实时日志监控已启动",
	}
	conn.WriteJSON(welcome)

	// Read loop (keep connection alive)
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if string(msg) == "ping" {
			conn.WriteMessage(websocket.TextMessage, []byte("pong"))
		}
	}

	h.logClientsMu.Lock()
	delete(h.logClients, conn)
	h.logClientsMu.Unlock()

	conn.Close()
	log.Printf("[WebSocket] 客户端已断开")
}

// BroadcastLog sends a log message to all WebSocket clients.
// Uses user-friendly Chinese translation.
func (h *Handler) BroadcastLog(level, message string) {
	h.BroadcastUserLog(level, message)
}

// ============================================================================
// Helpers
// ============================================================================

func splitPath(path string) []string {
	parts := []string{}
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func (h *Handler) localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRequest(r.RemoteAddr) {
			http.Error(w, "Management API is only available from localhost", http.StatusForbidden)
			return
		}

		// Allow cross-origin from the Vite dev server (wails dev) and any other
		// loopback origin. Since the request must already come from loopback,
		// reflecting the Origin header is safe.
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			reqHeaders := r.Header.Get("Access-Control-Request-Headers")
			if reqHeaders == "" {
				reqHeaders = "Content-Type, Authorization"
			}
			w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next(w, r)
	}
}

func isLoopbackRequest(remoteAddr string) bool {
	host := remoteAddr
	if parsedHost, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *Handler) saveConfig() error {
	return config.Save(h.config, h.effectiveConfigPath())
}

func (h *Handler) effectiveConfigPath() string {
	if h.configPath != "" {
		return h.configPath
	}
	return filepath.Join(config.GetConfigDir(), "config.yaml")
}

func (h *Handler) resolveKeyID(id string) string {
	for _, entry := range h.config.APIKeys {
		if entry.Key == id || keyEntryID(entry.Key) == id {
			return entry.Key
		}
	}
	return ""
}

func getInstanceConfig(name string, cfg *config.Config) *config.InstanceConfig {
	for i := range cfg.Instances {
		if cfg.Instances[i].Name == name {
			return &cfg.Instances[i]
		}
	}
	return nil
}

func getAccountConfig(id string, cfg *config.Config) *config.AccountConfig {
	if id == "" {
		return nil
	}
	for i := range cfg.Accounts {
		if cfg.Accounts[i].ID == id {
			return &cfg.Accounts[i]
		}
	}
	return nil
}

func authSourceForConfig(cfg *config.InstanceConfig, accountMgr *accounts.Manager) string {
	if cfg == nil {
		return ""
	}
	if cfg.AccountID != "" && accountMgr != nil {
		if account := accountMgr.Get(cfg.AccountID); account != nil {
			return accounts.AuthSource(*account)
		}
	}
	if cfg.Type == "local" || cfg.AutoDiscover {
		return "auto_discover"
	}
	if cfg.APIKey != "" {
		return "api_key"
	}
	return ""
}

func generateHex(n int) string {
	if n <= 0 {
		return ""
	}

	bytesLen := (n + 1) / 2
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Errorf("generate random key: %w", err))
	}

	return hex.EncodeToString(buf)[:n]
}

func keyEntryID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "key_" + hex.EncodeToString(sum[:8])
}

func sortStrings(s []string) {
	for i := 0; i < len(s)-1; i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func parseLimit(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			return 0, fmt.Errorf("invalid")
		}
	}
	return n, nil
}
