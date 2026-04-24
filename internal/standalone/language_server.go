// Package standalone manages Windsurf language server process.
package standalone

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"windsurf-proxy-go/internal/core/grpc"
	"windsurf-proxy-go/internal/core/protobuf"

	"github.com/google/uuid"
)

// LanguageServerProcess manages a standalone language_server child process.
type LanguageServerProcess struct {
	APIKey       string
	ServerPort   int
	BinaryPath   string
	Version      string
	APIServerURL string
	ProxyURL     string
	CSRFToken    string

	// Internal state
	process      *os.Process
	dbDir        string
	codeiumDir   string
	extPort      int
	extListener  net.Listener
	restartCount int
	maxRestarts  int

	mu             sync.Mutex
	monitorStopCh  chan struct{}
	monitorRunning bool
	wg             sync.WaitGroup
}

// Default settings
const (
	defaultMaxRestarts   = 5
	defaultExtPortOffset = 100
)

// NewLanguageServerProcess creates a new language server process manager.
func NewLanguageServerProcess(
	apiKey string,
	serverPort int,
	binaryPath string,
	version string,
	apiServerURL string,
	proxyURL string,
) (*LanguageServerProcess, error) {
	// Find binary if not specified
	binary, err := FindLanguageServerBinary(binaryPath)
	if err != nil {
		return nil, err
	}

	if version == "" {
		version = DefaultVersion
	}
	if apiServerURL == "" {
		apiServerURL = DefaultAPIServerURL
	}

	// Create temp directories
	dbDir, err := os.MkdirTemp("", "windsurf_ls_db_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create db dir: %w", err)
	}

	codeiumDir, err := os.MkdirTemp("", "windsurf_ls_codeium_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create codeium dir: %w", err)
	}

	return &LanguageServerProcess{
		APIKey:        apiKey,
		ServerPort:    serverPort,
		BinaryPath:    binary,
		Version:       version,
		APIServerURL:  apiServerURL,
		ProxyURL:      strings.TrimSpace(proxyURL),
		CSRFToken:     uuid.New().String(),
		dbDir:         dbDir,
		codeiumDir:    codeiumDir,
		extPort:       serverPort + defaultExtPortOffset,
		maxRestarts:   defaultMaxRestarts,
		monitorStopCh: make(chan struct{}),
	}, nil
}

// IsRunning returns true if the process is still running.
func (ls *LanguageServerProcess) IsRunning() bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.process != nil
}

// buildArgs builds command-line arguments for the language server.
func (ls *LanguageServerProcess) buildArgs() []string {
	return []string{
		ls.BinaryPath,
		"--api_server_url", ls.APIServerURL,
		"--inference_api_server_url", DefaultInferenceServer,
		"--run_child",
		"--enable_lsp",
		"--extension_server_port", strconv.Itoa(ls.extPort),
		"--ide_name", "windsurf",
		"--server_port", strconv.Itoa(ls.ServerPort),
		"--lsp_port", strconv.Itoa(ls.ServerPort + 1),
		"--windsurf_version", ls.Version,
		"--stdin_initial_metadata",
		"--database_dir", ls.dbDir,
		"--codeium_dir", ls.codeiumDir,
		"--enable_local_search",
		"--search_max_workspace_file_count", "5000",
		"--limit_go_max_procs", "4",
		"--workspace_id", "standalone_workspace",
	}
}

// buildEnv builds environment variables for the process.
func (ls *LanguageServerProcess) buildEnv() []string {
	env := os.Environ()
	env = append(env, fmt.Sprintf("WINDSURF_CSRF_TOKEN=%s", ls.CSRFToken))
	if ls.ProxyURL != "" {
		env = append(env,
			fmt.Sprintf("HTTP_PROXY=%s", ls.ProxyURL),
			fmt.Sprintf("HTTPS_PROXY=%s", ls.ProxyURL),
			fmt.Sprintf("http_proxy=%s", ls.ProxyURL),
			fmt.Sprintf("https_proxy=%s", ls.ProxyURL),
		)
	}
	return env
}

// buildStdinMetadata encodes the Metadata protobuf to send via stdin.
func (ls *LanguageServerProcess) buildStdinMetadata() []byte {
	return protobuf.EncodeMetadata(ls.APIKey, ls.Version)
}

func (ls *LanguageServerProcess) currentAPIKey() string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.APIKey
}

func (ls *LanguageServerProcess) setAPIKey(newKey string) {
	ls.mu.Lock()
	ls.APIKey = newKey
	ls.mu.Unlock()
}

// Start launches the language_server process.
func (ls *LanguageServerProcess) Start() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if ls.process != nil {
		log.Printf("[LS] Warning: Language server already running")
		return nil
	}

	for _, port := range []int{ls.ServerPort, ls.ServerPort + 1, ls.extPort} {
		inUse, err := IsPortInUse(port)
		if err != nil {
			return fmt.Errorf("check port %d: %w", port, err)
		}
		if inUse {
			return fmt.Errorf("port %d is already in use", port)
		}
	}

	// Start dummy extension server
	var err error
	ls.extListener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ls.extPort))
	if err != nil {
		return fmt.Errorf("failed to start extension server: %w", err)
	}

	// Accept loop (just close connections immediately)
	go func() {
		for {
			conn, acceptErr := ls.extListener.Accept()
			if acceptErr != nil {
				return // Listener closed
			}
			conn.Close()
		}
	}()

	args := ls.buildArgs()
	env := ls.buildEnv()

	if ls.ProxyURL != "" {
		log.Printf("[LS] Starting language_server on port %d via proxy %s (binary=%s)", ls.ServerPort, MaskProxyForLog(ls.ProxyURL), ls.BinaryPath)
	} else {
		log.Printf("[LS] Starting language_server on port %d (binary=%s)", ls.ServerPort, ls.BinaryPath)
	}
	log.Printf("[LS] Args: %s", strings.Join(args, " "))

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = env

	// Set up stdin pipe for metadata
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		ls.stopExtensionServer()
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// Capture stdout/stderr to os.Stdout
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		ls.stopExtensionServer()
		return fmt.Errorf("failed to start process: %w", err)
	}

	ls.process = cmd.Process

	// Send metadata via stdin
	metadata := ls.buildStdinMetadata()
	stdinPipe.Write(metadata)
	stdinPipe.Close()

	log.Printf("[LS] Language server started (pid=%d)", cmd.Process.Pid)

	return nil
}

// stopExtensionServer stops the dummy TCP listener.
func (ls *LanguageServerProcess) stopExtensionServer() {
	if ls.extListener != nil {
		ls.extListener.Close()
		ls.extListener = nil
	}
}

// WaitForReady waits for the gRPC port to become reachable.
func (ls *LanguageServerProcess) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ls.mu.Lock()
		running := ls.process != nil
		ls.mu.Unlock()

		if !running {
			log.Printf("[LS] Process died during startup")
			return false
		}

		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ls.ServerPort), 1*time.Second)
		if err == nil {
			conn.Close()
			log.Printf("[LS] Language server ready on port %d", ls.ServerPort)
			return true
		}

		time.Sleep(500 * time.Millisecond)
	}

	log.Printf("[LS] Language server failed to become ready within %.0fs", timeout.Seconds())
	return false
}

// InitializeCascadePanelState calls InitializeCascadePanelState on the LS.
// Required for Cascade to work without the IDE providing panel context.
func (ls *LanguageServerProcess) InitializeCascadePanelState() error {
	maxAttempts := 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		client := grpc.NewWindsurfGrpcClient("127.0.0.1", ls.ServerPort, ls.CSRFToken)

		_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := client.InitializeCascadePanelState(ls.currentAPIKey(), ls.Version)
		cancel()

		if err == nil {
			log.Printf("[LS] Cascade panel state initialized")
			return nil
		}

		log.Printf("[LS] InitializeCascadePanelState attempt %d/%d failed: %v", attempt, maxAttempts, err)
		if attempt < maxAttempts {
			time.Sleep(2 * time.Second)
		}
	}

	return fmt.Errorf("InitializeCascadePanelState failed after %d attempts", maxAttempts)
}

// StartAndWait starts the process and waits for readiness.
func (ls *LanguageServerProcess) StartAndWait(timeout time.Duration) error {
	err := ls.Start()
	if err != nil {
		return err
	}

	if !ls.WaitForReady(timeout) {
		return fmt.Errorf("language server not ready")
	}

	// Give gRPC extra time to initialize
	time.Sleep(2 * time.Second)

	// Start monitor
	ls.StartMonitor()

	return nil
}

// StartMonitor starts background monitoring for process health.
func (ls *LanguageServerProcess) StartMonitor() {
	ls.mu.Lock()
	if ls.monitorRunning {
		ls.mu.Unlock()
		return
	}
	ls.monitorStopCh = make(chan struct{})
	ls.monitorRunning = true
	stopCh := ls.monitorStopCh
	ls.mu.Unlock()

	ls.wg.Add(1)
	go ls.monitorLoop(stopCh)
}

// StopMonitor stops the background monitor.
func (ls *LanguageServerProcess) StopMonitor() {
	ls.mu.Lock()
	if !ls.monitorRunning {
		ls.mu.Unlock()
		return
	}
	stopCh := ls.monitorStopCh
	ls.monitorStopCh = nil
	ls.monitorRunning = false
	ls.mu.Unlock()

	close(stopCh)
	ls.wg.Wait()
}

// monitorLoop monitors the LS process and restarts if it dies.
func (ls *LanguageServerProcess) monitorLoop(stopCh chan struct{}) {
	defer ls.wg.Done()
	defer func() {
		ls.mu.Lock()
		if ls.monitorStopCh == stopCh {
			ls.monitorStopCh = nil
			ls.monitorRunning = false
		}
		ls.mu.Unlock()
	}()

	for {
		select {
		case <-stopCh:
			return
		case <-time.After(5 * time.Second):
			// Check process
		}

		ls.mu.Lock()
		process := ls.process
		ls.mu.Unlock()

		if process == nil {
			return
		}

		// Check if process is still alive
		err := process.Signal(syscall.Signal(0))
		if err != nil {
			// Process died
			ls.mu.Lock()
			ls.restartCount++
			count := ls.restartCount
			ls.mu.Unlock()

			log.Printf("[LS] Process died (restarts=%d/%d)", count, ls.maxRestarts)

			if count >= ls.maxRestarts {
				log.Printf("[LS] Max restarts reached, giving up")
				return
			}

			ls.stopExtensionServer()
			ls.mu.Lock()
			ls.process = nil
			ls.mu.Unlock()
			time.Sleep(2 * time.Second) // Let OS reclaim ports

			log.Printf("[LS] Restarting (attempt %d)...", count)

			startErr := ls.Start()
			if startErr != nil {
				log.Printf("[LS] Restart failed: %v", startErr)
				return
			}

			if !ls.WaitForReady(30 * time.Second) {
				log.Printf("[LS] Restart failed — port not ready")
				return
			}
		}
	}
}

// Shutdown gracefully stops the language_server process.
func (ls *LanguageServerProcess) Shutdown() {
	ls.StopMonitor()

	ls.mu.Lock()
	process := ls.process
	ls.process = nil
	ls.mu.Unlock()

	if process != nil {
		log.Printf("[LS] Sending SIGTERM to language_server (pid=%d)", process.Pid)
		process.Signal(syscall.SIGTERM)

		// Wait for graceful shutdown
		done := make(chan struct{})
		go func() {
			process.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Process exited cleanly
		case <-time.After(5 * time.Second):
			log.Printf("[LS] Force-killing language_server")
			process.Kill()
			process.Wait()
		}
	}

	ls.stopExtensionServer()

	// Clean up temp dirs
	os.RemoveAll(ls.dbDir)
	os.RemoveAll(ls.codeiumDir)

	log.Printf("[LS] Language server stopped")
}

// UpdateAPIKey updates the bootstrap API key used for future process starts.
// Request-time API keys are sent per gRPC call, so a key refresh must not
// restart a shared language server that may be serving other accounts.
func (ls *LanguageServerProcess) UpdateAPIKey(newKey string) error {
	ls.setAPIKey(newKey)
	log.Printf("[LS] Updated bootstrap API key without restarting language_server")
	return nil
}

// GetClient returns a gRPC client for this language server.
func (ls *LanguageServerProcess) GetClient() *grpc.WindsurfGrpcClient {
	return grpc.NewWindsurfGrpcClient("127.0.0.1", ls.ServerPort, ls.CSRFToken)
}
