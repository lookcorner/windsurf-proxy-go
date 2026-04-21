// Package config provides configuration loading from config.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

const appName = "windsurf-proxy"

// Config represents the application configuration.
type Config struct {
	Server    ServerConfig     `yaml:"server"`
	APIKeys   []APIKeyEntry    `yaml:"api_keys"`
	Instances []InstanceConfig `yaml:"instances"`
	Balancing BalancingConfig  `yaml:"balancing"`
	Logging   LoggingConfig    `yaml:"logging"`
}

// LoggingConfig controls which logs are persisted to disk.
//
// The text log (proxy-YYYYMMDD.log) is always on because the desktop UI's
// live log stream depends on it; only the per-request audit log is
// opt-in because it stores full request/response bodies and can grow
// quickly.
type LoggingConfig struct {
	// Audit enables the structured per-request audit log
	// (requests-YYYYMMDD.jsonl). Default: false.
	Audit bool `yaml:"audit"`
}

// ServerConfig represents server configuration.
type ServerConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	LogLevel string `yaml:"log_level"`
}

// APIKeyEntry represents an API key configuration.
type APIKeyEntry struct {
	Key           string   `yaml:"key"`
	Name          string   `yaml:"name"`
	RateLimit     int      `yaml:"rate_limit"`
	AllowedModels []string `yaml:"allowed_models"`
}

// InstanceConfig represents a Windsurf instance configuration.
type InstanceConfig struct {
	Name         string `yaml:"name"`
	Type         string `yaml:"type"` // local | manual | standalone
	AutoDiscover bool   `yaml:"auto_discover"`
	Host         string `yaml:"host"`
	GRPCPort     int    `yaml:"grpc_port"`
	CSRFToken    string `yaml:"csrf_token"`
	APIKey       string `yaml:"api_key"`
	Weight       int    `yaml:"weight"`
	// Standalone mode
	Email      string `yaml:"email"`
	Password   string `yaml:"password"`
	BinaryPath string `yaml:"binary_path"`
	ServerPort int    `yaml:"server_port"`
	Version    string `yaml:"version"`
	// Remote mode
	APIServer            string `yaml:"api_server"`
	FirebaseRefreshToken string `yaml:"firebase_refresh_token"`
}

// BalancingConfig represents load balancing configuration.
type BalancingConfig struct {
	Strategy            string  `yaml:"strategy"` // round_robin | weighted_round_robin | least_connections
	HealthCheckInterval int     `yaml:"health_check_interval"`
	MaxRetries          int     `yaml:"max_retries"`
	RetryDelay          float64 `yaml:"retry_delay"`
}

// DefaultConfig returns default configuration.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:     "127.0.0.1",
			Port:     8000,
			LogLevel: "info",
		},
		APIKeys: []APIKeyEntry{
			{
				Key:           "sk-windsurf-change-me",
				Name:          "default",
				RateLimit:     60,
				AllowedModels: []string{"*"},
			},
		},
		Instances: []InstanceConfig{
			{
				Name:         "local",
				Type:         "local",
				AutoDiscover: true,
				Weight:       10,
			},
		},
		Balancing: BalancingConfig{
			Strategy:            "round_robin",
			HealthCheckInterval: 30,
			MaxRetries:          3,
			RetryDelay:          1.0,
		},
		Logging: LoggingConfig{
			Audit: false,
		},
	}
}

// Load loads configuration from a YAML file.
func Load(path string) (*Config, error) {
	if path == "" {
		// Try default paths
		defaultPaths := []string{
			"config.yaml",
			"configs/config.yaml",
			getUserConfigPath(),
		}
		for _, p := range defaultPaths {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
	}

	if path == "" {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultConfig(), nil
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save saves configuration to a YAML file.
func Save(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func getUserConfigPath() string {
	return filepath.Join(GetConfigDir(), "config.yaml")
}

// GetConfigDir returns the platform-specific config directory.
//
// macOS:   ~/Library/Application Support/windsurf-proxy/
// Windows: %APPDATA%/windsurf-proxy/
// Linux:   ~/.config/windsurf-proxy/
func GetConfigDir() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", appName)
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			home, _ := os.UserHomeDir()
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, appName)
	default: // linux and others
		home, _ := os.UserHomeDir()
		// Check XDG_CONFIG_HOME first
		if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
			return filepath.Join(xdgConfig, appName)
		}
		return filepath.Join(home, ".config", appName)
	}
}

// GetDataDir returns the platform-specific data directory for logs and state.
//
// macOS:   ~/Library/Application Support/windsurf-proxy/
// Windows: %LOCALAPPDATA%/windsurf-proxy/
// Linux:   ~/.local/share/windsurf-proxy/
func GetDataDir() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", appName)
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			home, _ := os.UserHomeDir()
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, appName)
	default: // linux and others
		// Check XDG_DATA_HOME first
		if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
			return filepath.Join(xdgData, appName)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", appName)
	}
}

// legacyConfigPath returns the legacy config path (project root)
func legacyConfigPath() string {
	// Try to find the executable path and look for config.yaml nearby
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		return filepath.Join(exeDir, "config.yaml")
	}
	return "config.yaml"
}

// MigrateConfig migrates configuration from legacy path to platform-specific path.
// Returns true if migration was performed, false otherwise.
func MigrateConfig() (bool, error) {
	legacyPath := legacyConfigPath()
	newPath := getUserConfigPath()

	// Check if legacy config exists and new config doesn't
	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		return false, nil // No legacy config to migrate
	}
	if _, err := os.Stat(newPath); err == nil {
		return false, nil // New config already exists, don't overwrite
	}

	// Create new config directory
	newDir := filepath.Dir(newPath)
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Copy the file
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return false, fmt.Errorf("failed to read legacy config: %w", err)
	}

	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return false, fmt.Errorf("failed to write new config: %w", err)
	}

	return true, nil
}
