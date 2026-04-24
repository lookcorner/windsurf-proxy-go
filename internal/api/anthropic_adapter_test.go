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

func TestConvertAnthropicRequestPreservesClaudeCodeSystemPrompt(t *testing.T) {
	body := `{
		"model": "claude-opus-4-7",
		"max_tokens": 256,
		"system": [
			{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.111.b2b;"},
			{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."},
			{"type": "text", "text": "You are an interactive agent that helps users with software engineering tasks.\n\nCurrent working directory: /Users/rentong/Downloads/dnmp/goproject/windsurf-proxy-go\n\nIMPORTANT: very long static boilerplate here."},
			{"type": "text", "text": "Contents of /Users/rentong/.claude/CLAUDE.md:\nAlways respond in Chinese (中文).\nProject override: always answer in Chinese."}
		],
		"messages": [
			{"role": "user", "content": "hello"}
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
	if len(messages) != 2 {
		t.Fatalf("expected compacted message, got %d", len(messages))
	}
	if role, _ := messages[0]["role"].(string); role != "system" {
		t.Fatalf("native tools marker role = %q, want system", role)
	}
	if role, _ := messages[1]["role"].(string); role != "user" {
		t.Fatalf("compacted role = %q, want user", role)
	}
	content, _ := messages[1]["content"].(string)
	for _, want := range []string{
		"Working directory: /Users/rentong/Downloads/dnmp/goproject/windsurf-proxy-go",
		"Relevant user/project instructions:",
		"Project override: always answer in Chinese.",
		"User request:",
		"hello",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected compacted prompt to contain %q, got %q", want, content)
		}
	}
	for _, unwanted := range []string{
		"x-anthropic-billing-header",
		"You are Claude Code, Anthropic's official CLI for Claude.",
		"IMPORTANT: very long static boilerplate here.",
	} {
		if strings.Contains(content, unwanted) {
			t.Fatalf("compacted prompt still contains noise %q: %q", unwanted, content)
		}
	}
}

func TestConvertAnthropicRequestCompactsClaudeCodeReminders(t *testing.T) {
	body := `{
		"model": "claude-opus-4-7",
		"max_tokens": 256,
		"system": [
			{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."},
			{"type": "text", "text": "Primary working directory: /repo/project\nHuge boilerplate"}
		],
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "<system-reminder>skills noise</system-reminder>"},
				{"type": "text", "text": "<system-reminder>\nThe user opened the file /repo/project/internal/reuse/reuse.go in the IDE. This may or may not be related to the current task.\n</system-reminder>"},
				{"type": "text", "text": "阅读项目代码"}
			]}
		]
	}`

	var req AnthropicMessagesRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	messages, tools, err := convertAnthropicRequest(&req)
	if err != nil {
		t.Fatalf("convertAnthropicRequest: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools len = %d, want 0", len(tools))
	}
	content, _ := messages[1]["content"].(string)
	for _, want := range []string{
		"Working directory: /repo/project",
		"Opened file: /repo/project/internal/reuse/reuse.go",
		"User request:\n阅读项目代码",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("compacted prompt missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "skills noise") || strings.Contains(content, "Huge boilerplate") {
		t.Fatalf("compacted prompt contains noise:\n%s", content)
	}
}

func TestConvertAnthropicRequestPreservesCustomSystemString(t *testing.T) {
	body := `{
		"model": "claude-opus-4-7",
		"max_tokens": 256,
		"system": "Custom system prompt",
		"messages": [
			{"role": "user", "content": "hello"}
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
	if role, _ := messages[0]["role"].(string); role != "user" {
		t.Fatalf("system context role = %q, want user", role)
	}
	system, _ := messages[0]["content"].(string)
	if !strings.Contains(system, "Custom system prompt") {
		t.Fatalf("system context = %q, want to contain %q", system, "Custom system prompt")
	}
}
