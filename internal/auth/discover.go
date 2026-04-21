// Package auth provides credential discovery for Windsurf language server.
package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// Platform-specific paths for state.vscdb
var vscodeStatePaths = map[string]string{
	"darwin":  filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Windsurf", "User", "globalStorage", "state.vscdb"),
	"linux":   filepath.Join(os.Getenv("HOME"), ".config", "Windsurf", "User", "globalStorage", "state.vscdb"),
	"windows": filepath.Join(os.Getenv("APPDATA"), "Windsurf", "User", "globalStorage", "state.vscdb"),
}

// Legacy config path
var legacyConfigPath = filepath.Join(os.Getenv("HOME"), ".codeium", "config.json")

// Language server process patterns by platform
var languageServerPatterns = map[string]string{
	"darwin":  "language_server_macos",
	"linux":   "language_server_linux",
	"windows": "language_server_windows",
}

// getLSPattern returns the pattern for the current platform.
func getLSPattern() string {
	pattern, ok := languageServerPatterns[runtime.GOOS]
	if !ok {
		return "language_server"
	}
	return pattern
}

// ============================================================================
// Process inspection
// ============================================================================

// getProcessInfo returns process info containing the language server.
func getProcessInfo() (string, error) {
	pattern := getLSPattern()

	if runtime.GOOS == "windows" {
		return getProcessInfoWindows(pattern)
	}

	return getProcessInfoUnix(pattern)
}

// getProcessInfoUnix uses ps aux on Unix systems.
func getProcessInfoUnix(pattern string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ps failed: %w", err)
	}

	lines := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, pattern) && !strings.Contains(line, "grep") {
			lines = append(lines, line)
		}
	}

	if len(lines) == 0 {
		return "", fmt.Errorf("language server process not found")
	}

	return strings.Join(lines, "\n"), nil
}

// getProcessInfoWindows uses PowerShell on Windows.
func getProcessInfoWindows(pattern string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try PowerShell approach
	psCmd := fmt.Sprintf(
		"Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*%s*' } | Select-Object ProcessId,CommandLine | Format-List",
		pattern,
	)

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err == nil && strings.Contains(string(output), pattern) {
		return string(output), nil
	}

	// Fallback: wmic
	wmicCmd := exec.CommandContext(ctx, "wmic", "process", "where",
		fmt.Sprintf("name like '%s%%'", pattern),
		"get", "ProcessId,CommandLine", "/FORMAT:LIST")
	output, err = wmicCmd.Output()
	if err == nil && strings.Contains(string(output), pattern) {
		return string(output), nil
	}

	return "", fmt.Errorf("language server process not found on Windows")
}

// ============================================================================
// Credential extraction
// ============================================================================

// GetCSRFToken extracts the CSRF token from the Windsurf language server.
// Strategy:
//  1. Get the PID of the language server
//  2. Read WINDSURF_CSRF_TOKEN from its environment (ps eww)
//  3. Fallback: parse --csrf_token from command-line args (older versions)
func GetCSRFToken() (string, error) {
	pid, err := getPID()
	if err != nil {
		return "", fmt.Errorf("Windsurf language server not running — is Windsurf open?")
	}

	// Strategy 1: Read from process environment (Windsurf 1.96+)
	if runtime.GOOS == "windows" {
		return getCSRFTokenWindows(pid)
	}

	return getCSRFTokenUnix(pid)
}

// getCSRFTokenUnix reads CSRF token from process environment on Unix.
func getCSRFTokenUnix(pid string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// ps eww shows environment variables
	cmd := exec.CommandContext(ctx, "ps", "eww", pid)
	output, err := cmd.Output()
	if err == nil {
		// Look for WINDSURF_CSRF_TOKEN in environment
		re := regexp.MustCompile(`WINDSURF_CSRF_TOKEN=([a-f0-9-]+)`)
		match := re.FindStringSubmatch(string(output))
		if len(match) > 1 {
			return match[1], nil
		}
	}

	// Strategy 2: Fallback to --csrf_token arg
	info, err := getProcessInfo()
	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`--csrf_token\s+([a-f0-9-]+)`)
	match := re.FindStringSubmatch(info)
	if len(match) > 1 {
		return match[1], nil
	}

	return "", fmt.Errorf("CSRF token not found. Checked: WINDSURF_CSRF_TOKEN env, --csrf_token arg")
}

// getCSRFTokenWindows extracts CSRF token on Windows.
func getCSRFTokenWindows(pid string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	psCmd := fmt.Sprintf("(Get-CimInstance Win32_Process -Filter \"ProcessId=%s\").CommandLine", pid)
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", psCmd)
	output, err := cmd.Output()
	if err == nil {
		// Check for WINDSURF_CSRF_TOKEN in command line
		re := regexp.MustCompile(`WINDSURF_CSRF_TOKEN=([a-f0-9-]+)`)
		match := re.FindStringSubmatch(string(output))
		if len(match) > 1 {
			return match[1], nil
		}

		// Check for --csrf_token arg
		re = regexp.MustCompile(`--csrf_token\s+([a-f0-9-]+)`)
		match = re.FindStringSubmatch(string(output))
		if len(match) > 1 {
			return match[1], nil
		}
	}

	return "", fmt.Errorf("CSRF token not found on Windows")
}

// getExtensionServerPort extracts --extension_server_port from the running process.
func getExtensionServerPort() (int, error) {
	info, err := getProcessInfo()
	if err != nil {
		return 0, err
	}

	re := regexp.MustCompile(`--extension_server_port\s+(\d+)`)
	match := re.FindStringSubmatch(info)
	if len(match) > 1 {
		return strconv.Atoi(match[1])
	}

	return 0, fmt.Errorf("extension_server_port not found")
}

// getPID returns the PID of the language server process.
func getPID() (string, error) {
	info, err := getProcessInfo()
	if err != nil {
		return "", err
	}

	if runtime.GOOS == "windows" {
		// wmic/PowerShell output: ProcessId=12345 or ProcessId : 12345
		re := regexp.MustCompile(`ProcessId\s*[=:]\s*(\d+)`)
		match := re.FindStringSubmatch(info)
		if len(match) > 1 {
			return match[1], nil
		}
		return "", fmt.Errorf("PID not found in Windows output")
	}

	// Unix: ps aux output — prefer Windsurf.app line
	lines := strings.Split(strings.TrimSpace(info), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Windsurf.app") || strings.Contains(line, "Windsurf") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
	}

	// Fallback: first match
	if len(lines) > 0 {
		parts := strings.Fields(lines[0])
		if len(parts) >= 2 {
			return parts[1], nil
		}
	}

	return "", fmt.Errorf("PID not found")
}

// discoverListenPorts discovers LISTEN ports for a given PID.
func discoverListenPorts(pid string) ([]int, error) {
	if runtime.GOOS == "windows" {
		return discoverListenPortsWindows(pid)
	}
	return discoverListenPortsUnix(pid)
}

// discoverListenPortsUnix uses lsof to find LISTEN ports on Unix.
//
// IMPORTANT: lsof selection flags default to OR, so `-p PID -i` means
// "belonging to PID X OR any IP socket" and dumps every LISTEN on the
// machine. The `-a` flag forces AND across selections; we also restrict
// to TCP LISTEN explicitly so the caller only sees ports actually opened
// by the language_server PID itself.
func discoverListenPortsUnix(pid string) ([]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lsof", "-a", "-p", pid, "-iTCP", "-sTCP:LISTEN", "-P", "-n")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("lsof failed: %w", err)
	}

	ports := []int{}
	re := regexp.MustCompile(`:(\d+)\s+\(LISTEN\)`)
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "LISTEN") {
			match := re.FindStringSubmatch(line)
			if len(match) > 1 {
				port, _ := strconv.Atoi(match[1])
				ports = append(ports, port)
			}
		}
	}

	return ports, nil
}

// discoverListenPortsWindows uses netstat to find LISTEN ports on Windows.
func discoverListenPortsWindows(pid string) ([]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "netstat", "-ano")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("netstat failed: %w", err)
	}

	ports := []int{}
	re := regexp.MustCompile(`:(\d+)\s`)
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "LISTENING") && strings.HasSuffix(strings.TrimSpace(line), pid) {
			match := re.FindStringSubmatch(line)
			if len(match) > 1 {
				port, _ := strconv.Atoi(match[1])
				ports = append(ports, port)
			}
		}
	}

	// Deduplicate
	unique := make(map[int]bool)
	for _, p := range ports {
		unique[p] = true
	}
	result := make([]int, 0, len(unique))
	for p := range unique {
		result = append(result, p)
	}

	return result, nil
}

// GetGRPCPort discovers the gRPC port used by the local language server.
// Strategy:
//  1. Use lsof to find actual LISTEN ports for the process PID
//  2. Pick the first port > extension_server_port
//  3. Fallback: extension_server_port + 3
func GetGRPCPort() (int, error) {
	pid, err := getPID()
	if err != nil {
		return 0, err
	}

	extPort, err := getExtensionServerPort()
	if err != nil {
		extPort = 0
	}

	// Try port discovery based on OS
	ports, err := discoverListenPorts(pid)
	if err == nil && len(ports) > 0 {
		if extPort > 0 {
			// Pick first port > extension_server_port
			candidates := []int{}
			for _, p := range ports {
				if p > extPort {
					candidates = append(candidates, p)
				}
			}
			if len(candidates) > 0 {
				return candidates[0], nil
			}
		}
		return ports[0], nil
	}

	// Fallback: offset-based
	if extPort > 0 {
		return extPort + 3, nil
	}

	return 0, fmt.Errorf("Cannot discover gRPC port. Is Windsurf running?")
}

// GetAPIKey reads the Windsurf/Codeium API key.
// Tries (in order):
//  1. VSCode state.vscdb SQLite DB (windsurfAuthStatus)
//  2. Legacy ~/.codeium/config.json
func GetAPIKey() (string, error) {
	// 1. Try state.vscdb
	statePath, ok := vscodeStatePaths[runtime.GOOS]
	if ok {
		if _, err := os.Stat(statePath); err == nil {
			key, err := readAPIKeyFromStateDB(statePath)
			if err == nil && key != "" {
				return key, nil
			}
		}
	}

	// 2. Legacy config
	if _, err := os.Stat(legacyConfigPath); err == nil {
		data, err := os.ReadFile(legacyConfigPath)
		if err == nil {
			var config map[string]interface{}
			if err := json.Unmarshal(data, &config); err == nil {
				if key, ok := config["apiKey"].(string); ok && key != "" {
					return key, nil
				}
				if key, ok := config["api_key"].(string); ok && key != "" {
					return key, nil
				}
			}
		}
	}

	return "", fmt.Errorf("API key not found. Please login to Windsurf first. Checked: state.vscdb, ~/.codeium/config.json")
}

// readAPIKeyFromStateDB reads API key from VSCode state.vscdb SQLite.
func readAPIKeyFromStateDB(path string) (string, error) {
	// Use modernc.org/sqlite via database/sql (pure Go, no CGO)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return "", fmt.Errorf("failed to open state.vscdb: %w", err)
	}
	defer db.Close()

	// Query for windsurfAuthStatus
	var value string
	err = db.QueryRow("SELECT value FROM ItemTable WHERE key = 'windsurfAuthStatus'").Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("windsurfAuthStatus not found in state.vscdb")
		}
		return "", fmt.Errorf("query failed: %w", err)
	}

	// Parse JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return "", fmt.Errorf("failed to parse windsurfAuthStatus: %w", err)
	}

	key, ok := parsed["apiKey"].(string)
	if !ok || key == "" {
		return "", fmt.Errorf("apiKey not found in windsurfAuthStatus")
	}

	return key, nil
}

// GetWindsurfVersion returns Windsurf version from process arguments.
func GetWindsurfVersion() string {
	info, err := getProcessInfo()
	if err != nil {
		return "1.13.104"
	}

	re := regexp.MustCompile(`--windsurf_version\s+(\S+)`)
	match := re.FindStringSubmatch(info)
	if len(match) > 1 {
		// Remove build suffix (+something)
		version := match[1]
		if idx := strings.Index(version, "+"); idx > 0 {
			version = version[:idx]
		}
		return version
	}

	return "1.13.104"
}

// ============================================================================
// Public API
// ============================================================================

// DiscoverCredentials discovers all credentials from the running Windsurf instance.
func DiscoverCredentials() (*WindsurfCredentials, error) {
	csrfToken, err := GetCSRFToken()
	if err != nil {
		return nil, err
	}

	grpcPort, err := GetGRPCPort()
	if err != nil {
		return nil, err
	}

	apiKey, err := GetAPIKey()
	if err != nil {
		return nil, err
	}

	version := GetWindsurfVersion()

	return &WindsurfCredentials{
		CSRFToken: csrfToken,
		GRPCPort:  grpcPort,
		APIKey:    apiKey,
		Version:   version,
	}, nil
}

// IsWindsurfRunning checks if Windsurf language server is running.
func IsWindsurfRunning() bool {
	_, err := getProcessInfo()
	return err == nil
}
