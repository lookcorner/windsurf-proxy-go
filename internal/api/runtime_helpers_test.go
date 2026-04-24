package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"windsurf-proxy-go/internal/accounts"
	"windsurf-proxy-go/internal/balancer"
	"windsurf-proxy-go/internal/config"
)

func TestShouldRefreshLocalCredentials(t *testing.T) {
	local := &balancer.Instance{Type: "local"}
	manual := &balancer.Instance{Type: "manual"}

	tests := []struct {
		name string
		inst *balancer.Instance
		err  error
		want bool
	}{
		{
			name: "local csrf unauthenticated",
			inst: local,
			err:  errors.New("rpc error: code = Unauthenticated desc = invalid CSRF token"),
			want: true,
		},
		{
			name: "local missing csrf unauthenticated",
			inst: local,
			err:  errors.New("rpc error: code = Unauthenticated desc = missing CSRF token"),
			want: true,
		},
		{
			name: "local other unauthenticated",
			inst: local,
			err:  errors.New("rpc error: code = Unauthenticated desc = invalid api key"),
			want: false,
		},
		{
			name: "manual csrf unauthenticated",
			inst: manual,
			err:  errors.New("rpc error: code = Unauthenticated desc = invalid CSRF token"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &requestTarget{Instance: tt.inst}
			if got := shouldRefreshLocalCredentials(target, tt.err); got != tt.want {
				t.Fatalf("shouldRefreshLocalCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsQuotaExhaustedError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "quota message",
			err:  errors.New("rpc error: code = Unknown desc = weekly quota exhausted"),
			want: true,
		},
		{
			name: "credit message",
			err:  errors.New("rpc error: code = Unknown desc = no premium credits remaining"),
			want: true,
		},
		{
			name: "csrf message",
			err:  errors.New("rpc error: code = Unauthenticated desc = invalid CSRF token"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isQuotaExhaustedError(tt.err); got != tt.want {
				t.Fatalf("isQuotaExhaustedError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRateLimitedError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "trial global rate limit",
			err:  errors.New("All instances failed: all API providers are over their global rate limit for trial users"),
			want: true,
		},
		{
			name: "too many requests",
			err:  errors.New("rpc error: code = ResourceExhausted desc = too many requests"),
			want: true,
		},
		{
			name: "csrf message",
			err:  errors.New("rpc error: code = Unauthenticated desc = invalid CSRF token"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRateLimitedError(tt.err); got != tt.want {
				t.Fatalf("isRateLimitedError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRateLimitCooldown(t *testing.T) {
	if got := rateLimitCooldown(errors.New("please try again in an hour")); got != time.Hour {
		t.Fatalf("rateLimitCooldown(hour) = %v, want %v", got, time.Hour)
	}
	if got := rateLimitCooldown(errors.New("trial users global rate limit")); got != 5*time.Minute {
		t.Fatalf("rateLimitCooldown(default) = %v, want %v", got, 5*time.Minute)
	}
}

func TestFinalErrorStatusClassifiesUpstreamInternalAsRateLimit(t *testing.T) {
	status := finalErrorStatus("an internal error occurred (error ID: abc)")
	if status != http.StatusTooManyRequests {
		t.Fatalf("finalErrorStatus(internal) = %d, want %d", status, http.StatusTooManyRequests)
	}
}

func TestAnthropicInternalErrorTypeIsRateLimit(t *testing.T) {
	status := finalErrorStatus("model claude-opus-4-7-medium temporarily unavailable for all accounts")
	if got := anthropicErrorType(status); got != "rate_limit_error" {
		t.Fatalf("anthropicErrorType(%d) = %q, want rate_limit_error", status, got)
	}
}

func TestRememberFailedAccountSkipsAccountOnNextSelection(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_a", Name: "Account A", Status: "active", APIKey: "sk-a"},
			{ID: "acct_b", Name: "Account B", Status: "active", APIKey: "sk-b"},
		},
		Balancing: config.BalancingConfig{Strategy: "round_robin"},
	}
	accountMgr := accounts.NewManager(cfg)
	lb := balancer.New(&cfg.Balancing, accountMgr)
	lb.AddInstance(&balancer.Instance{Name: "standalone-a", Type: "standalone", AccountID: "acct_a", Healthy: true, Weight: 10})
	lb.AddInstance(&balancer.Instance{Name: "standalone-b", Type: "standalone", AccountID: "acct_b", Healthy: true, Weight: 10})

	h := &Handler{balancer: lb, accounts: accountMgr}
	triedAccounts := make(map[string]bool)

	first, err := h.selectRequestTarget(context.Background(), nil, nil, triedAccounts, "")
	if err != nil {
		t.Fatalf("first selectRequestTarget() error = %v", err)
	}
	if first.AccountID != "acct_a" {
		t.Fatalf("first account = %q, want acct_a", first.AccountID)
	}
	rememberFailedAccount(triedAccounts, first)
	h.releaseTarget(first)

	second, err := h.selectRequestTarget(context.Background(), nil, nil, triedAccounts, "")
	if err != nil {
		t.Fatalf("second selectRequestTarget() error = %v", err)
	}
	defer h.releaseTarget(second)
	if second.AccountID != "acct_b" {
		t.Fatalf("second account = %q, want acct_b", second.AccountID)
	}
}

func TestMarkTargetErrorBlocksRateLimitedAccount(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_trial", Name: "Trial", Status: "active", APIKey: "sk-trial"},
			{ID: "acct_ready", Name: "Ready", Status: "active", APIKey: "sk-ready"},
		},
	}
	accountMgr := accounts.NewManager(cfg)
	h := &Handler{
		balancer: &balancer.LoadBalancer{},
		accounts: accountMgr,
	}

	target := &requestTarget{
		Instance:    &balancer.Instance{Name: "trial-worker", Type: "standalone", Healthy: true},
		AccountID:   "acct_trial",
		AccountName: "Trial",
	}
	h.markTargetError(target, errors.New("all API providers are over their global rate limit for trial users"), "")

	state := accountMgr.Runtime("acct_trial")
	if !state.RateLimitedUntil.After(time.Now()) {
		t.Fatalf("RateLimitedUntil = %v, want future time", state.RateLimitedUntil)
	}
	if state.QuotaExhausted {
		t.Fatalf("QuotaExhausted = true, want false for rate limit")
	}

	selected := accountMgr.Select(nil, "")
	if selected == nil {
		t.Fatalf("Select() returned nil, want acct_ready")
	}
	if selected.ID != "acct_ready" {
		t.Fatalf("Select() picked %q, want acct_ready", selected.ID)
	}
}

func TestMarkTargetErrorModelRateLimitIsScopedToModel(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_trial", Name: "Trial", Status: "active", APIKey: "sk-trial"},
		},
	}
	accountMgr := accounts.NewManager(cfg)
	h := &Handler{
		balancer: &balancer.LoadBalancer{},
		accounts: accountMgr,
	}

	target := &requestTarget{
		Instance:    &balancer.Instance{Name: "trial-worker", Type: "standalone", Healthy: true},
		AccountID:   "acct_trial",
		AccountName: "Trial",
	}
	h.markTargetError(target, errors.New("rpc error: code = ResourceExhausted desc = too many requests"), "claude-opus-4-7-low")

	if accountMgr.IsSchedulableForModel("acct_trial", "claude-opus-4-7-low") {
		t.Fatalf("IsSchedulableForModel(opus-low) = true, want false")
	}
	if !accountMgr.IsSchedulableForModel("acct_trial", "claude-4.6-sonnet") {
		t.Fatalf("IsSchedulableForModel(sonnet) = false, want true")
	}
}

func TestMarkTargetErrorModelUnavailableBlocksModelFamily(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_trial", Name: "Trial", Status: "active", APIKey: "sk-trial"},
		},
	}
	accountMgr := accounts.NewManager(cfg)
	h := &Handler{
		balancer: &balancer.LoadBalancer{},
		accounts: accountMgr,
	}

	target := &requestTarget{
		Instance:    &balancer.Instance{Name: "trial-worker", Type: "standalone", Healthy: true},
		AccountID:   "acct_trial",
		AccountName: "Trial",
	}
	h.markTargetError(target, errors.New("rpc error: code = InvalidArgument desc = model not available for this account"), "claude-opus-4-7-low")

	if accountMgr.IsSchedulableForModel("acct_trial", "claude-opus-4-7-medium") {
		t.Fatalf("IsSchedulableForModel(opus-medium) = true, want false after family block")
	}
	if !accountMgr.IsSchedulableForModel("acct_trial", "claude-4.6-sonnet") {
		t.Fatalf("IsSchedulableForModel(sonnet) = false, want true")
	}
}

func TestMarkTargetErrorUpstreamInternalCoolsOnlyModel(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_trial", Name: "Trial", Status: "active", APIKey: "sk-trial"},
		},
	}
	accountMgr := accounts.NewManager(cfg)
	bal := balancer.New(&config.BalancingConfig{}, accountMgr)
	h := &Handler{
		balancer: bal,
		accounts: accountMgr,
	}

	inst := &balancer.Instance{Name: "trial-worker", Type: "standalone", Healthy: true}
	target := &requestTarget{
		Instance:    inst,
		AccountID:   "acct_trial",
		AccountName: "Trial",
	}
	h.markTargetError(target, errors.New("an internal error occurred (error ID: abc)"), "claude-opus-4-7-medium")

	if accountMgr.IsSchedulableForModel("acct_trial", "claude-opus-4-7-medium") {
		t.Fatalf("IsSchedulableForModel(opus-medium) = true, want false")
	}
	if !accountMgr.IsSchedulableForModel("acct_trial", "claude-4.6-sonnet") {
		t.Fatalf("IsSchedulableForModel(sonnet) = false, want true")
	}
	if inst.Healthy {
		t.Fatalf("worker Healthy = true, want false after upstream internal error")
	}
}
