// Package management provides log capturing and user-friendly translation.
package management

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogHook captures log output and broadcasts to WebSocket.
type LogHook struct {
	handler *Handler
	buffer  *bytes.Buffer
	mu      sync.Mutex
	enabled bool

	fileMu sync.Mutex
	file   *os.File
	path   string
}

// Global log hook instance
var globalLogHook *LogHook

// InitLogHook initializes the global log hook.
//
// Safe to call multiple times. The first call installs the io.Writer
// chain (logHook + stderr) so all log output is captured; subsequent
// calls just attach the management handler so log lines start being
// broadcast over the WebSocket. Splitting it this way lets the standalone
// CLI start writing to the log file before its handler is constructed.
func InitLogHook(handler *Handler) {
	ensureGlobalLogHook()
	globalLogHook.mu.Lock()
	globalLogHook.handler = handler
	globalLogHook.enabled = true
	globalLogHook.mu.Unlock()

	log.Printf("[Management] Log hook initialized - 中文日志翻译已启用")
}

// ensureGlobalLogHook lazily creates the singleton log hook and wires it
// into the standard logger. Idempotent.
func ensureGlobalLogHook() {
	if globalLogHook != nil {
		return
	}
	globalLogHook = &LogHook{
		buffer:  bytes.NewBuffer(nil),
		enabled: true,
	}
	log.SetOutput(io.MultiWriter(globalLogHook, os.Stderr))
}

// Write implements io.Writer for log hook.
func (h *LogHook) Write(p []byte) (n int, err error) {
	n = len(p)

	h.fileMu.Lock()
	if h.file != nil {
		_, _ = h.file.Write(p)
	}
	h.fileMu.Unlock()

	if !h.enabled || h.handler == nil {
		return n, nil
	}

	line := strings.TrimSpace(string(p))
	if line == "" {
		return n, nil
	}

	level, message := parseLogLine(line)

	h.handler.BroadcastUserLog(level, message)

	return n, nil
}

// EnableLogFile starts mirroring log output to a file under dir.
//
// The active file is named proxy-YYYYMMDD.log and rotates daily on the
// first write of each calendar day (a new file is opened, the previous
// one is closed). The function is safe to call multiple times — the
// last call wins.
//
// Returns the absolute path of the file currently being written, or an
// error if the directory or file cannot be created.
func EnableLogFile(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("log dir is empty")
	}
	ensureGlobalLogHook()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("proxy-%s.log", time.Now().Format("20060102")))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("open log file: %w", err)
	}

	globalLogHook.fileMu.Lock()
	old := globalLogHook.file
	globalLogHook.file = f
	globalLogHook.path = path
	globalLogHook.fileMu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	go startDailyRotation(dir)

	log.Printf("[Management] Log file enabled: %s", path)
	return path, nil
}

// CurrentLogFilePath returns the path of the active log file, or "" if
// file logging hasn't been enabled.
func CurrentLogFilePath() string {
	if globalLogHook == nil {
		return ""
	}
	globalLogHook.fileMu.Lock()
	defer globalLogHook.fileMu.Unlock()
	return globalLogHook.path
}

// startDailyRotation rotates the log file at local midnight. Only one
// rotation goroutine should be active at any time; subsequent calls to
// EnableLogFile will spawn a new one but the older one will harmlessly
// rotate to the same file path on its next tick.
func startDailyRotation(dir string) {
	for {
		now := time.Now()
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(24 * time.Hour)
		time.Sleep(time.Until(nextMidnight) + time.Second)
		if globalLogHook == nil {
			return
		}
		newPath := filepath.Join(dir, fmt.Sprintf("proxy-%s.log", time.Now().Format("20060102")))
		f, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Printf("[Management] Rotate log file failed: %v", err)
			continue
		}
		globalLogHook.fileMu.Lock()
		old := globalLogHook.file
		globalLogHook.file = f
		globalLogHook.path = newPath
		globalLogHook.fileMu.Unlock()
		if old != nil {
			_ = old.Close()
		}
		log.Printf("[Management] Rotated log file -> %s", newPath)
	}
}

// parseLogLine extracts level and message from a log line.
// Go log format: "YYYY/MM/DD HH:MM:SS [prefix] message"
func parseLogLine(line string) (string, string) {
	// Default level is INFO
	level := "INFO"
	message := line

	// Check for level indicators
	if strings.Contains(line, "ERROR") || strings.Contains(line, "error") || strings.Contains(line, "Error") {
		level = "ERROR"
	} else if strings.Contains(line, "WARNING") || strings.Contains(line, "WARN") || strings.Contains(line, "warning") {
		level = "WARNING"
	} else if strings.Contains(line, "DEBUG") || strings.Contains(line, "debug") {
		level = "DEBUG"
	}

	// Remove timestamp prefix if present
	// Go log format: 2009/01/23 01:23:23 message
	if len(line) > 19 {
		// Check if it starts with date format
		if line[4] == '/' && line[7] == '/' && line[10] == ' ' && line[13] == ':' && line[16] == ':' {
			message = strings.TrimSpace(line[19:])
		}
	}

	return level, message
}

// StopLogHook disables the log hook.
func StopLogHook() {
	if globalLogHook != nil {
		globalLogHook.enabled = false
		log.SetOutput(os.Stderr)
	}
}

// LogBroadcaster provides manual log broadcasting.
// Use this for important events that should always be shown.
type LogBroadcaster struct {
	handler *Handler
}

// NewLogBroadcaster creates a log broadcaster.
func NewLogBroadcaster(handler *Handler) *LogBroadcaster {
	return &LogBroadcaster{handler: handler}
}

// Info broadcasts an info-level log.
func (b *LogBroadcaster) Info(message string) {
	log.Print(message)
}

// Warning broadcasts a warning-level log.
func (b *LogBroadcaster) Warning(message string) {
	log.Printf("WARNING: %s", message)
}

// Error broadcasts an error-level log.
func (b *LogBroadcaster) Error(message string) {
	log.Printf("ERROR: %s", message)
}

// Common user-friendly messages (predefined for easy use)
var UserMessages = struct {
	// Startup
	ServerStarted      string
	HealthCheckStarted string
	ConfigLoaded       string

	// Instance
	InstanceAdded     string
	InstanceRecovered string
	InstanceFailed    string

	// Request
	RequestStarted   string
	RequestCompleted string
	RequestFailed    string

	// Cascade
	CascadeStarted   string
	CascadeCompleted string
	AutoContinue     string
	ToolCall         string
}{
	ServerStarted:      "服务启动成功",
	HealthCheckStarted: "健康检查已启动",
	ConfigLoaded:       "配置加载成功",
	InstanceAdded:      "实例添加成功",
	InstanceRecovered:  "实例已恢复连接",
	InstanceFailed:     "实例连接失败",
	RequestStarted:     "开始处理请求",
	RequestCompleted:   "请求处理完成",
	RequestFailed:      "请求处理失败",
	CascadeStarted:     "Cascade 会话已开始",
	CascadeCompleted:   "Cascade 会话已完成",
	AutoContinue:       "自动继续对话",
	ToolCall:           "工具调用",
}

// BroadcastUserMessage broadcasts a predefined user-friendly message.
func (h *Handler) BroadcastUserMessage(message string) {
	h.BroadcastUserLog("INFO", message)
}
