// Package audit writes structured per-request audit logs.
//
// One JSON Lines record is emitted per HTTP request handled by the
// proxy. Each record captures:
//
//   - the incoming request body (truncated for safety),
//   - the resolved upstream Windsurf gRPC target (host:port) plus a
//     count/timing breakdown of the gRPC methods that were invoked,
//   - the outgoing response body (or, for streamed responses, the
//     concatenated assistant text and tool calls),
//   - status, duration and any error string.
//
// The audit log is written to its own file (separate from the
// human-readable proxy log) so it can be trivially diffed, grep'd, and
// fed into log analysis pipelines.
//
// Audit records may contain end-user content (chat messages, tool
// arguments). The file is created with mode 0600 to keep it
// owner-readable only.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MaxBodyBytes caps how much of the request and response body we keep
// per record. Anything beyond is dropped and signaled via the
// "_truncated_to" field in the surrounding object. 1 MiB is plenty for
// the largest realistic chat payloads we've seen (~600 KB system prompts).
const MaxBodyBytes = 1 << 20

// ============================================================================
// Singleton logger
// ============================================================================

var (
	loggerMu sync.RWMutex
	logger   *Logger
)

// Logger writes audit entries to a daily-rotated file.
type Logger struct {
	mu   sync.Mutex
	dir  string
	file *os.File
	day  string // YYYYMMDD of currently open file
}

// Enable opens (or rotates to) today's audit log file under dir and
// installs it as the global logger. Safe to call multiple times.
//
// Returns the path of the file currently in use.
func Enable(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("audit dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create audit dir: %w", err)
	}

	loggerMu.Lock()
	defer loggerMu.Unlock()

	if logger == nil {
		logger = &Logger{dir: dir}
	} else {
		logger.dir = dir
	}
	path, err := logger.openTodayLocked()
	if err != nil {
		return "", err
	}
	go logger.rotateDaily()
	return path, nil
}

// Disable closes the active audit file, if any. Subsequent calls to
// Write are no-ops until Enable is called again.
func Disable() {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	if logger != nil && logger.file != nil {
		_ = logger.file.Close()
		logger.file = nil
	}
}

// CurrentPath returns the absolute path of the active audit file, or ""
// if auditing is disabled.
func CurrentPath() string {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	if logger == nil || logger.file == nil {
		return ""
	}
	return logger.file.Name()
}

func (l *Logger) openTodayLocked() (string, error) {
	day := time.Now().Format("20060102")
	path := filepath.Join(l.dir, "requests-"+day+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", fmt.Errorf("open audit file: %w", err)
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	l.file = f
	l.day = day
	return path, nil
}

// rotateDaily rotates the log file at local midnight.
func (l *Logger) rotateDaily() {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(24 * time.Hour)
		time.Sleep(time.Until(next) + time.Second)
		l.mu.Lock()
		if path, err := l.openTodayLocked(); err != nil {
			log.Printf("[audit] rotate failed: %v", err)
		} else {
			log.Printf("[audit] rotated -> %s", path)
		}
		l.mu.Unlock()
	}
}

func (l *Logger) write(b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	_, _ = l.file.Write(b)
	_, _ = l.file.Write([]byte("\n"))
}

// ============================================================================
// Entry
// ============================================================================

// UpstreamCalls aggregates per-method statistics for the gRPC calls
// issued while handling one HTTP request.
type UpstreamCalls struct {
	Target string                  `json:"target"`
	Calls  map[string]*UpstreamAgg `json:"calls"`
}

// UpstreamAgg accumulates the count + total latency of one gRPC method.
type UpstreamAgg struct {
	Count     int    `json:"count"`
	TotalMs   int64  `json:"total_ms"`
	Errors    int    `json:"errors,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

// Entry is the JSON record written to the audit log for one HTTP
// request.
type Entry struct {
	mu sync.Mutex

	Timestamp     time.Time       `json:"ts"`
	RequestID     string          `json:"request_id"`
	Protocol      string          `json:"protocol"` // "openai" | "anthropic"
	Endpoint      string          `json:"endpoint"`
	ClientIP      string          `json:"client_ip,omitempty"`
	Model         string          `json:"model,omitempty"`
	InternalModel string          `json:"internal_model,omitempty"`
	Stream        bool            `json:"stream"`
	RequestBody   json.RawMessage `json:"request_body,omitempty"`
	RequestTrunc  bool            `json:"request_truncated,omitempty"`
	Upstream      *UpstreamCalls  `json:"upstream,omitempty"`
	ResponseBody  json.RawMessage `json:"response_body,omitempty"`
	StreamText    string          `json:"stream_text,omitempty"`
	StatusCode    int             `json:"status_code"`
	DurationMs    int64           `json:"duration_ms"`
	Error         string          `json:"error,omitempty"`
}

// New starts a new audit entry. Always returns a non-nil *Entry, even
// when auditing is disabled, so call-sites don't need nil checks. When
// disabled, Finish becomes a no-op.
func New(protocol, endpoint, clientIP string) *Entry {
	return &Entry{
		Timestamp: time.Now(),
		RequestID: shortID(),
		Protocol:  protocol,
		Endpoint:  endpoint,
		ClientIP:  clientIP,
	}
}

// SetModel records the requested and resolved model names.
func (e *Entry) SetModel(requested, internal string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.Model = requested
	e.InternalModel = internal
	e.mu.Unlock()
}

// SetStream marks the request as streaming.
func (e *Entry) SetStream(stream bool) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.Stream = stream
	e.mu.Unlock()
}

// SetRequestBody stores up to MaxBodyBytes of the raw incoming body.
// Non-JSON bodies are stored as a JSON string for safety.
func (e *Entry) SetRequestBody(b []byte) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.RequestBody, e.RequestTrunc = encodeBody(b)
}

// SetResponseBody stores up to MaxBodyBytes of the response body.
func (e *Entry) SetResponseBody(b []byte) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ResponseBody, _ = encodeBody(b)
}

// AppendStreamText concatenates a streamed text delta onto the
// stream_text field. Tool-call JSON arguments may be appended too —
// pass them as text so they are auditable.
func (e *Entry) AppendStreamText(s string) {
	if e == nil || s == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.StreamText)+len(s) > MaxBodyBytes {
		s = s[:MaxBodyBytes-len(e.StreamText)]
		if s == "" {
			return
		}
	}
	e.StreamText += s
}

// SetUpstreamTarget records the host:port of the chosen Windsurf
// language_server. Subsequent RecordUpstreamCall invocations roll up
// into the same UpstreamCalls aggregator regardless of how many gRPC
// methods get invoked.
func (e *Entry) SetUpstreamTarget(target string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Upstream == nil {
		e.Upstream = &UpstreamCalls{Calls: map[string]*UpstreamAgg{}}
	}
	e.Upstream.Target = target
}

// RecordUpstreamCall logs one gRPC unary or streaming call. Concurrent
// callers (e.g. the polling loop) are safe.
func (e *Entry) RecordUpstreamCall(target, method string, durationMs int64, err error) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Upstream == nil {
		e.Upstream = &UpstreamCalls{Calls: map[string]*UpstreamAgg{}}
	}
	if e.Upstream.Target == "" {
		e.Upstream.Target = target
	}
	agg := e.Upstream.Calls[method]
	if agg == nil {
		agg = &UpstreamAgg{}
		e.Upstream.Calls[method] = agg
	}
	agg.Count++
	agg.TotalMs += durationMs
	if err != nil {
		agg.Errors++
		agg.LastError = err.Error()
	}
}

// Finish stamps the duration / status / error fields and writes the
// entry to the audit log file. No-op if auditing is disabled or if
// Finish has already been called for this entry.
func (e *Entry) Finish(statusCode int, err error) {
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.DurationMs != 0 {
		e.mu.Unlock()
		return // already finished
	}
	e.DurationMs = time.Since(e.Timestamp).Milliseconds()
	if e.DurationMs == 0 {
		e.DurationMs = 1 // distinguish from "unfinished" sentinel
	}
	e.StatusCode = statusCode
	if err != nil {
		e.Error = err.Error()
	}
	payload, jerr := json.Marshal(e)
	e.mu.Unlock()
	if jerr != nil {
		log.Printf("[audit] marshal entry %s failed: %v", e.RequestID, jerr)
		return
	}
	loggerMu.RLock()
	l := logger
	loggerMu.RUnlock()
	if l == nil {
		return
	}
	l.write(payload)
}

// ============================================================================
// Context helpers
// ============================================================================

type ctxKey struct{}

// WithEntry attaches an audit entry to ctx. Downstream code can call
// FromContext(ctx) to fetch it without depending on this package's
// caller.
func WithEntry(parent context.Context, e *Entry) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, ctxKey{}, e)
}

// FromContext returns the audit entry attached to ctx, or nil.
func FromContext(ctx context.Context) *Entry {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(ctxKey{}); v != nil {
		if e, ok := v.(*Entry); ok {
			return e
		}
	}
	return nil
}

// ============================================================================
// helpers
// ============================================================================

// encodeBody returns body as json.RawMessage when it is valid JSON,
// otherwise as a JSON string (so the audit line stays valid JSONL).
// Bodies larger than MaxBodyBytes are truncated and the truncated flag
// is returned.
func encodeBody(b []byte) (json.RawMessage, bool) {
	if len(b) == 0 {
		return nil, false
	}
	truncated := false
	if len(b) > MaxBodyBytes {
		b = b[:MaxBodyBytes]
		truncated = true
	}
	if json.Valid(b) {
		out := make([]byte, len(b))
		copy(out, b)
		return out, truncated
	}
	// Fall back: encode as JSON string.
	encoded, err := json.Marshal(string(b))
	if err != nil {
		return nil, truncated
	}
	return encoded, truncated
}

// shortID returns an 8-byte hex string. Cheaper than uuid and entirely
// sufficient since the timestamp + request_id pair is unique within a
// single proxy instance.
func shortID() string {
	var b [8]byte
	if _, err := readRandom(b[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out)
}
