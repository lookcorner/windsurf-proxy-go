package audit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEntryFinishWritesJSONLine(t *testing.T) {
	dir := t.TempDir()
	path, err := Enable(dir)
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	defer Disable()

	e := New("openai", "/v1/chat/completions", "127.0.0.1")
	e.SetModel("claude-sonnet-4-6", "claude-4.6-sonnet")
	e.SetStream(true)
	e.SetRequestBody([]byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`))
	e.SetUpstreamTarget("127.0.0.1:50604")
	e.RecordUpstreamCall("127.0.0.1:50604", "StartCascade", 12, nil)
	e.RecordUpstreamCall("127.0.0.1:50604", "GetCascadeTrajectorySteps", 8, nil)
	e.RecordUpstreamCall("127.0.0.1:50604", "GetCascadeTrajectorySteps", 9, errors.New("boom"))
	e.AppendStreamText("hello ")
	e.AppendStreamText("world")
	e.Finish(200, nil)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("audit file is empty")
	}
	line := strings.TrimSpace(string(data))
	if strings.Count(line, "\n") != 0 {
		t.Fatalf("expected exactly one JSONL record, got %q", line)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("invalid JSONL: %v\n%s", err, line)
	}
	if got["protocol"] != "openai" {
		t.Errorf("protocol: got %v", got["protocol"])
	}
	if got["model"] != "claude-sonnet-4-6" {
		t.Errorf("model: got %v", got["model"])
	}
	if got["internal_model"] != "claude-4.6-sonnet" {
		t.Errorf("internal_model: got %v", got["internal_model"])
	}
	if got["stream"] != true {
		t.Errorf("stream: got %v", got["stream"])
	}
	if got["status_code"].(float64) != 200 {
		t.Errorf("status_code: got %v", got["status_code"])
	}
	if got["stream_text"] != "hello world" {
		t.Errorf("stream_text: got %v", got["stream_text"])
	}

	upstream, ok := got["upstream"].(map[string]any)
	if !ok {
		t.Fatalf("upstream missing or wrong type: %v", got["upstream"])
	}
	if upstream["target"] != "127.0.0.1:50604" {
		t.Errorf("upstream.target: got %v", upstream["target"])
	}
	calls := upstream["calls"].(map[string]any)
	get := calls["GetCascadeTrajectorySteps"].(map[string]any)
	if get["count"].(float64) != 2 {
		t.Errorf("GetCascadeTrajectorySteps count: got %v", get["count"])
	}
	if get["total_ms"].(float64) != 17 {
		t.Errorf("GetCascadeTrajectorySteps total_ms: got %v", get["total_ms"])
	}
	if get["errors"].(float64) != 1 {
		t.Errorf("GetCascadeTrajectorySteps errors: got %v", get["errors"])
	}
	if get["last_error"] != "boom" {
		t.Errorf("GetCascadeTrajectorySteps last_error: got %v", get["last_error"])
	}

	// request_body should be parsed back as a JSON object, not a string.
	body, ok := got["request_body"].(map[string]any)
	if !ok {
		t.Fatalf("request_body should be embedded JSON object, got %T", got["request_body"])
	}
	if body["model"] != "claude-sonnet-4-6" {
		t.Errorf("request_body.model: got %v", body["model"])
	}
}

func TestContextRoundTrip(t *testing.T) {
	e := New("anthropic", "/v1/messages", "")
	ctx := WithEntry(context.Background(), e)
	if got := FromContext(ctx); got != e {
		t.Fatalf("FromContext mismatch: got %p want %p", got, e)
	}

	if got := FromContext(context.Background()); got != nil {
		t.Fatalf("FromContext should be nil for unaugmented ctx, got %v", got)
	}

	// Calls on a nil entry must not panic.
	var nilEntry *Entry
	nilEntry.SetModel("x", "y")
	nilEntry.SetUpstreamTarget("z")
	nilEntry.RecordUpstreamCall("z", "m", 1, nil)
	nilEntry.AppendStreamText("ignored")
	nilEntry.SetRequestBody([]byte("ignored"))
	nilEntry.SetResponseBody([]byte("ignored"))
	nilEntry.Finish(200, nil) // no logger configured -> no-op
}

func TestEnableCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path, err := Enable(dir)
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	defer Disable()

	if filepath.Dir(path) != dir {
		t.Errorf("audit file outside dir: %s", path)
	}
	if !strings.HasPrefix(filepath.Base(path), "requests-") {
		t.Errorf("unexpected filename: %s", path)
	}
	if got := CurrentPath(); got != path {
		t.Errorf("CurrentPath mismatch: %s vs %s", got, path)
	}
}
