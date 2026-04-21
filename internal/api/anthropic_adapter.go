package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// convertAnthropicRequest translates an incoming Anthropic /v1/messages
// request into the internal representation (messages + tools) consumed by
// the rest of the proxy pipeline.
//
// The shapes returned here match what convertMessages / tool_adapter already
// expects downstream: messages is []map[string]interface{} with optional
// tool_calls + tool_call_id, tools is []map[string]interface{} in
// OpenAI-function form.
func convertAnthropicRequest(req *AnthropicMessagesRequest) (
	messages []map[string]interface{},
	tools []map[string]interface{},
	err error,
) {
	messages = make([]map[string]interface{}, 0, len(req.Messages)+1)

	if sys := flattenAnthropicSystem(req.System); sys != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": sys,
		})
	}

	for i, m := range req.Messages {
		role := m.Role
		if role != "user" && role != "assistant" {
			return nil, nil, fmt.Errorf("messages[%d].role must be 'user' or 'assistant'", i)
		}

		// Content may be a plain string...
		if text, ok := decodeMaybeString(m.Content); ok {
			messages = append(messages, map[string]interface{}{
				"role":    role,
				"content": text,
			})
			continue
		}

		// ...or an array of content blocks.
		var blocks []AnthropicBlock
		if len(m.Content) > 0 {
			if decodeErr := json.Unmarshal(m.Content, &blocks); decodeErr != nil {
				return nil, nil, fmt.Errorf("messages[%d].content: %w", i, decodeErr)
			}
		}

		var (
			textBuf   strings.Builder
			toolCalls []map[string]interface{}
		)

		for _, b := range blocks {
			switch b.Type {
			case "text":
				if textBuf.Len() > 0 {
					textBuf.WriteString("\n")
				}
				textBuf.WriteString(b.Text)

			case "thinking":
				// Upstream may echo the thinking block back when the client
				// replays assistant turns. Preserve it as plain text so we
				// don't lose context, but don't synthesize extra events.
				if b.Thinking != "" {
					if textBuf.Len() > 0 {
						textBuf.WriteString("\n")
					}
					textBuf.WriteString(b.Thinking)
				}

			case "tool_use":
				// Assistant-side tool call. Encode as OpenAI-style tool_calls
				// so the existing tool_adapter path can see it.
				args := string(b.Input)
				if args == "" || args == "null" {
					args = "{}"
				}
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   b.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      b.Name,
						"arguments": args,
					},
				})

			case "tool_result":
				// User-side tool result. Flush any accumulated text for the
				// current message first, then emit a standalone role=tool
				// message referencing the original tool_use id.
				if textBuf.Len() > 0 {
					messages = append(messages, map[string]interface{}{
						"role":    role,
						"content": textBuf.String(),
					})
					textBuf.Reset()
				}
				resultText := flattenToolResultContent(b.Content)
				if b.IsError && resultText != "" {
					resultText = "[error] " + resultText
				}
				messages = append(messages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": b.ToolUseID,
					"content":      resultText,
				})

			case "image":
				// Cascade/gRPC path does not currently carry binary image
				// payloads, so we surface the image as a text marker that
				// includes media type and size. The model won't see the
				// pixels, but it will at least know something was attached.
				if textBuf.Len() > 0 {
					textBuf.WriteString("\n")
				}
				textBuf.WriteString(describeAnthropicImage(b.Source))
			}
		}

		if textBuf.Len() > 0 || len(toolCalls) > 0 {
			msg := map[string]interface{}{
				"role":    role,
				"content": textBuf.String(),
			}
			if len(toolCalls) > 0 {
				msg["tool_calls"] = toolCalls
			}
			messages = append(messages, msg)
		}
	}

	// Tool definitions: Anthropic input_schema maps 1:1 to OpenAI parameters.
	for _, t := range req.Tools {
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		})
	}

	return messages, tools, nil
}

// flattenAnthropicSystem reduces the system field (which may be a plain
// string or a []AnthropicBlock) to a single prompt string.
func flattenAnthropicSystem(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if text, ok := decodeMaybeString(raw); ok {
		return text
	}
	var blocks []AnthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var buf strings.Builder
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if buf.Len() > 0 {
				buf.WriteString("\n")
			}
			buf.WriteString(b.Text)
		}
	}
	return buf.String()
}

// flattenToolResultContent turns a tool_result's content (string | []block)
// into plain text that a chat-completion-style tool role can carry.
func flattenToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if text, ok := decodeMaybeString(raw); ok {
		return text
	}
	var blocks []AnthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		// Fall back to the raw JSON so nothing is silently dropped.
		return strings.TrimSpace(string(raw))
	}
	var buf strings.Builder
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if buf.Len() > 0 {
				buf.WriteString("\n")
			}
			buf.WriteString(b.Text)
		}
	}
	return buf.String()
}

// decodeMaybeString returns (value, true) iff raw is a JSON string.
func decodeMaybeString(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err != nil {
		return "", false
	}
	return s, true
}

// describeAnthropicImage renders an image source (base64 or url) as a compact
// text marker. The caller inlines the result into the prompt.
func describeAnthropicImage(src *AnthropicImageSource) string {
	if src == nil {
		return "[image]"
	}
	return describeImage(src.MediaType, src.Data, src.URL)
}
