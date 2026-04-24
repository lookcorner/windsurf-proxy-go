package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"windsurf-proxy-go/internal/accounts"
	"windsurf-proxy-go/internal/api"
	"windsurf-proxy-go/internal/audit"
	"windsurf-proxy-go/internal/balancer"
	"windsurf-proxy-go/internal/config"
	"windsurf-proxy-go/internal/keys"
	"windsurf-proxy-go/internal/management"
)

func main() {
	// Parse flags
	configPath := flag.String("c", "", "Path to config.yaml")
	host := flag.String("host", "", "Bind host")
	port := flag.Int("port", 0, "Bind port")
	logDirFlag := flag.String("log-dir", "", "Directory for log files (defaults to <data-dir>/logs)")
	flag.Parse()

	// Set up file logging as early as possible so config-load failures
	// and other startup messages also land on disk.
	logDir := *logDirFlag
	if logDir == "" {
		logDir = filepath.Join(config.GetDataDir(), "logs")
	}
	if path, err := management.EnableLogFile(logDir); err != nil {
		log.Printf("Log file disabled: %v", err)
	} else {
		log.Printf("Log file: %s", path)
	}

	// Try to migrate config from legacy path if no config specified
	if *configPath == "" {
		if migrated, err := config.MigrateConfig(); err != nil {
			log.Printf("Config migration failed: %v", err)
		} else if migrated {
			log.Printf("Config migrated from legacy path to %s", config.GetConfigDir())
		}
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override with flags
	if *host != "" {
		cfg.Server.Host = *host
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}

	log.Printf("Config loaded: host=%s, port=%d, instances=%d",
		cfg.Server.Host, cfg.Server.Port, len(cfg.Instances))

	// Per-request audit log is opt-in; only enable when logging.audit
	// is true in config.
	if cfg.Logging.Audit {
		if path, err := audit.Enable(logDir); err != nil {
			log.Printf("Audit log enable failed: %v", err)
		} else {
			log.Printf("Audit log: %s (logging.audit=true)", path)
		}
	} else {
		log.Printf("Audit log disabled (logging.audit=false; set to true in config to record request bodies)")
	}

	// Create key manager
	keyMgr := keys.NewManager(cfg.APIKeys)
	if keyMgr.Enabled() {
		log.Printf("API key auth enabled (%d keys)", len(cfg.APIKeys))
	} else {
		log.Printf("API key auth disabled — all requests allowed")
	}

	accountMgr := accounts.NewManager(cfg)

	// Create load balancer
	bal := balancer.New(&cfg.Balancing, accountMgr)

	// Initialize instances from config
	bal.InitFromConfigs(cfg.Instances)

	// Start health checks
	bal.StartHealthChecks()

	// Create management handler
	mgmtHandler := management.NewHandler(bal, keyMgr, accountMgr, cfg, *configPath)
	mgmtHandler.SetLoggingChangedHandler(func(next config.LoggingConfig) {
		if next.Audit {
			if path, err := audit.Enable(logDir); err != nil {
				log.Printf("Audit log enable failed: %v", err)
			} else {
				log.Printf("Audit log: %s (logging.audit=true)", path)
			}
		} else {
			audit.Disable()
			log.Printf("Audit log disabled (logging.audit=false)")
		}
	})

	// Attach the management handler so subsequent log lines start being
	// broadcast to WebSocket clients (file logging is already active).
	management.InitLogHook(mgmtHandler)

	// Create API handler
	handler := api.NewHandler(bal, accountMgr, keyMgr, cfg, mgmtHandler.RecordRequest)

	// Create HTTP server
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	mgmtHandler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long timeout for streaming
	}

	// Start server
	go func() {
		log.Printf("Server starting on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	mgmtHandler.Stop()

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server.Shutdown(ctx)
	bal.Stop()
	accountMgr.Stop()

	log.Println("Server stopped")
}
