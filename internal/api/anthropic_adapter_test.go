package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertAnthropicRequestDescribesImages(t *testing.T) {
	// Build an Anthropic request with a text part and a base64 image source.
	body := `{
		"model": "claude-3.5-sonnet",
		"max_tokens": 256,
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "look at this"},
					{
						"type": "image",
						"source": {
							"type": "base64",
							"media_type": "image/png",
							"data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="
						}
					},
					{"type": "text", "text": "what is it?"}
				]
			}
		]
	}`

	var req AnthropicMessagesRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	messages, _, err := convertAnthropicRequest(&req)
	if err != nil {
		t.Fatalf("convertAnthropicRequest: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	content, _ := messages[0]["content"].(string)
	if !strings.Contains(content, "look at this") ||
		!strings.Contains(content, "[image:") ||
		!strings.Contains(content, "image/png") ||
		!strings.Contains(content, "what is it?") {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestConvertAnthropicRequestDescribesURLImages(t *testing.T) {
	body := `{
		"model": "claude-3.5-sonnet",
		"max_tokens": 128,
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "image",
						"source": {"type": "url", "url": "https://example.com/a.jpg"}
					}
				]
			}
		]
	}`

	var req AnthropicMessagesRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	messages, _, err := convertAnthropicRequest(&req)
	if err != nil {
		t.Fatalf("convertAnthropicRequest: %v", err)
	}
	content, _ := messages[0]["content"].(string)
	if !strings.Contains(content, "https://example.com/a.jpg") {
		t.Fatalf("expected URL in marker, got %q", content)
	}
}
