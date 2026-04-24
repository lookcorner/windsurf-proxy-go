// Package standalone provides binary discovery for Windsurf language server.
package standalone

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// FindFreePort returns an available local TCP port. Used when a standalone
// instance is created without an explicit server_port so multiple
// language_server processes can coexist without colliding on the legacy
// 42100 default. The port is closed before being returned, so there is a
// (tiny) race window before the caller binds it; in practice
// language_server starts immediately after.
func FindFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find free port: %w", err)
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", l.Addr())
	}
	return addr.Port, nil
}

// FindFreePortBlock returns a server port whose adjacent LS and extension
// ports are currently free. language_server binds server_port, server_port+1,
// and this proxy binds server_port+100 for the dummy extension server.
func FindFreePortBlock() (int, error) {
	var lastErr error
	for i := 0; i < 50; i++ {
		port, err := FindFreePort()
		if err != nil {
			return 0, err
		}
		if port+100 > 65535 {
			continue
		}
		ok := true
		for _, candidate := range []int{port, port + 1, port + 100} {
			inUse, checkErr := IsPortInUse(candidate)
			if checkErr != nil {
				lastErr = checkErr
				ok = false
				break
			}
			if inUse {
				ok = false
				break
			}
		}
		if ok {
			return port, nil
		}
	}
	if lastErr != nil {
		return 0, fmt.Errorf("find free port block: %w", lastErr)
	}
	return 0, fmt.Errorf("find free port block: no free port block found")
}

// IsPortInUse reports whether a local TCP port already has a listener.
func IsPortInUse(port int) (bool, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true, nil
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return false, nil
	}
	return false, nil
}

func MaskProxyForLog(raw string) string {
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

// Binary search paths by platform
var binarySearchPaths = map[string][]string{
	"darwin": []string{
		"/Applications/Windsurf.app/Contents/Resources/app/extensions/windsurf/bin/language_server_macos_arm",
		"/Applications/Windsurf.app/Contents/Resources/app/extensions/windsurf/bin/language_server_macos_x64",
		filepath.Join(os.Getenv("HOME"), "Applications", "Windsurf.app", "Contents", "Resources", "app", "extensions", "windsurf", "bin", "language_server_macos_arm"),
		filepath.Join(os.Getenv("HOME"), "Applications", "Windsurf.app", "Contents", "Resources", "app", "extensions", "windsurf", "bin", "language_server_macos_x64"),
	},
	"linux": []string{
		"/usr/share/windsurf/resources/app/extensions/windsurf/bin/language_server_linux_x64",
		filepath.Join(os.Getenv("HOME"), ".local", "share", "windsurf", "resources", "app", "extensions", "windsurf", "bin", "language_server_linux_x64"),
		"/app/bin/language_server", // Docker default
	},
	"windows": []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Windsurf", "resources", "app", "extensions", "windsurf", "bin", "language_server_windows_x64.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES"), "Windsurf", "resources", "app", "extensions", "windsurf", "bin", "language_server_windows_x64.exe"),
	},
}

// Default versions
const (
	DefaultVersion         = "1.9600.41"
	DefaultAPIServerURL    = "https://server.self-serve.windsurf.com"
	DefaultInferenceServer = "https://inference.codeium.com"
)

// FindLanguageServerBinary finds the language_server binary.
// If customPath is provided, uses that path directly.
// Otherwise, searches platform-specific default paths.
func FindLanguageServerBinary(customPath string) (string, error) {
	if customPath != "" {
		if _, err := os.Stat(customPath); err == nil {
			return customPath, nil
		}
		return "", fmt.Errorf("specified binary not found: %s", customPath)
	}

	// Get platform-specific search paths
	paths, ok := binarySearchPaths[runtime.GOOS]
	if !ok {
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// On macOS ARM, prefer arm binary; on macOS Intel, prefer x64
	if runtime.GOOS == "darwin" {
		if runtime.GOARCH == "arm64" {
			// Try ARM first
			for _, path := range paths {
				if strings.Contains(path, "arm") && fileExists(path) {
					return path, nil
				}
			}
			// Fallback to x64
			for _, path := range paths {
				if strings.Contains(path, "x64") && fileExists(path) {
					return path, nil
				}
			}
		} else {
			// Try x64 first
			for _, path := range paths {
				if strings.Contains(path, "x64") && fileExists(path) {
					return path, nil
				}
			}
		}
	}

	// Try all paths
	for _, path := range paths {
		if fileExists(path) {
			return path, nil
		}
	}

	return "", fmt.Errorf("language_server binary not found. Install Windsurf or set binary_path in config")
}

// fileExists checks if a file exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// KillPortHolder kills any process occupying the given port.
// Used to avoid "address already in use" errors.
func KillPortHolder(port int) error {
	if runtime.GOOS == "windows" {
		return killPortHolderWindows(port)
	}
	return killPortHolderUnix(port)
}

// killPortHolderUnix uses lsof to find and kill processes on Unix.
func killPortHolderUnix(port int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lsof", "-ti", fmt.Sprintf(":%d", port))
	output, err := cmd.Output()
	if err != nil {
		return nil // No process found, ignore
	}

	pids := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, pidStr := range pids {
		if pidStr == "" {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if pid == os.Getpid() {
			continue // Don't kill ourselves
		}
		fmt.Printf("[standalone] Killing process %d occupying port %d\n", pid, port)
		// Find the process and send SIGKILL
		p, findErr := os.FindProcess(pid)
		if findErr == nil {
			p.Signal(syscall.SIGKILL)
		}
	}

	return nil
}

// killPortHolderWindows uses netstat + taskkill on Windows.
func killPortHolderWindows(port int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "netstat", "-ano")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, fmt.Sprintf(":%d", port)) && strings.Contains(line, "LISTENING") {
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				pidStr := parts[len(parts)-1]
				pid, err := strconv.Atoi(pidStr)
				if err != nil {
					continue
				}
				if pid == os.Getpid() {
					continue
				}
				fmt.Printf("[standalone] Killing process %d occupying port %d\n", pid, port)
				killCmd := exec.CommandContext(ctx, "taskkill", "/F", "/PID", fmt.Sprintf("%d", pid))
				killCmd.Run()
			}
		}
	}

	return nil
}
