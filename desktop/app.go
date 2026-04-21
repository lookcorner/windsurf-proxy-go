package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"windsurf-proxy-go/internal/api"
	"windsurf-proxy-go/internal/audit"
	"windsurf-proxy-go/internal/balancer"
	"windsurf-proxy-go/internal/config"
	"windsurf-proxy-go/internal/keys"
	"windsurf-proxy-go/internal/management"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App holds the desktop application state.
type App struct {
	ctx        context.Context
	server     *http.Server
	bal        *balancer.LoadBalancer
	keyMgr     *keys.Manager
	cfg        *config.Config
	apiHandler *api.Handler
	mgmt       *management.Handler
	running    bool
	startTime  time.Time
	mu         sync.Mutex
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// Startup is called when the app starts (Wails lifecycle).
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	wruntime.WindowSetTitle(ctx, "Windsurf Proxy")

	// Enable file logging as the very first thing so config migration,
	// load failures, and the rest of startup are persisted to disk.
	logDir := filepath.Join(config.GetDataDir(), "logs")
	if path, err := management.EnableLogFile(logDir); err != nil {
		log.Printf("[Desktop] Log file disabled: %v", err)
	} else {
		log.Printf("[Desktop] Log file: %s", path)
	}

	log.Printf("[Desktop] Application starting...")

	// Migrate config from legacy path if needed
	if migrated, err := config.MigrateConfig(); err != nil {
		log.Printf("[Desktop] Config migration failed: %v", err)
	} else if migrated {
		log.Printf("[Desktop] Config migrated to %s", config.GetConfigDir())
	}

	// Load config
	cfg, err := config.Load("")
	if err != nil {
		log.Printf("[Desktop] Config load failed: %v", err)
		// Create default config
		cfg = &config.Config{
			Server: config.ServerConfig{
				Host: "127.0.0.1",
				Port: 8000,
			},
			Balancing: config.BalancingConfig{
				Strategy:            "round_robin",
				HealthCheckInterval: 30,
				MaxRetries:          3,
				RetryDelay:          1.0,
			},
			Instances: []config.InstanceConfig{},
			APIKeys: []config.APIKeyEntry{
				{
					Key:           "sk-windsurf-desktop",
					Name:          "desktop-default",
					RateLimit:     60,
					AllowedModels: []string{"*"},
				},
			},
		}
	}
	a.cfg = cfg

	// Per-request audit log is opt-in (logging.audit in config). It
	// stores full request/response bodies, so we leave it off by
	// default to respect privacy + disk space.
	a.applyAuditConfig(logDir)

	// Create key manager
	a.keyMgr = keys.NewManager(cfg.APIKeys)
	log.Printf("[Desktop] API key auth: %d keys loaded", len(cfg.APIKeys))

	// Create load balancer
	a.bal = balancer.New(&cfg.Balancing)
	a.bal.InitFromConfigs(cfg.Instances)
	a.bal.StartHealthChecks()
	log.Printf("[Desktop] Load balancer: %d instances", len(cfg.Instances))

	a.mgmt = management.NewHandler(a.bal, a.keyMgr, a.cfg, "")
	a.mgmt.SetServerConfigChangedHandler(a.restartServerWithConfig)
	a.mgmt.SetLoggingChangedHandler(func(_ config.LoggingConfig) {
		a.applyAuditConfig(filepath.Join(config.GetDataDir(), "logs"))
	})
	a.apiHandler = api.NewHandler(a.bal, a.keyMgr, a.cfg, a.mgmt.RecordRequest)

	// Attach the management handler so log lines start being broadcast
	// to WebSocket clients (file logging is already running).
	management.InitLogHook(a.mgmt)

	// Start HTTP server
	a.startServer()
	a.running = true
	a.startTime = time.Now()

	log.Printf("[Desktop] Application started successfully")
}

// Shutdown is called when the app closes (Wails lifecycle).
func (a *App) Shutdown(ctx context.Context) {
	log.Printf("[Desktop] Application shutting down...")

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		a.server.Shutdown(shutdownCtx)
		log.Printf("[Desktop] HTTP server stopped")
	}

	if a.bal != nil {
		a.bal.Stop()
		log.Printf("[Desktop] Load balancer stopped")
	}

	a.running = false
	log.Printf("[Desktop] Application shutdown complete")
}

// applyAuditConfig enables or disables the per-request audit log to
// match a.cfg.Logging.Audit. Safe to call repeatedly — Enable rotates
// to today's file and Disable closes the active handle.
func (a *App) applyAuditConfig(logDir string) {
	if a.cfg == nil {
		return
	}
	if a.cfg.Logging.Audit {
		path, err := audit.Enable(logDir)
		if err != nil {
			log.Printf("[Desktop] Audit log enable failed: %v", err)
			return
		}
		log.Printf("[Desktop] Audit log: %s (logging.audit=true)", path)
	} else {
		audit.Disable()
		log.Printf("[Desktop] Audit log disabled (logging.audit=false)")
	}
}

// startServer starts the embedded HTTP server.
func (a *App) startServer() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.startServerLocked()
}

func (a *App) startServerLocked() {
	mux := http.NewServeMux()

	// Register handlers
	a.apiHandler.RegisterRoutes(mux)
	a.mgmt.RegisterRoutes(mux)

	addr := fmt.Sprintf("%s:%d", a.cfg.Server.Host, a.cfg.Server.Port)
	a.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	go func() {
		log.Printf("[Desktop] HTTP server listening on %s", addr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Desktop] HTTP server error: %v", err)
		}
	}()
}

func (a *App) restartServerWithConfig(prev, next config.ServerConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			log.Printf("[Desktop] HTTP server restart shutdown error: %v", err)
		}
		cancel()
	}

	log.Printf("[Desktop] Restarting HTTP server: %s:%d -> %s:%d", prev.Host, prev.Port, next.Host, next.Port)
	a.startServerLocked()
}

// ========== Wails bindings (called from frontend) ==========

// GetStatus returns the current service status.
func (a *App) GetStatus() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	uptime := 0
	if a.running && !a.startTime.IsZero() {
		uptime = int(time.Since(a.startTime).Seconds())
	}

	instances := a.bal.GetInstances()
	healthy := 0
	totalConns := 0
	for _, inst := range instances {
		if inst.Healthy {
			healthy++
		}
		totalConns += inst.ActiveConns
	}

	return map[string]interface{}{
		"running":            a.running,
		"port":               a.cfg.Server.Port,
		"instance_count":     len(instances),
		"healthy_count":      healthy,
		"active_connections": totalConns,
		"uptime_seconds":     uptime,
	}
}

// ShowWindow shows the main window.
func (a *App) ShowWindow() {
	wruntime.WindowShow(a.ctx)
}

// HideWindow hides the window (minimize to tray).
func (a *App) HideWindow() {
	wruntime.WindowHide(a.ctx)
}

// Quit quits the application.
func (a *App) Quit() {
	wruntime.Quit(a.ctx)
}

// OpenLogFile opens the log/data directory in the system file manager.
func (a *App) OpenLogFile() {
	dataDir := config.GetDataDir()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dataDir)
	case "windows":
		cmd = exec.Command("explorer", dataDir)
	default:
		cmd = exec.Command("xdg-open", dataDir)
	}
	if err := cmd.Run(); err != nil {
		log.Printf("[Desktop] Failed to open data dir: %v", err)
	}
}
