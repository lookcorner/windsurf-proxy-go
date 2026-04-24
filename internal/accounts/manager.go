// Package accounts manages standalone Windsurf accounts independently from
// runtime instances.
package accounts

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"windsurf-proxy-go/internal/auth"
	"windsurf-proxy-go/internal/config"
	"windsurf-proxy-go/internal/core"
	coregrpc "windsurf-proxy-go/internal/core/grpc"
)

// View is the management-API shape exposed to the desktop UI.
type View struct {
	ID                          string   `json:"id"`
	Name                        string   `json:"name"`
	Provider                    string   `json:"provider"`
	Status                      string   `json:"status"`
	Email                       string   `json:"email"`
	AuthSource                  string   `json:"auth_source"`
	HasPassword                 bool     `json:"has_password"`
	HasRefreshToken             bool     `json:"has_refresh_token"`
	HasAPIKey                   bool     `json:"has_api_key"`
	KeyMasked                   string   `json:"key_masked"`
	APIServer                   string   `json:"api_server"`
	Proxy                       string   `json:"proxy"`
	AvailableModels             []string `json:"available_models"`
	SyncedAvailableModels       []string `json:"synced_available_models"`
	BlockedModels               []string `json:"blocked_models"`
	AttachedInstances           int      `json:"attached_instances"`
	Healthy                     bool     `json:"healthy"`
	ActiveRequests              int      `json:"active_requests"`
	TotalRequests               int      `json:"total_requests"`
	ConsecutiveFails            int      `json:"consecutive_failures"`
	LastError                   string   `json:"last_error"`
	LastUsedUnix                int64    `json:"last_used_unix"`
	UsageStatus                 string   `json:"usage_status"`
	QuotaExhausted              bool     `json:"quota_exhausted"`
	QuotaLow                    bool     `json:"quota_low"`
	LowestQuotaPercent          int      `json:"lowest_quota_percent"`
	PlanName                    string   `json:"plan_name"`
	UsedPromptCredits           int64    `json:"used_prompt_credits"`
	UsedFlowCredits             int64    `json:"used_flow_credits"`
	AvailablePromptCredits      int      `json:"available_prompt_credits"`
	AvailableFlowCredits        int      `json:"available_flow_credits"`
	DailyQuotaRemainingPercent  int      `json:"daily_quota_remaining_percent"`
	WeeklyQuotaRemainingPercent int      `json:"weekly_quota_remaining_percent"`
	DailyQuotaResetUnix         int64    `json:"daily_quota_reset_unix"`
	WeeklyQuotaResetUnix        int64    `json:"weekly_quota_reset_unix"`
	HideDailyQuota              bool     `json:"hide_daily_quota"`
	HideWeeklyQuota             bool     `json:"hide_weekly_quota"`
	LastUsageCheckUnix          int64    `json:"last_usage_check_unix"`
	LastModelSyncUnix           int64    `json:"last_model_sync_unix"`
	UsageError                  string   `json:"usage_error"`
}

// Resolved contains the credentials needed to start a standalone runtime.
type Resolved struct {
	Account        config.AccountConfig
	APIKey         string
	APIServer      string
	AuthSource     string
	FirebaseTokens *auth.FirebaseTokens
	ServiceAuth    *auth.WindsurfServiceAuth
}

// Manager owns the configured standalone accounts.
type Manager struct {
	cfg           *config.Config
	mu            sync.RWMutex
	runtime       map[string]*RuntimeState
	tokenManagers map[string]*auth.TokenManager
	persistHook   func()
}

// RuntimeState holds live scheduling signals for an account.
type RuntimeState struct {
	Healthy                     bool
	ActiveRequests              int
	TotalRequests               int
	ConsecutiveFails            int
	LastError                   string
	LastUsed                    time.Time
	LastResolved                time.Time
	BlockedUntil                time.Time
	RateLimitedUntil            time.Time
	ModelRateLimitedUntil       map[string]time.Time
	ModelUnavailableUntil       map[string]time.Time
	UsageStatus                 string
	QuotaExhausted              bool
	QuotaLow                    bool
	LowestQuotaPercent          int
	UsageBlockedUntil           time.Time
	PlanName                    string
	UsedPromptCredits           int64
	UsedFlowCredits             int64
	AvailablePromptCredits      int
	AvailableFlowCredits        int
	DailyQuotaRemainingPercent  int
	WeeklyQuotaRemainingPercent int
	DailyQuotaResetUnix         int64
	WeeklyQuotaResetUnix        int64
	HideDailyQuota              bool
	HideWeeklyQuota             bool
	LastUsageCheck              time.Time
	LastModelSync               time.Time
	UsageError                  string
}

type UsageSnapshot struct {
	PlanName                    string
	UsedPromptCredits           int64
	UsedFlowCredits             int64
	AvailablePromptCredits      int
	AvailableFlowCredits        int
	DailyQuotaRemainingPercent  int
	WeeklyQuotaRemainingPercent int
	DailyQuotaResetUnix         int64
	WeeklyQuotaResetUnix        int64
	HideDailyQuota              bool
	HideWeeklyQuota             bool
	PromptCreditsPercent        int
	FlowCreditsPercent          int
	QuotaExhausted              bool
	QuotaLow                    bool
	LowestQuotaPercent          int
}

const (
	requestResolveTimeout = 6 * time.Second
	UsageRefreshTTL       = 10 * time.Minute
	QuotaRetryCooldown    = 10 * time.Minute
	QuotaLowThreshold     = 20
	maxIdleScoreSeconds   = 3600
)

var (
	fullLoginFunc            = auth.FullLoginWithProxy
	refreshFirebaseTokenFunc = auth.RefreshFirebaseTokenWithProxy
	registerWindsurfUserFunc = auth.RegisterWindsurfUserWithProxy
)

// NewManager creates a manager backed by the shared config object.
func NewManager(cfg *config.Config) *Manager {
	if cfg.Accounts == nil {
		cfg.Accounts = []config.AccountConfig{}
	}
	m := &Manager{
		cfg:           cfg,
		runtime:       make(map[string]*RuntimeState),
		tokenManagers: make(map[string]*auth.TokenManager),
	}
	for i := range cfg.Accounts {
		cfg.Accounts[i].AvailableModels = normalizeModelList(cfg.Accounts[i].AvailableModels)
		cfg.Accounts[i].SyncedAvailableModels = normalizeModelList(cfg.Accounts[i].SyncedAvailableModels)
		cfg.Accounts[i].BlockedModels = normalizeModelList(cfg.Accounts[i].BlockedModels)
		m.ensureRuntimeLocked(cfg.Accounts[i].ID, normalizeStatus(cfg.Accounts[i].Status) == "active")
	}
	return m
}

// Stop halts background token refreshers owned by accounts.
func (m *Manager) Stop() {
	if m == nil {
		return
	}

	m.mu.Lock()
	managers := make([]*auth.TokenManager, 0, len(m.tokenManagers))
	for id, tm := range m.tokenManagers {
		if tm != nil {
			managers = append(managers, tm)
		}
		delete(m.tokenManagers, id)
	}
	m.mu.Unlock()

	for _, tm := range managers {
		tm.Stop()
	}
}

// SetPersistHook registers a callback used when refreshed credentials should be
// persisted to disk.
func (m *Manager) SetPersistHook(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistHook = fn
}

// List returns all configured accounts with derived metadata for the UI.
func (m *Manager) List(instances []config.InstanceConfig) []View {
	m.mu.RLock()
	defer m.mu.RUnlock()

	attached := make(map[string]int)
	for _, inst := range instances {
		if inst.AccountID != "" {
			attached[inst.AccountID]++
		}
	}

	result := make([]View, 0, len(m.cfg.Accounts))
	for _, entry := range m.cfg.Accounts {
		runtime := RuntimeState{Healthy: normalizeStatus(entry.Status) == "active"}
		if state, ok := m.runtime[entry.ID]; ok && state != nil {
			runtime = *state
		}
		result = append(result, View{
			ID:                          entry.ID,
			Name:                        entry.Name,
			Provider:                    providerForAccount(entry),
			Status:                      normalizeStatus(entry.Status),
			Email:                       entry.Email,
			AuthSource:                  AuthSource(entry),
			HasPassword:                 entry.Password != "",
			HasRefreshToken:             entry.FirebaseRefreshToken != "",
			HasAPIKey:                   entry.APIKey != "",
			KeyMasked:                   maskKey(entry.APIKey),
			APIServer:                   entry.APIServer,
			Proxy:                       maskProxy(entry.Proxy),
			AvailableModels:             cloneStrings(entry.AvailableModels),
			SyncedAvailableModels:       cloneStrings(entry.SyncedAvailableModels),
			BlockedModels:               cloneStrings(entry.BlockedModels),
			AttachedInstances:           attached[entry.ID],
			Healthy:                     runtime.Healthy,
			ActiveRequests:              runtime.ActiveRequests,
			TotalRequests:               runtime.TotalRequests,
			ConsecutiveFails:            runtime.ConsecutiveFails,
			LastError:                   runtime.LastError,
			LastUsedUnix:                runtime.LastUsed.Unix(),
			UsageStatus:                 usageStatusOrDefault(runtime.UsageStatus),
			QuotaExhausted:              runtime.QuotaExhausted,
			QuotaLow:                    runtime.QuotaLow,
			LowestQuotaPercent:          runtime.LowestQuotaPercent,
			PlanName:                    runtime.PlanName,
			UsedPromptCredits:           runtime.UsedPromptCredits,
			UsedFlowCredits:             runtime.UsedFlowCredits,
			AvailablePromptCredits:      runtime.AvailablePromptCredits,
			AvailableFlowCredits:        runtime.AvailableFlowCredits,
			DailyQuotaRemainingPercent:  runtime.DailyQuotaRemainingPercent,
			WeeklyQuotaRemainingPercent: runtime.WeeklyQuotaRemainingPercent,
			DailyQuotaResetUnix:         runtime.DailyQuotaResetUnix,
			WeeklyQuotaResetUnix:        runtime.WeeklyQuotaResetUnix,
			HideDailyQuota:              runtime.HideDailyQuota,
			HideWeeklyQuota:             runtime.HideWeeklyQuota,
			LastUsageCheckUnix:          runtime.LastUsageCheck.Unix(),
			LastModelSyncUnix:           runtime.LastModelSync.Unix(),
			UsageError:                  runtime.UsageError,
		})
	}

	return result
}

// Get returns a copy of the account config for the given ID.
func (m *Manager) Get(id string) *config.AccountConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.cfg.Accounts {
		if m.cfg.Accounts[i].ID == id {
			entry := m.cfg.Accounts[i]
			return &entry
		}
	}
	return nil
}

// Add appends a new account config after normalizing it.
func (m *Manager) Add(entry config.AccountConfig) (config.AccountConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry.ID = strings.TrimSpace(entry.ID)
	if entry.ID == "" {
		return config.AccountConfig{}, fmt.Errorf("account id is required")
	}
	if strings.TrimSpace(entry.Name) == "" {
		return config.AccountConfig{}, fmt.Errorf("account name is required")
	}
	for _, existing := range m.cfg.Accounts {
		if existing.ID == entry.ID {
			return config.AccountConfig{}, fmt.Errorf("account %q already exists", entry.ID)
		}
	}

	entry.Name = strings.TrimSpace(entry.Name)
	entry.Email = strings.TrimSpace(entry.Email)
	entry.APIServer = strings.TrimSpace(entry.APIServer)
	entry.AvailableModels = normalizeModelList(entry.AvailableModels)
	entry.SyncedAvailableModels = normalizeModelList(entry.SyncedAvailableModels)
	entry.BlockedModels = normalizeModelList(entry.BlockedModels)
	if err := validateCredentials(entry); err != nil {
		return config.AccountConfig{}, err
	}
	entry.Status = normalizeStatus(entry.Status)
	m.cfg.Accounts = append(m.cfg.Accounts, entry)
	m.ensureRuntimeLocked(entry.ID, entry.Status == "active")

	return entry, nil
}

// Remove deletes an account if no instance currently references it.
func (m *Manager) Remove(id string, instances []config.InstanceConfig) error {
	m.mu.Lock()

	for _, inst := range instances {
		if inst.AccountID == id && inst.Type != "standalone" {
			m.mu.Unlock()
			return fmt.Errorf("account %q is still attached to instance %q", id, inst.Name)
		}
	}

	var tm *auth.TokenManager
	for i := range m.cfg.Accounts {
		if m.cfg.Accounts[i].ID == id {
			m.cfg.Accounts = append(m.cfg.Accounts[:i], m.cfg.Accounts[i+1:]...)
			delete(m.runtime, id)
			tm = m.tokenManagers[id]
			delete(m.tokenManagers, id)
			m.mu.Unlock()
			if tm != nil {
				tm.Stop()
			}
			return nil
		}
	}
	m.mu.Unlock()
	return fmt.Errorf("account %q not found", id)
}

// ResolveStandalone turns an account record into runtime credentials.
func (m *Manager) ResolveStandalone(ctx context.Context, id string) (*Resolved, error) {
	entry := m.Get(id)
	if entry == nil {
		return nil, fmt.Errorf("account %q not found", id)
	}
	if normalizeStatus(entry.Status) != "active" {
		return nil, fmt.Errorf("account %q is not active", id)
	}

	resolved := &Resolved{
		Account:    *entry,
		APIKey:     entry.APIKey,
		APIServer:  entry.APIServer,
		AuthSource: AuthSource(*entry),
	}

	serviceAuth, firebaseTokens, source, err := resolveFreshCredentials(ctx, *entry)
	if err != nil {
		return nil, err
	}

	switch source {
	case "password", "refresh_token":
		resolved.ServiceAuth = serviceAuth
		resolved.FirebaseTokens = firebaseTokens
		resolved.APIKey = serviceAuth.APIKey
		resolved.APIServer = serviceAuth.APIServerURL
		resolved.AuthSource = source
		m.UpdateResolved(id, firebaseTokens, serviceAuth)
	case "api_key":
		if resolved.APIKey == "" {
			return nil, fmt.Errorf("account %q has no api key", id)
		}
	default:
		if resolved.APIKey == "" {
			return nil, fmt.Errorf("account %q has no usable credentials", id)
		}
		resolved.AuthSource = "api_key"
	}

	if resolved.APIServer == "" {
		resolved.APIServer = auth.DefaultAPIServerURL
	}
	m.ensureTokenManager(id, resolved.FirebaseTokens, resolved.ServiceAuth, resolved.Account.Proxy)

	return resolved, nil
}

// UpdateResolved persists the most recent service credentials back into config.
func (m *Manager) UpdateResolved(id string, tokens *auth.FirebaseTokens, service *auth.WindsurfServiceAuth) {
	m.mu.Lock()
	var persistHook func()

	for i := range m.cfg.Accounts {
		if m.cfg.Accounts[i].ID != id {
			continue
		}
		if tokens != nil {
			if tokens.RefreshToken != "" {
				m.cfg.Accounts[i].FirebaseRefreshToken = tokens.RefreshToken
			}
			if tokens.Email != "" && m.cfg.Accounts[i].Email == "" {
				m.cfg.Accounts[i].Email = tokens.Email
			}
		}
		if service != nil {
			if service.APIKey != "" {
				m.cfg.Accounts[i].APIKey = service.APIKey
			}
			if service.APIServerURL != "" {
				m.cfg.Accounts[i].APIServer = service.APIServerURL
			}
			if service.Name != "" && strings.TrimSpace(m.cfg.Accounts[i].Name) == "" {
				m.cfg.Accounts[i].Name = service.Name
			}
		}
		m.cfg.Accounts[i].Status = normalizeStatus(m.cfg.Accounts[i].Status)
		state := m.ensureRuntimeLocked(id, m.cfg.Accounts[i].Status == "active")
		state.LastResolved = time.Now()
		state.Healthy = true
		persistHook = m.persistHook
		break
	}
	m.mu.Unlock()

	if persistHook != nil {
		persistHook()
	}
}

// Runtime returns a copy of the current runtime state for an account.
func (m *Manager) Runtime(id string) RuntimeState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if state, ok := m.runtime[id]; ok && state != nil {
		return *state
	}
	if cfg := m.getLocked(id); cfg != nil {
		return RuntimeState{Healthy: normalizeStatus(cfg.Status) == "active"}
	}
	return RuntimeState{}
}

// Select returns the best schedulable account that is not excluded.
func (m *Manager) Select(exclude map[string]bool, modelKey string) *config.AccountConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var best *config.AccountConfig
	bestScore := -1 << 30
	bestAffinity := -1
	now := time.Now()
	modelKey = core.CanonicalModelKey(modelKey)
	for i := range m.cfg.Accounts {
		entry := m.cfg.Accounts[i]
		if exclude != nil && exclude[entry.ID] {
			continue
		}
		if normalizeStatus(entry.Status) != "active" {
			continue
		}

		state := RuntimeState{Healthy: true}
		if runtime, ok := m.runtime[entry.ID]; ok && runtime != nil {
			state = *runtime
		}
		if !state.BlockedUntil.IsZero() && now.Before(state.BlockedUntil) {
			continue
		}
		if !state.RateLimitedUntil.IsZero() && now.Before(state.RateLimitedUntil) {
			continue
		}
		if modelKey != "" && isModelTemporarilyBlocked(state, modelKey, now) {
			continue
		}
		if state.QuotaExhausted && !state.UsageBlockedUntil.IsZero() && now.Before(state.UsageBlockedUntil) {
			continue
		}
		if !state.Healthy {
			continue
		}
		if modelKey != "" && !isModelAllowedForAccount(entry, state, modelKey, now) {
			continue
		}

		score := accountScore(state)
		affinity := modelAffinity(entry, modelKey)
		if best == nil || affinity > bestAffinity || (affinity == bestAffinity && score > bestScore) {
			copy := entry
			best = &copy
			bestScore = score
			bestAffinity = affinity
		}
	}

	return best
}

// ActiveCount returns the number of configured accounts that can participate in
// routing before transient runtime exclusions like rate limits are applied.
func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, entry := range m.cfg.Accounts {
		if normalizeStatus(entry.Status) == "active" {
			count++
		}
	}
	return count
}

// IsSchedulable reports whether the account can currently be selected.
func (m *Manager) IsSchedulable(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg := m.getLocked(id)
	if cfg == nil || normalizeStatus(cfg.Status) != "active" {
		return false
	}
	if state, ok := m.runtime[id]; ok && state != nil {
		if !state.BlockedUntil.IsZero() && time.Now().Before(state.BlockedUntil) {
			return false
		}
		if !state.RateLimitedUntil.IsZero() && time.Now().Before(state.RateLimitedUntil) {
			return false
		}
		if state.QuotaExhausted && !state.UsageBlockedUntil.IsZero() && time.Now().Before(state.UsageBlockedUntil) {
			return false
		}
		return state.Healthy
	}
	return true
}

// IsSchedulableForModel reports whether an account can currently serve a
// specific model, taking static filters and temporary per-model blocks into account.
func (m *Manager) IsSchedulableForModel(id string, modelKey string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg := m.getLocked(id)
	if cfg == nil || normalizeStatus(cfg.Status) != "active" {
		return false
	}
	state := RuntimeState{Healthy: true}
	if current, ok := m.runtime[id]; ok && current != nil {
		state = *current
	}
	now := time.Now()
	if !state.BlockedUntil.IsZero() && now.Before(state.BlockedUntil) {
		return false
	}
	if !state.RateLimitedUntil.IsZero() && now.Before(state.RateLimitedUntil) {
		return false
	}
	if state.QuotaExhausted && !state.UsageBlockedUntil.IsZero() && now.Before(state.UsageBlockedUntil) {
		return false
	}
	if !state.Healthy {
		return false
	}
	return isModelAllowedForAccount(*cfg, state, modelKey, now)
}

// ResolveForRequest returns credentials for per-request routing. When an API
// key is already cached on the account, it is reused directly; otherwise the
// account is resolved via the standalone login flow once and the fresh
// credentials are persisted back into config.
func (m *Manager) ResolveForRequest(ctx context.Context, id string) (*Resolved, error) {
	entry := m.Get(id)
	if entry == nil {
		return nil, fmt.Errorf("account %q not found", id)
	}
	if normalizeStatus(entry.Status) != "active" {
		return nil, fmt.Errorf("account %q is not active", id)
	}
	if strings.TrimSpace(entry.APIKey) != "" {
		resolved := &Resolved{
			Account:    *entry,
			APIKey:     strings.TrimSpace(entry.APIKey),
			APIServer:  strings.TrimSpace(entry.APIServer),
			AuthSource: AuthSource(*entry),
		}
		if resolved.APIServer == "" {
			resolved.APIServer = auth.DefaultAPIServerURL
		}
		m.ensureTokenManagerFromCached(entry, resolved)
		return resolved, nil
	}
	resolveCtx, cancel := context.WithTimeout(ctx, requestResolveTimeout)
	defer cancel()
	return m.ResolveStandalone(resolveCtx, id)
}

func (m *Manager) ensureTokenManagerFromCached(entry *config.AccountConfig, resolved *Resolved) {
	if entry == nil || resolved == nil {
		return
	}
	refreshToken := strings.TrimSpace(entry.FirebaseRefreshToken)
	if refreshToken == "" || strings.TrimSpace(resolved.APIKey) == "" {
		return
	}
	tokens := &auth.FirebaseTokens{
		RefreshToken: refreshToken,
		ExpiresIn:    3600,
		Email:        strings.TrimSpace(entry.Email),
	}
	service := &auth.WindsurfServiceAuth{
		APIKey:       strings.TrimSpace(resolved.APIKey),
		Name:         strings.TrimSpace(entry.Name),
		APIServerURL: strings.TrimSpace(resolved.APIServer),
	}
	m.ensureTokenManager(entry.ID, tokens, service, entry.Proxy)
}

func (m *Manager) ensureTokenManager(id string, tokens *auth.FirebaseTokens, service *auth.WindsurfServiceAuth, proxyURL string) {
	if m == nil || strings.TrimSpace(id) == "" || tokens == nil || service == nil {
		return
	}
	if strings.TrimSpace(tokens.RefreshToken) == "" || strings.TrimSpace(service.APIKey) == "" {
		return
	}

	m.mu.Lock()
	if m.tokenManagers == nil {
		m.tokenManagers = make(map[string]*auth.TokenManager)
	}
	if m.tokenManagers[id] != nil {
		m.mu.Unlock()
		return
	}
	tm := auth.NewTokenManager(tokens, service, strings.TrimSpace(proxyURL), nil, func(tokens *auth.FirebaseTokens, service *auth.WindsurfServiceAuth) {
		m.UpdateResolved(id, tokens, service)
	})
	m.tokenManagers[id] = tm
	m.mu.Unlock()

	tm.Start()
	log.Printf("[Accounts] Token refresh manager started for account '%s'", id)
}

// MarkAcquire records the start of a request routed to this account.
func (m *Manager) MarkAcquire(id string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	state.ActiveRequests++
	state.TotalRequests++
	state.LastUsed = time.Now()
}

// MarkRelease decrements active requests for an account.
func (m *Manager) MarkRelease(id string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	if state.ActiveRequests > 0 {
		state.ActiveRequests--
	}
}

// MarkSuccess records a successful request.
func (m *Manager) MarkSuccess(id string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	state.Healthy = true
	state.ConsecutiveFails = 0
	state.LastError = ""
	state.LastUsed = time.Now()
	state.BlockedUntil = time.Time{}
}

// MarkError records a failed request. After repeated failures the account is
// temporarily marked unhealthy until a later success.
func (m *Manager) MarkError(id string, err string, threshold int) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	state.ConsecutiveFails++
	state.LastError = strings.TrimSpace(err)
	state.LastUsed = time.Now()
	if threshold > 0 && state.ConsecutiveFails >= threshold {
		state.Healthy = false
	}
}

// Block temporarily skips an account from scheduling without permanently
// disabling it. Used when resolving fresh credentials is timing out.
func (m *Manager) Block(id string, err string, cooldown time.Duration) {
	if id == "" || cooldown <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	state.LastError = strings.TrimSpace(err)
	state.LastUsed = time.Now()
	state.BlockedUntil = time.Now().Add(cooldown)
}

// MarkRateLimited temporarily removes an account from scheduling without
// conflating it with quota exhaustion or transport health.
func (m *Manager) MarkRateLimited(id string, err string, cooldown time.Duration) {
	if id == "" || cooldown <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	state.LastError = strings.TrimSpace(err)
	state.LastUsed = time.Now()
	state.RateLimitedUntil = time.Now().Add(cooldown)
}

// MarkModelRateLimited temporarily removes one model on an account from
// scheduling while leaving other models available.
func (m *Manager) MarkModelRateLimited(id string, modelKey string, err string, cooldown time.Duration) {
	if id == "" || cooldown <= 0 {
		return
	}
	modelKey = core.CanonicalModelKey(modelKey)
	if modelKey == "" {
		m.MarkRateLimited(id, err, cooldown)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	if state.ModelRateLimitedUntil == nil {
		state.ModelRateLimitedUntil = make(map[string]time.Time)
	}
	until := time.Now().Add(cooldown)
	state.LastError = strings.TrimSpace(err)
	state.LastUsed = time.Now()
	state.ModelRateLimitedUntil[modelKey] = until
}

// MarkModelUnavailable temporarily removes one model family on an account from
// scheduling after the upstream reports the model is unsupported or not entitled.
func (m *Manager) MarkModelUnavailable(id string, modelKey string, err string, cooldown time.Duration) {
	if id == "" || cooldown <= 0 {
		return
	}
	modelKey = core.CanonicalModelKey(modelKey)
	if modelKey == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	if state.ModelUnavailableUntil == nil {
		state.ModelUnavailableUntil = make(map[string]time.Time)
	}
	until := time.Now().Add(cooldown)
	state.LastError = strings.TrimSpace(err)
	state.LastUsed = time.Now()
	state.ModelUnavailableUntil[modelKey] = until
	if family := core.ModelFamilyKey(modelKey); family != "" && family != modelKey {
		state.ModelUnavailableUntil[family] = until
	}
}

// ShouldRefreshUsage reports whether a fresh GetUserStatus call should be made.
func (m *Manager) ShouldRefreshUsage(id string, ttl time.Duration) bool {
	if id == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	state := m.runtime[id]
	if state == nil {
		return true
	}
	if ttl <= 0 {
		return true
	}
	return state.LastUsageCheck.IsZero() || time.Since(state.LastUsageCheck) >= ttl
}

// UpdateUsage stores the last successfully fetched upstream usage state.
func (m *Manager) UpdateUsage(id string, usage UsageSnapshot, cooldown time.Duration) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	wasLow := state.QuotaLow
	wasExhausted := state.QuotaExhausted
	state.UsageStatus = "ok"
	state.QuotaExhausted = usage.QuotaExhausted
	state.QuotaLow = usage.QuotaLow
	state.LowestQuotaPercent = usage.LowestQuotaPercent
	state.PlanName = strings.TrimSpace(usage.PlanName)
	state.UsedPromptCredits = usage.UsedPromptCredits
	state.UsedFlowCredits = usage.UsedFlowCredits
	state.AvailablePromptCredits = usage.AvailablePromptCredits
	state.AvailableFlowCredits = usage.AvailableFlowCredits
	state.DailyQuotaRemainingPercent = usage.DailyQuotaRemainingPercent
	state.WeeklyQuotaRemainingPercent = usage.WeeklyQuotaRemainingPercent
	state.DailyQuotaResetUnix = usage.DailyQuotaResetUnix
	state.WeeklyQuotaResetUnix = usage.WeeklyQuotaResetUnix
	state.HideDailyQuota = usage.HideDailyQuota
	state.HideWeeklyQuota = usage.HideWeeklyQuota
	state.LastUsageCheck = time.Now()
	state.UsageError = ""
	if usage.QuotaExhausted && cooldown > 0 {
		state.UsageStatus = "exhausted"
		state.UsageBlockedUntil = time.Now().Add(cooldown)
	} else {
		state.UsageBlockedUntil = time.Time{}
	}
	if !wasExhausted && usage.QuotaExhausted {
		log.Printf("[Accounts] Account '%s' quota exhausted", id)
	} else if !wasLow && usage.QuotaLow {
		log.Printf("[Accounts] Account '%s' quota low (%d%% remaining)", id, usage.LowestQuotaPercent)
	}
}

// SyncAllowedModels stores the latest authoritative allowlist returned by
// GetUserStatus. The synced list is advisory: manual available_models still
// remain the only hard allowlist.
func (m *Manager) SyncAllowedModels(id string, models []string) {
	if id == "" {
		return
	}

	models = normalizeModelList(models)
	sort.Strings(models)

	var persistHook func()
	m.mu.Lock()
	state := m.ensureRuntimeLocked(id, true)
	state.LastModelSync = time.Now()

	if len(models) == 0 {
		m.mu.Unlock()
		return
	}

	entry := m.getLocked(id)
	if entry == nil {
		m.mu.Unlock()
		return
	}
	if stringSlicesEqual(entry.SyncedAvailableModels, models) {
		m.mu.Unlock()
		return
	}
	entry.SyncedAvailableModels = cloneStrings(models)
	persistHook = m.persistHook
	m.mu.Unlock()

	if persistHook != nil {
		persistHook()
	}
}

// MarkUsageUnavailable records a failed upstream usage refresh without taking
// the account out of rotation.
func (m *Manager) MarkUsageUnavailable(id string, err string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	if state.LastUsageCheck.IsZero() {
		state.UsageStatus = "unavailable"
	}
	state.LastUsageCheck = time.Now()
	state.UsageError = strings.TrimSpace(err)
}

// MarkQuotaExhausted blocks an account from scheduling until a later recheck.
func (m *Manager) MarkQuotaExhausted(id string, err string, cooldown time.Duration) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.ensureRuntimeLocked(id, true)
	state.UsageStatus = "exhausted"
	state.QuotaExhausted = true
	state.QuotaLow = false
	state.LowestQuotaPercent = 0
	state.LastUsageCheck = time.Now()
	state.UsageError = strings.TrimSpace(err)
	if cooldown > 0 {
		state.UsageBlockedUntil = time.Now().Add(cooldown)
	}
}

func UsageFromUserStatus(status *coregrpc.UserStatus) UsageSnapshot {
	if status == nil {
		return UsageSnapshot{}
	}
	return UsageSnapshot{
		PlanName:                    strings.TrimSpace(status.PlanName),
		UsedPromptCredits:           status.UsedPromptCredits,
		UsedFlowCredits:             status.UsedFlowCredits,
		AvailablePromptCredits:      status.AvailablePromptCredits,
		AvailableFlowCredits:        status.AvailableFlowCredits,
		DailyQuotaRemainingPercent:  status.DailyQuotaRemainingPercent,
		WeeklyQuotaRemainingPercent: status.WeeklyQuotaRemainingPercent,
		DailyQuotaResetUnix:         status.DailyQuotaResetUnix,
		WeeklyQuotaResetUnix:        status.WeeklyQuotaResetUnix,
		HideDailyQuota:              status.HideDailyQuota,
		HideWeeklyQuota:             status.HideWeeklyQuota,
		QuotaExhausted:              status.QuotaExhausted,
		PromptCreditsPercent:        ratioPercent(status.AvailablePromptCredits, int(status.UsedPromptCredits)),
		FlowCreditsPercent:          ratioPercent(status.AvailableFlowCredits, int(status.UsedFlowCredits)),
		QuotaLow:                    isQuotaLow(status),
		LowestQuotaPercent:          lowestQuotaPercent(status),
	}
}

// AuthSource returns which credential path an account will use.
func AuthSource(entry config.AccountConfig) string {
	if entry.Email != "" && entry.Password != "" {
		return "password"
	}
	if entry.FirebaseRefreshToken != "" {
		return "refresh_token"
	}
	if entry.APIKey != "" {
		return "api_key"
	}
	return ""
}

func accountScore(state RuntimeState) int {
	score := 10000
	score -= state.ActiveRequests * 100
	score -= state.ConsecutiveFails * 500
	if state.QuotaLow {
		score -= 2500
		score -= max(0, QuotaLowThreshold-state.LowestQuotaPercent) * 50
	}
	if state.LastUsed.IsZero() {
		score += maxIdleScoreSeconds
	} else {
		score += min(int(time.Since(state.LastUsed).Seconds()), maxIdleScoreSeconds)
	}
	return score
}

func modelAffinity(entry config.AccountConfig, modelKey string) int {
	modelKey = core.CanonicalModelKey(modelKey)
	if modelKey == "" {
		return 0
	}
	if len(entry.AvailableModels) > 0 {
		return 2
	}
	for _, allowed := range entry.SyncedAvailableModels {
		if modelPolicyMatches(allowed, modelKey) {
			return 1
		}
	}
	return 0
}

func ratioPercent(available int, used int) int {
	total := available + used
	if total <= 0 {
		return -1
	}
	return (available * 100) / total
}

func lowestQuotaPercent(status *coregrpc.UserStatus) int {
	if status == nil {
		return -1
	}
	candidates := []int{}
	if status.DailyQuotaRemainingPercent >= 0 {
		candidates = append(candidates, status.DailyQuotaRemainingPercent)
	}
	if status.WeeklyQuotaRemainingPercent >= 0 {
		candidates = append(candidates, status.WeeklyQuotaRemainingPercent)
	}
	if pct := ratioPercent(status.AvailablePromptCredits, int(status.UsedPromptCredits)); pct >= 0 {
		candidates = append(candidates, pct)
	}
	if pct := ratioPercent(status.AvailableFlowCredits, int(status.UsedFlowCredits)); pct >= 0 {
		candidates = append(candidates, pct)
	}
	if len(candidates) == 0 {
		return -1
	}
	lowest := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate < lowest {
			lowest = candidate
		}
	}
	return lowest
}

func isQuotaLow(status *coregrpc.UserStatus) bool {
	if status == nil || status.QuotaExhausted {
		return false
	}
	lowest := lowestQuotaPercent(status)
	return lowest >= 0 && lowest <= QuotaLowThreshold
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func validateCredentials(entry config.AccountConfig) error {
	hasEmail := strings.TrimSpace(entry.Email) != ""
	hasPassword := entry.Password != ""
	if hasEmail != hasPassword {
		return fmt.Errorf("email and password must be provided together")
	}
	if !hasEmail && entry.FirebaseRefreshToken == "" && entry.APIKey == "" {
		return fmt.Errorf("account credentials are required")
	}
	return nil
}

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "active":
		return "active"
	case "disabled":
		return "disabled"
	default:
		return "active"
	}
}

func usageStatusOrDefault(status string) string {
	if strings.TrimSpace(status) == "" {
		return "unknown"
	}
	return status
}

func isModelAllowedForAccount(entry config.AccountConfig, state RuntimeState, modelKey string, now time.Time) bool {
	modelKey = core.CanonicalModelKey(modelKey)
	if modelKey == "" {
		return true
	}
	if isModelTemporarilyBlocked(state, modelKey, now) {
		return false
	}
	for _, blocked := range entry.BlockedModels {
		if modelPolicyMatches(blocked, modelKey) {
			return false
		}
	}
	if len(entry.AvailableModels) == 0 {
		return true
	}
	for _, allowed := range entry.AvailableModels {
		if modelPolicyMatches(allowed, modelKey) {
			return true
		}
	}
	return false
}

func isModelTemporarilyBlocked(state RuntimeState, modelKey string, now time.Time) bool {
	modelKey = core.CanonicalModelKey(modelKey)
	if modelKey == "" {
		return false
	}
	keys := []string{modelKey}
	if family := core.ModelFamilyKey(modelKey); family != "" && family != modelKey {
		keys = append(keys, family)
	}
	for _, key := range keys {
		if state.ModelRateLimitedUntil != nil {
			if until, ok := state.ModelRateLimitedUntil[key]; ok && now.Before(until) {
				return true
			}
		}
		if state.ModelUnavailableUntil != nil {
			if until, ok := state.ModelUnavailableUntil[key]; ok && now.Before(until) {
				return true
			}
		}
	}
	return false
}

func modelPolicyMatches(policy string, modelKey string) bool {
	policy = core.CanonicalModelKey(policy)
	modelKey = core.CanonicalModelKey(modelKey)
	if policy == "" || modelKey == "" {
		return false
	}
	if policy == "*" || policy == modelKey {
		return true
	}
	return core.ModelFamilyKey(policy) == core.ModelFamilyKey(modelKey)
}

func (m *Manager) getLocked(id string) *config.AccountConfig {
	for i := range m.cfg.Accounts {
		if m.cfg.Accounts[i].ID == id {
			return &m.cfg.Accounts[i]
		}
	}
	return nil
}

func (m *Manager) ensureRuntimeLocked(id string, healthy bool) *RuntimeState {
	if id == "" {
		return &RuntimeState{}
	}
	state, ok := m.runtime[id]
	if !ok || state == nil {
		state = &RuntimeState{
			Healthy:     healthy,
			UsageStatus: "unknown",
		}
		m.runtime[id] = state
	}
	if healthy && !state.Healthy && state.ConsecutiveFails == 0 {
		state.Healthy = true
	}
	return state
}

func providerForAccount(entry config.AccountConfig) string {
	switch AuthSource(entry) {
	case "password":
		return "email"
	case "refresh_token":
		return "oauth"
	case "api_key":
		return "api_key"
	default:
		return "manual"
	}
}

func resolveFreshCredentials(ctx context.Context, entry config.AccountConfig) (*auth.WindsurfServiceAuth, *auth.FirebaseTokens, string, error) {
	proxyURL := strings.TrimSpace(entry.Proxy)
	email := strings.TrimSpace(entry.Email)
	refreshToken := strings.TrimSpace(entry.FirebaseRefreshToken)

	if refreshToken != "" {
		firebaseTokens, err := refreshFirebaseTokenFunc(ctx, refreshToken, proxyURL)
		if err == nil {
			serviceAuth, regErr := registerWindsurfUserFunc(ctx, firebaseTokens.IDToken, proxyURL)
			if regErr == nil {
				return serviceAuth, firebaseTokens, "refresh_token", nil
			}
			err = regErr
		}

		if email == "" || entry.Password == "" {
			return nil, nil, "", err
		}

		serviceAuth, firebaseTokens, loginErr := fullLoginFunc(ctx, email, entry.Password, proxyURL)
		if loginErr != nil {
			return nil, nil, "", fmt.Errorf("refresh token auth failed: %v; password auth failed: %w", err, loginErr)
		}
		return serviceAuth, firebaseTokens, "password", nil
	}

	if email != "" && entry.Password != "" {
		serviceAuth, firebaseTokens, err := fullLoginFunc(ctx, email, entry.Password, proxyURL)
		if err != nil {
			return nil, nil, "", err
		}
		return serviceAuth, firebaseTokens, "password", nil
	}

	if strings.TrimSpace(entry.APIKey) != "" {
		return nil, nil, "api_key", nil
	}

	return nil, nil, "", nil
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringSlicesEqual(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizeModelList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if trimmed != "*" {
			trimmed = core.CanonicalModelKey(trimmed)
		}
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func maskKey(key string) string {
	if len(key) > 16 {
		return key[:8] + "..." + key[len(key)-4:]
	}
	if key != "" {
		return "****"
	}
	return ""
}

func maskProxy(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		if at := strings.LastIndex(raw, "@"); at > 0 {
			return "***@" + raw[at+1:]
		}
		return raw
	}
	if parsed.User != nil {
		if username := parsed.User.Username(); username != "" {
			parsed.User = url.UserPassword(username, "***")
		} else {
			parsed.User = url.User("***")
		}
	}
	return parsed.String()
}
