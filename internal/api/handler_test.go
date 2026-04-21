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
