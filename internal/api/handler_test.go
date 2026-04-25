package api

import (
	"strings"
	"testing"
	"time"
)

func TestConvertMessagesJoinsTextAndDescribesImages(t *testing.T) {
	msgs := []map[string]interface{}{
		{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hello"},
				map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "https://example.com/image.png"}},
				map[string]interface{}{"type": "text", "text": " world"},
			},
		},
	}

	got := convertMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 converted message, got %d", len(got))
	}
	want := "hello\n[image: https://example.com/image.png]\n world"
	if got[0]["content"] != want {
		t.Fatalf("unexpected content:\n got:  %q\n want: %q", got[0]["content"], want)
	}
}

func TestConvertMessagesDescribesDataURLImages(t *testing.T) {
	msgs := []map[string]interface{}{
		{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "see this"},
				map[string]interface{}{
					"type": "image_url",
					"image_url": map[string]interface{}{
						"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=",
					},
				},
			},
		},
	}

	got := convertMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 converted message, got %d", len(got))
	}
	content := got[0]["content"]
	if content == "" {
		t.Fatalf("expected non-empty content with image marker, got empty")
	}
	if !strings.Contains(content, "see this") ||
		!strings.Contains(content, "[image:") ||
		!strings.Contains(content, "image/png") {
		t.Fatalf("expected marker with media type, got %q", content)
	}
}

func TestConvertMessagesPreservesAssistantToolCallHistoryAndToolCallID(t *testing.T) {
	msgs := []map[string]interface{}{
		{
			"role":    "assistant",
			"content": "starting",
			"tool_calls": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": `{"q":"abc"}`,
					},
				},
			},
		},
		{
			"role":         "tool",
			"tool_call_id": "call_1",
			"content":      "done",
		},
	}

	got := convertMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 converted messages, got %d", len(got))
	}
	if got[0]["content"] != "starting\n[called tool search with {\"q\":\"abc\"}]" {
		t.Fatalf("assistant content = %q", got[0]["content"])
	}
	if got[1]["tool_call_id"] != "call_1" {
		t.Fatalf("tool_call_id = %q, want %q", got[1]["tool_call_id"], "call_1")
	}
}

func TestConvertMessagesPreservesTypedToolCallSlices(t *testing.T) {
	msgs := []map[string]interface{}{
		{
			"role":    "assistant",
			"content": "",
			"tool_calls": []map[string]interface{}{
				{
					"function": map[string]interface{}{
						"name":      "Read",
						"arguments": `{"file_path":"README.md"}`,
					},
				},
			},
		},
	}

	got := convertMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 converted message, got %d", len(got))
	}
	if got[0]["content"] != `[called tool Read with {"file_path":"README.md"}]` {
		t.Fatalf("assistant content = %q", got[0]["content"])
	}
}

func TestRetryAttempts(t *testing.T) {
	tests := []struct {
		name       string
		maxRetries int
		want       int
	}{
		{name: "negative", maxRetries: -1, want: 1},
		{name: "zero", maxRetries: 0, want: 1},
		{name: "three retries", maxRetries: 3, want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryAttempts(tt.maxRetries); got != tt.want {
				t.Fatalf("retryAttempts(%d) = %d, want %d", tt.maxRetries, got, tt.want)
			}
		})
	}
}

func TestRetryDelayDuration(t *testing.T) {
	tests := []struct {
		name       string
		retryDelay float64
		want       time.Duration
	}{
		{name: "disabled", retryDelay: 0, want: 0},
		{name: "fractional seconds", retryDelay: 0.25, want: 250 * time.Millisecond},
		{name: "whole second", retryDelay: 1, want: time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryDelayDuration(tt.retryDelay); got != tt.want {
				t.Fatalf("retryDelayDuration(%v) = %v, want %v", tt.retryDelay, got, tt.want)
			}
		})
	}
}

func TestNormalizeToolAdapterOutputFinal(t *testing.T) {
	raw := `{"action":"final","content":"hello"}`

	content, calls := normalizeToolAdapterOutput(raw, true)
	if content != "hello" {
		t.Fatalf("content = %q, want %q", content, "hello")
	}
	if len(calls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(calls))
	}
}

func TestNormalizeToolAdapterOutputToolCall(t *testing.T) {
	raw := `{"action":"tool_call","tool_calls":[{"name":"search","arguments":{"q":"123"}}]}`

	content, calls := normalizeToolAdapterOutput(raw, true)
	if content != "" {
		t.Fatalf("content = %q, want empty", content)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "search" {
		t.Fatalf("tool name = %q, want %q", calls[0].Name, "search")
	}
	if calls[0].Arguments != `{"q":"123"}` {
		t.Fatalf("arguments = %q, want %q", calls[0].Arguments, `{"q":"123"}`)
	}
}

func TestNormalizeToolAdapterOutputLegacyToolTag(t *testing.T) {
	raw := `<tool search {"q":"123"}`

	content, calls := normalizeToolAdapterOutput(raw, true)
	if content != "" {
		t.Fatalf("content = %q, want empty", content)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "search" {
		t.Fatalf("tool name = %q, want %q", calls[0].Name, "search")
	}
	if calls[0].Arguments != `{"q":"123"}` {
		t.Fatalf("arguments = %q, want %q", calls[0].Arguments, `{"q":"123"}`)
	}
}

func TestNormalizeToolAdapterOutputPlainTextDoesNotPanic(t *testing.T) {
	raw := `你只发送了"123"，没有具体任务。请告诉我需要做什么？`

	content, calls := normalizeToolAdapterOutput(raw, true)
	if content != raw {
		t.Fatalf("content = %q, want %q", content, raw)
	}
	if len(calls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(calls))
	}
}

func TestNormalizeToolAdapterOutputExtractsStructuredJSONAfterPrefixText(t *testing.T) {
	raw := `The message looks like test input with no actual question, so I should ask for clarification.{"action":"final","content":"收到消息,但没看到具体任务。"}`

	content, calls := normalizeToolAdapterOutput(raw, true)
	if content != "收到消息,但没看到具体任务。" {
		t.Fatalf("content = %q, want %q", content, "收到消息,但没看到具体任务。")
	}
	if len(calls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(calls))
	}
}
