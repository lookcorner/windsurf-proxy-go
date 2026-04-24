package accounts

import (
	"context"
	"slices"
	"testing"
	"time"

	"windsurf-proxy-go/internal/auth"
	"windsurf-proxy-go/internal/config"
	coregrpc "windsurf-proxy-go/internal/core/grpc"
)

func TestAuthSourcePriority(t *testing.T) {
	entry := config.AccountConfig{
		Email:                "user@example.com",
		Password:             "secret",
		APIKey:               "sk-test",
		FirebaseRefreshToken: "refresh",
	}
	if got := AuthSource(entry); got != "password" {
		t.Fatalf("AuthSource() = %q, want password", got)
	}

	entry.Password = ""
	if got := AuthSource(entry); got != "refresh_token" {
		t.Fatalf("AuthSource() = %q, want refresh_token", got)
	}

	entry.FirebaseRefreshToken = ""
	if got := AuthSource(entry); got != "api_key" {
		t.Fatalf("AuthSource() = %q, want api_key", got)
	}
}

func TestListAttachedInstances(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_1", Name: "alpha", Status: "active", APIKey: "sk-1234567890abcdef"},
		},
	}
	manager := NewManager(cfg)

	views := manager.List([]config.InstanceConfig{
		{Name: "inst-a", AccountID: "acct_1"},
		{Name: "inst-b", AccountID: "acct_1"},
	})
	if len(views) != 1 {
		t.Fatalf("expected 1 account view, got %d", len(views))
	}
	if views[0].AttachedInstances != 2 {
		t.Fatalf("AttachedInstances = %d, want 2", views[0].AttachedInstances)
	}
	if views[0].KeyMasked == "" {
		t.Fatalf("expected masked key in view")
	}
}

func TestAddRejectsPartialPasswordCredentials(t *testing.T) {
	manager := NewManager(&config.Config{})

	_, err := manager.Add(config.AccountConfig{
		ID:    "acct_1",
		Name:  "alpha",
		Email: "user@example.com",
	})
	if err == nil {
		t.Fatalf("expected Add() to reject email without password")
	}
}

func TestSelectPrefersHealthyLeastBusyAccount(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_busy", Name: "busy", Status: "active", APIKey: "sk-busy"},
			{ID: "acct_idle", Name: "idle", Status: "active", APIKey: "sk-idle"},
			{ID: "acct_down", Name: "down", Status: "active", APIKey: "sk-down"},
		},
	}
	manager := NewManager(cfg)

	manager.runtime["acct_busy"] = &RuntimeState{
		Healthy:        true,
		ActiveRequests: 3,
		LastUsed:       time.Now(),
	}
	manager.runtime["acct_idle"] = &RuntimeState{
		Healthy:  true,
		LastUsed: time.Now().Add(-2 * time.Minute),
	}
	manager.runtime["acct_down"] = &RuntimeState{
		Healthy: false,
	}

	selected := manager.Select(nil, "")
	if selected == nil {
		t.Fatalf("Select() returned nil")
	}
	if selected.ID != "acct_idle" {
		t.Fatalf("Select() picked %q, want acct_idle", selected.ID)
	}
}

func TestSelectPrefersNeverUsedOverRecentlyUsedAccount(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_recent", Name: "recent", Status: "active", APIKey: "sk-recent"},
			{ID: "acct_never", Name: "never", Status: "active", APIKey: "sk-never"},
		},
	}
	manager := NewManager(cfg)
	manager.MarkAcquire("acct_recent")
	manager.MarkRelease("acct_recent")
	manager.MarkSuccess("acct_recent")

	selected := manager.Select(nil, "")
	if selected == nil {
		t.Fatalf("Select() returned nil")
	}
	if selected.ID != "acct_never" {
		t.Fatalf("Select() picked %q, want acct_never", selected.ID)
	}
}

func TestResolveForRequestUsesCachedAPIKey(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{
				ID:        "acct_cached",
				Name:      "cached",
				Status:    "active",
				APIKey:    "sk-cached",
				APIServer: "https://cached.example",
			},
		},
	}
	manager := NewManager(cfg)

	resolved, err := manager.ResolveForRequest(context.Background(), "acct_cached")
	if err != nil {
		t.Fatalf("ResolveForRequest() error = %v", err)
	}
	if resolved.APIKey != "sk-cached" {
		t.Fatalf("ResolveForRequest() api key = %q, want sk-cached", resolved.APIKey)
	}
	if resolved.APIServer != "https://cached.example" {
		t.Fatalf("ResolveForRequest() api server = %q, want https://cached.example", resolved.APIServer)
	}
}

func TestResolveForRequestStartsCachedTokenManager(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{
				ID:                   "acct_refreshable",
				Name:                 "refreshable",
				Status:               "active",
				APIKey:               "sk-cached",
				FirebaseRefreshToken: "refresh-token",
			},
		},
	}
	manager := NewManager(cfg)
	defer manager.Stop()

	if _, err := manager.ResolveForRequest(context.Background(), "acct_refreshable"); err != nil {
		t.Fatalf("ResolveForRequest() error = %v", err)
	}
	if manager.tokenManagers["acct_refreshable"] == nil {
		t.Fatalf("token manager was not started for cached refreshable account")
	}
}

func TestResolveStandalonePrefersRefreshTokenOverPassword(t *testing.T) {
	prevRefresh := refreshFirebaseTokenFunc
	prevRegister := registerWindsurfUserFunc
	prevFullLogin := fullLoginFunc
	defer func() {
		refreshFirebaseTokenFunc = prevRefresh
		registerWindsurfUserFunc = prevRegister
		fullLoginFunc = prevFullLogin
	}()

	refreshCalls := 0
	registerCalls := 0
	fullLoginCalls := 0

	refreshFirebaseTokenFunc = func(_ context.Context, refreshToken string, proxyURL string) (*auth.FirebaseTokens, error) {
		refreshCalls++
		if refreshToken != "refresh-token" {
			t.Fatalf("refresh token = %q, want refresh-token", refreshToken)
		}
		if proxyURL != "http://127.0.0.1:7890" {
			t.Fatalf("proxyURL = %q, want explicit account proxy", proxyURL)
		}
		return &auth.FirebaseTokens{
			IDToken:      "id-from-refresh",
			RefreshToken: refreshToken,
			ExpiresIn:    3600,
			Email:        "user@example.com",
		}, nil
	}
	registerWindsurfUserFunc = func(_ context.Context, idToken string, proxyURL string) (*auth.WindsurfServiceAuth, error) {
		registerCalls++
		if idToken != "id-from-refresh" {
			t.Fatalf("idToken = %q, want id-from-refresh", idToken)
		}
		if proxyURL != "http://127.0.0.1:7890" {
			t.Fatalf("proxyURL = %q, want explicit account proxy", proxyURL)
		}
		return &auth.WindsurfServiceAuth{
			APIKey:       "sk-from-refresh",
			APIServerURL: "https://server.example",
		}, nil
	}
	fullLoginFunc = func(context.Context, string, string, string) (*auth.WindsurfServiceAuth, *auth.FirebaseTokens, error) {
		fullLoginCalls++
		t.Fatalf("full login should not be called when refresh token is available")
		return nil, nil, nil
	}

	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{
				ID:                   "acct_refresh_first",
				Name:                 "refresh-first",
				Status:               "active",
				Email:                "user@example.com",
				Password:             "secret",
				FirebaseRefreshToken: "refresh-token",
				Proxy:                "http://127.0.0.1:7890",
			},
		},
	}
	manager := NewManager(cfg)
	defer manager.Stop()

	resolved, err := manager.ResolveStandalone(context.Background(), "acct_refresh_first")
	if err != nil {
		t.Fatalf("ResolveStandalone() error = %v", err)
	}
	if resolved.AuthSource != "refresh_token" {
		t.Fatalf("ResolveStandalone() auth source = %q, want refresh_token", resolved.AuthSource)
	}
	if resolved.APIKey != "sk-from-refresh" {
		t.Fatalf("ResolveStandalone() api key = %q, want sk-from-refresh", resolved.APIKey)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls)
	}
	if registerCalls != 1 {
		t.Fatalf("register calls = %d, want 1", registerCalls)
	}
	if fullLoginCalls != 0 {
		t.Fatalf("full login calls = %d, want 0", fullLoginCalls)
	}
}

func TestResolveStandaloneFallsBackToPasswordWhenRefreshTokenFails(t *testing.T) {
	prevRefresh := refreshFirebaseTokenFunc
	prevRegister := registerWindsurfUserFunc
	prevFullLogin := fullLoginFunc
	defer func() {
		refreshFirebaseTokenFunc = prevRefresh
		registerWindsurfUserFunc = prevRegister
		fullLoginFunc = prevFullLogin
	}()

	refreshFirebaseTokenFunc = func(context.Context, string, string) (*auth.FirebaseTokens, error) {
		return nil, context.DeadlineExceeded
	}
	registerWindsurfUserFunc = func(context.Context, string, string) (*auth.WindsurfServiceAuth, error) {
		t.Fatalf("register should not be called when refresh token exchange failed")
		return nil, nil
	}
	fullLoginCalls := 0
	fullLoginFunc = func(_ context.Context, email string, password string, proxyURL string) (*auth.WindsurfServiceAuth, *auth.FirebaseTokens, error) {
		fullLoginCalls++
		if email != "user@example.com" {
			t.Fatalf("email = %q, want user@example.com", email)
		}
		if password != "secret" {
			t.Fatalf("password = %q, want secret", password)
		}
		if proxyURL != "http://127.0.0.1:7890" {
			t.Fatalf("proxyURL = %q, want explicit account proxy", proxyURL)
		}
		return &auth.WindsurfServiceAuth{
				APIKey:       "sk-from-password",
				APIServerURL: "https://server.example",
			}, &auth.FirebaseTokens{
				IDToken:      "id-from-password",
				RefreshToken: "new-refresh-token",
				ExpiresIn:    3600,
				Email:        email,
			}, nil
	}

	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{
				ID:                   "acct_password_fallback",
				Name:                 "password-fallback",
				Status:               "active",
				Email:                "user@example.com",
				Password:             "secret",
				FirebaseRefreshToken: "refresh-token",
				Proxy:                "http://127.0.0.1:7890",
			},
		},
	}
	manager := NewManager(cfg)
	defer manager.Stop()

	resolved, err := manager.ResolveStandalone(context.Background(), "acct_password_fallback")
	if err != nil {
		t.Fatalf("ResolveStandalone() error = %v", err)
	}
	if resolved.AuthSource != "password" {
		t.Fatalf("ResolveStandalone() auth source = %q, want password", resolved.AuthSource)
	}
	if resolved.APIKey != "sk-from-password" {
		t.Fatalf("ResolveStandalone() api key = %q, want sk-from-password", resolved.APIKey)
	}
	if fullLoginCalls != 1 {
		t.Fatalf("full login calls = %d, want 1", fullLoginCalls)
	}
}

func TestRemoveAllowsStandaloneBootstrapReference(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_bootstrap", Name: "bootstrap", Status: "active", APIKey: "sk-bootstrap"},
		},
	}
	manager := NewManager(cfg)

	err := manager.Remove("acct_bootstrap", []config.InstanceConfig{
		{Name: "shared-standalone", Type: "standalone", AccountID: "acct_bootstrap"},
	})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
}

func TestSelectSkipsBlockedAccount(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_blocked", Name: "blocked", Status: "active", APIKey: "sk-blocked"},
			{ID: "acct_ready", Name: "ready", Status: "active", APIKey: "sk-ready"},
		},
	}
	manager := NewManager(cfg)
	manager.Block("acct_blocked", "timeout", time.Minute)

	selected := manager.Select(nil, "")
	if selected == nil {
		t.Fatalf("Select() returned nil")
	}
	if selected.ID != "acct_ready" {
		t.Fatalf("Select() picked %q, want acct_ready", selected.ID)
	}
}

func TestSelectSkipsQuotaExhaustedAccount(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_exhausted", Name: "exhausted", Status: "active", APIKey: "sk-exhausted"},
			{ID: "acct_ready", Name: "ready", Status: "active", APIKey: "sk-ready"},
		},
	}
	manager := NewManager(cfg)
	manager.UpdateUsage("acct_exhausted", UsageSnapshot{
		PlanName:               "Starter",
		UsedPromptCredits:      300,
		AvailablePromptCredits: 0,
		QuotaExhausted:         true,
	}, time.Minute)

	selected := manager.Select(nil, "")
	if selected == nil {
		t.Fatalf("Select() returned nil")
	}
	if selected.ID != "acct_ready" {
		t.Fatalf("Select() picked %q, want acct_ready", selected.ID)
	}
}

func TestSelectDeprioritizesLowQuotaAccount(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_low", Name: "low", Status: "active", APIKey: "sk-low"},
			{ID: "acct_ready", Name: "ready", Status: "active", APIKey: "sk-ready"},
		},
	}
	manager := NewManager(cfg)

	manager.UpdateUsage("acct_low", UsageSnapshot{
		QuotaLow:           true,
		LowestQuotaPercent: 8,
	}, 0)

	selected := manager.Select(nil, "")
	if selected == nil {
		t.Fatalf("Select() returned nil")
	}
	if selected.ID != "acct_ready" {
		t.Fatalf("Select() picked %q, want acct_ready", selected.ID)
	}
}

func TestSelectFiltersByAvailableModels(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_sonnet", Name: "sonnet", Status: "active", APIKey: "sk-sonnet", AvailableModels: []string{"claude-4.6-sonnet"}},
			{ID: "acct_opus", Name: "opus", Status: "active", APIKey: "sk-opus", AvailableModels: []string{"claude-4.6-opus"}},
		},
	}
	manager := NewManager(cfg)

	selected := manager.Select(nil, "claude-opus-4-6")
	if selected == nil {
		t.Fatalf("Select() returned nil")
	}
	if selected.ID != "acct_opus" {
		t.Fatalf("Select() picked %q, want acct_opus", selected.ID)
	}
}

func TestSelectMatchesModelFamilyPolicy(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_family", Name: "family", Status: "active", APIKey: "sk-family", AvailableModels: []string{"claude-4.7-opus"}},
		},
	}
	manager := NewManager(cfg)

	selected := manager.Select(nil, "claude-opus-4-7-low")
	if selected == nil {
		t.Fatalf("Select() returned nil for family-matched model")
	}
	if selected.ID != "acct_family" {
		t.Fatalf("Select() picked %q, want acct_family", selected.ID)
	}
}

func TestSelectPrefersSyncedModelMatchOverUnknownAccount(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_unknown", Name: "unknown", Status: "active", APIKey: "sk-unknown"},
			{ID: "acct_synced", Name: "synced", Status: "active", APIKey: "sk-synced", SyncedAvailableModels: []string{"gpt-4o-mini"}},
		},
	}
	manager := NewManager(cfg)

	selected := manager.Select(nil, "gpt-4o-mini")
	if selected == nil {
		t.Fatalf("Select() returned nil")
	}
	if selected.ID != "acct_synced" {
		t.Fatalf("Select() picked %q, want acct_synced", selected.ID)
	}
}

func TestMarkModelRateLimitedOnlyBlocksOneModel(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_multi", Name: "multi", Status: "active", APIKey: "sk-multi"},
		},
	}
	manager := NewManager(cfg)
	manager.MarkModelRateLimited("acct_multi", "claude-opus-4-7-low", "too many requests", time.Minute)

	if manager.IsSchedulableForModel("acct_multi", "claude-opus-4-7-low") {
		t.Fatalf("IsSchedulableForModel(opus-low) = true, want false")
	}
	if !manager.IsSchedulableForModel("acct_multi", "claude-4.6-sonnet") {
		t.Fatalf("IsSchedulableForModel(sonnet) = false, want true")
	}
}

func TestSyncAllowedModelsStoresNormalizedSyncedModels(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_sync", Name: "sync", Status: "active", APIKey: "sk-sync"},
		},
	}
	manager := NewManager(cfg)

	manager.SyncAllowedModels("acct_sync", []string{" GPT-4O-MINI ", "gpt-4o-mini", "Gemini-2.5-Flash"})

	account := manager.Get("acct_sync")
	if account == nil {
		t.Fatalf("Get() returned nil")
	}
	if len(account.SyncedAvailableModels) != 2 {
		t.Fatalf("SyncedAvailableModels len = %d, want 2 (%#v)", len(account.SyncedAvailableModels), account.SyncedAvailableModels)
	}
	if !slices.Contains(account.SyncedAvailableModels, "gpt-4o-mini") {
		t.Fatalf("SyncedAvailableModels missing gpt-4o-mini: %#v", account.SyncedAvailableModels)
	}
	if !slices.Contains(account.SyncedAvailableModels, "gemini-2.5-flash") {
		t.Fatalf("SyncedAvailableModels missing gemini-2.5-flash: %#v", account.SyncedAvailableModels)
	}
	if manager.Runtime("acct_sync").LastModelSync.IsZero() {
		t.Fatalf("LastModelSync was not updated")
	}
}

func TestUsageFromUserStatusMarksQuotaLow(t *testing.T) {
	snapshot := UsageFromUserStatus(&coregrpc.UserStatus{
		AvailablePromptCredits:      10,
		UsedPromptCredits:           90,
		DailyQuotaRemainingPercent:  15,
		WeeklyQuotaRemainingPercent: 50,
	})

	if !snapshot.QuotaLow {
		t.Fatalf("QuotaLow = false, want true")
	}
	if snapshot.LowestQuotaPercent != 10 {
		t.Fatalf("LowestQuotaPercent = %d, want 10", snapshot.LowestQuotaPercent)
	}
}
