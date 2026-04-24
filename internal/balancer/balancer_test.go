package balancer

import (
	"testing"

	"windsurf-proxy-go/internal/accounts"
	"windsurf-proxy-go/internal/auth"
	"windsurf-proxy-go/internal/config"
)

func TestRefreshLocalInstanceUpdatesCredentials(t *testing.T) {
	originalDiscover := discoverLocalCredentials
	discoverLocalCredentials = func() (*auth.WindsurfCredentials, error) {
		return &auth.WindsurfCredentials{
			CSRFToken: "new-csrf",
			GRPCPort:  43111,
			APIKey:    "new-api-key",
			Version:   "2.0.0",
		}, nil
	}
	defer func() {
		discoverLocalCredentials = originalDiscover
	}()

	lb := &LoadBalancer{}
	inst := &Instance{
		Name:      "local",
		Type:      "local",
		Host:      "127.0.0.1",
		Port:      42100,
		CSRFToken: "old-csrf",
		APIKey:    "old-api-key",
		Version:   "1.0.0",
		Healthy:   false,
		LastError: "invalid CSRF token",
	}

	if err := lb.RefreshLocalInstance(inst); err != nil {
		t.Fatalf("RefreshLocalInstance() error = %v", err)
	}
	if inst.Port != 43111 {
		t.Fatalf("Port = %d, want 43111", inst.Port)
	}
	if inst.CSRFToken != "new-csrf" {
		t.Fatalf("CSRFToken = %q, want new-csrf", inst.CSRFToken)
	}
	if inst.APIKey != "new-api-key" {
		t.Fatalf("APIKey = %q, want new-api-key", inst.APIKey)
	}
	if inst.Version != "2.0.0" {
		t.Fatalf("Version = %q, want 2.0.0", inst.Version)
	}
	if !inst.Healthy {
		t.Fatalf("Healthy = false, want true")
	}
	if inst.LastError != "" {
		t.Fatalf("LastError = %q, want empty", inst.LastError)
	}
	if inst.Client == nil {
		t.Fatalf("Client = nil, want initialized client")
	}
}

func TestGetWorkerPrefersSharedStandaloneForAccountPool(t *testing.T) {
	lb := &LoadBalancer{
		config: &config.BalancingConfig{Strategy: "round_robin"},
		instances: []*Instance{
			{Name: "local", Type: "local", Healthy: true, Weight: 10},
			{Name: "standalone-other", Type: "standalone", AccountID: "acct_other", Healthy: true, Weight: 10},
		},
	}

	inst, err := lb.GetWorker(nil, "acct_pool", "", "")
	if err != nil {
		t.Fatalf("GetWorker() error = %v", err)
	}
	if inst.Name != "standalone-other" {
		t.Fatalf("GetWorker() picked %q, want standalone-other", inst.Name)
	}
}

func TestGetWorkerAllowsOneStandaloneForMultipleAccounts(t *testing.T) {
	lb := &LoadBalancer{
		config: &config.BalancingConfig{Strategy: "round_robin"},
		instances: []*Instance{
			{Name: "standalone-shared", Type: "standalone", BootstrapAccountID: "acct_boot", Healthy: true, Weight: 10},
		},
	}

	first, err := lb.GetWorker(nil, "acct_a", "", "")
	if err != nil {
		t.Fatalf("GetWorker(acct_a) error = %v", err)
	}
	lb.ReleaseInstance(first)
	second, err := lb.GetWorker(nil, "acct_b", "", "")
	if err != nil {
		t.Fatalf("GetWorker(acct_b) error = %v", err)
	}
	if first.Name != "standalone-shared" || second.Name != "standalone-shared" {
		t.Fatalf("shared standalone picks = %q/%q, want standalone-shared for both", first.Name, second.Name)
	}
}

func TestGetWorkerWithoutAccountSkipsAccountBackedStandalone(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_match", Name: "match", Status: "active", APIKey: "sk-match"},
		},
		Balancing: config.BalancingConfig{Strategy: "round_robin"},
	}
	accountMgr := accounts.NewManager(cfg)
	lb := New(&cfg.Balancing, accountMgr)
	lb.AddInstance(&Instance{Name: "local", Type: "local", Healthy: true, Weight: 10})
	lb.AddInstance(&Instance{Name: "standalone-match", Type: "standalone", AccountID: "acct_match", Healthy: true, Weight: 10})

	inst, err := lb.GetWorker(nil, "", "", "")
	if err != nil {
		t.Fatalf("GetWorker() error = %v", err)
	}
	if inst.Name != "local" {
		t.Fatalf("GetWorker() picked %q, want local", inst.Name)
	}
}

func TestGetWorkerWithoutAccountReturnsNoHealthyForStandaloneOnly(t *testing.T) {
	cfg := &config.Config{
		Accounts: []config.AccountConfig{
			{ID: "acct_match", Name: "match", Status: "active", APIKey: "sk-match"},
		},
		Balancing: config.BalancingConfig{Strategy: "round_robin"},
	}
	accountMgr := accounts.NewManager(cfg)
	lb := New(&cfg.Balancing, accountMgr)
	lb.AddInstance(&Instance{Name: "standalone-match", Type: "standalone", AccountID: "acct_match", Healthy: true, Weight: 10})

	if _, err := lb.GetWorker(nil, "", "", ""); err != ErrNoHealthyInstances {
		t.Fatalf("GetWorker() error = %v, want %v", err, ErrNoHealthyInstances)
	}
}

func TestGetWorkerRejectsStandaloneAPIServerMismatch(t *testing.T) {
	lb := &LoadBalancer{
		config: &config.BalancingConfig{Strategy: "round_robin"},
		instances: []*Instance{
			{Name: "standalone-default", Type: "standalone", APIServer: "https://server.self-serve.windsurf.com", Healthy: true, Weight: 10},
		},
	}

	if _, err := lb.GetWorker(nil, "acct_custom", "https://custom.example", ""); err != ErrNoHealthyInstances {
		t.Fatalf("GetWorker() error = %v, want %v", err, ErrNoHealthyInstances)
	}
	if probe := lb.ProbeWorker("acct_custom", "https://custom.example", ""); probe != nil {
		t.Fatalf("ProbeWorker() = %q, want nil for api_server mismatch", probe.Name)
	}
}

func TestGetWorkerRequiresMatchingProxyRoute(t *testing.T) {
	lb := &LoadBalancer{
		config: &config.BalancingConfig{Strategy: "round_robin"},
		instances: []*Instance{
			{
				Name:      "standalone-proxy-a",
				Type:      "standalone",
				APIServer: "https://server.self-serve.windsurf.com",
				ProxyURL:  "http://proxy-a.example:8080",
				ProxyKey:  normalizeProxyKey("http://proxy-a.example:8080"),
				Healthy:   true,
				Weight:    10,
			},
			{
				Name:      "standalone-proxy-b",
				Type:      "standalone",
				APIServer: "https://server.self-serve.windsurf.com",
				ProxyURL:  "http://proxy-b.example:8080",
				ProxyKey:  normalizeProxyKey("http://proxy-b.example:8080"),
				Healthy:   true,
				Weight:    10,
			},
		},
	}

	inst, err := lb.GetWorker(nil, "acct_proxy", "https://server.self-serve.windsurf.com", "http://proxy-b.example:8080")
	if err != nil {
		t.Fatalf("GetWorker() error = %v", err)
	}
	if inst.Name != "standalone-proxy-b" {
		t.Fatalf("GetWorker() picked %q, want standalone-proxy-b", inst.Name)
	}

	if _, err := lb.GetWorker(nil, "acct_proxy", "https://server.self-serve.windsurf.com", "http://proxy-c.example:8080"); err != ErrNoHealthyInstances {
		t.Fatalf("GetWorker() error = %v, want %v for unmatched proxy", err, ErrNoHealthyInstances)
	}
}
