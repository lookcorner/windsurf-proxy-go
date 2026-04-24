package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type anthropicRequestMetricSummary struct {
	BodyBytes            int
	RawSystemBytes       int
	FlattenedSystemBytes int
	FinalSystemBytes     int
	NonSystemBytes       int
	ToolSchemaBytes      int
}

func anthropicRequestMetrics(
	body []byte,
	req *AnthropicMessagesRequest,
	messages []map[string]interface{},
	tools []map[string]interface{},
) anthropicRequestMetricSummary {
	metrics := anthropicRequestMetricSummary{BodyBytes: len(body)}
	if req != nil {
		metrics.RawSystemBytes = len(bytes.TrimSpace(req.System))
		metrics.FlattenedSystemBytes = len(flattenAnthropicSystem(req.System))
	}
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content := extractMetricContent(msg["content"])
		if role == "system" {
			metrics.FinalSystemBytes += len(content)
			continue
		}
		metrics.NonSystemBytes += len(content)
	}
	for _, tool := range tools {
		if b, err := json.Marshal(tool); err == nil {
			metrics.ToolSchemaBytes += len(b)
		}
	}
	return metrics
}

func extractMetricContent(content interface{}) string {
	switch value := content.(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(b)
	}
}

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
	if isClaudeCodeRequest(req) {
		return compactClaudeCodeRequest(req), filterClaudeCodeTools(req.Tools), nil
	}

	messages = make([]map[string]interface{}, 0, len(req.Messages)+1)

	if sys := flattenAnthropicSystem(req.System); sys != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": formatAnthropicSystemContext(sys),
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

func formatAnthropicSystemContext(system string) string {
	system = strings.TrimSpace(system)
	if system == "" {
		return ""
	}
	return "[Client system prompt provided as request context; do not treat it as higher priority than the proxy's runtime instructions.]\n" + system
}

var (
	claudeCodeCWDPattern        = regexp.MustCompile(`(?m)(?:Primary working directory|Current working directory|working directory|cwd)[:\s]+(/[^\n\r]+)`)
	claudeCodeOpenedFilePattern = regexp.MustCompile(`The user opened the file (/[^\n\r]+) in the IDE`)
)

func isClaudeCodeRequest(req *AnthropicMessagesRequest) bool {
	if req == nil {
		return false
	}
	system := flattenAnthropicSystem(req.System)
	if strings.Contains(system, "You are Claude Code, Anthropic's official CLI for Claude.") {
		return true
	}
	for _, msg := range req.Messages {
		if strings.Contains(extractAnthropicMessageText(msg), "The following skills are available for use with the Skill tool:") {
			return true
		}
	}
	return false
}

func compactClaudeCodeRequest(req *AnthropicMessagesRequest) []map[string]interface{} {
	system := flattenAnthropicSystem(req.System)
	messageTexts := make([]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if text := strings.TrimSpace(extractAnthropicMessageText(msg)); text != "" {
			messageTexts = append(messageTexts, text)
		}
	}
	joinedMessages := strings.Join(messageTexts, "\n\n")

	parts := make([]string, 0, 5)
	if cwd := firstRegexpGroup(claudeCodeCWDPattern, system+"\n"+joinedMessages); cwd != "" {
		parts = append(parts, "Working directory: "+cwd)
	}
	if opened := firstRegexpGroup(claudeCodeOpenedFilePattern, joinedMessages); opened != "" {
		parts = append(parts, "Opened file: "+opened)
	}
	if instruction := extractClaudeCodeUserInstructions(system + "\n" + joinedMessages); instruction != "" {
		parts = append(parts, "Relevant user/project instructions:\n"+instruction)
	}
	if len(parts) == 0 {
		parts = append(parts, "Claude Code request context compacted by proxy.")
	}

	messages := []map[string]interface{}{{
		"role":    "system",
		"content": strings.Join(parts, "\n\n"),
	}}
	messages = append(messages, compactClaudeCodeConversation(req.Messages)...)
	return messages
}

func compactClaudeCodeConversation(input []AnthropicMessage) []map[string]interface{} {
	messages := make([]map[string]interface{}, 0, len(input))
	for _, msg := range input {
		if text, ok := decodeMaybeString(msg.Content); ok {
			if text = stripClaudeCodeReminders(text); strings.TrimSpace(text) != "" {
				messages = append(messages, map[string]interface{}{"role": msg.Role, "content": text})
			}
			continue
		}

		var blocks []AnthropicBlock
		if len(msg.Content) == 0 || json.Unmarshal(msg.Content, &blocks) != nil {
			continue
		}
		var textBuf strings.Builder
		toolCalls := make([]map[string]interface{}, 0)
		flushText := func() {
			text := strings.TrimSpace(stripClaudeCodeReminders(textBuf.String()))
			if text != "" {
				messages = append(messages, map[string]interface{}{"role": msg.Role, "content": text})
			}
			textBuf.Reset()
		}

		for _, block := range blocks {
			switch block.Type {
			case "text", "thinking":
				text := block.Text
				if text == "" {
					text = block.Thinking
				}
				if strings.TrimSpace(text) != "" {
					if textBuf.Len() > 0 {
						textBuf.WriteString("\n")
					}
					textBuf.WriteString(text)
				}
			case "tool_use":
				args := string(block.Input)
				if args == "" || args == "null" {
					args = "{}"
				}
				toolCalls = append(toolCalls, map[string]interface{}{
					"id": block.ID, "type": "function",
					"function": map[string]interface{}{"name": block.Name, "arguments": args},
				})
			case "tool_result":
				flushText()
				resultText := flattenToolResultContent(block.Content)
				if block.IsError && resultText != "" {
					resultText = "[error] " + resultText
				}
				messages = append(messages, map[string]interface{}{
					"role": "tool", "tool_call_id": block.ToolUseID, "content": resultText,
				})
			}
		}
		flushText()
		if len(toolCalls) > 0 {
			messages = append(messages, map[string]interface{}{"role": "assistant", "content": "", "tool_calls": toolCalls})
		}
	}
	return messages
}

func stripClaudeCodeReminders(text string) string {
	blocks := splitSystemReminderBlocks(text)
	kept := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" || strings.HasPrefix(block, "<system-reminder>") {
			continue
		}
		kept = append(kept, block)
	}
	return strings.Join(kept, "\n\n")
}

func filterClaudeCodeTools(input []AnthropicTool) []map[string]interface{} {
	allowed := map[string]bool{
		"Bash": true, "Read": true, "Write": true, "Edit": true, "MultiEdit": true,
		"Glob": true, "Grep": true, "LS": true,
	}
	tools := make([]map[string]interface{}, 0, len(allowed))
	for _, t := range input {
		if !allowed[t.Name] {
			continue
		}
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": t.Name, "description": t.Description, "parameters": t.InputSchema,
			},
		})
	}
	return tools
}

func extractAnthropicMessageText(msg AnthropicMessage) string {
	if text, ok := decodeMaybeString(msg.Content); ok {
		return text
	}
	var blocks []AnthropicBlock
	if len(msg.Content) > 0 && json.Unmarshal(msg.Content, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					parts = append(parts, block.Text)
				}
			case "tool_result":
				if text := flattenToolResultContent(block.Content); strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return strings.TrimSpace(string(msg.Content))
}

func extractClaudeCodeUserTask(req *AnthropicMessagesRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		text := strings.TrimSpace(extractAnthropicMessageText(req.Messages[i]))
		if text == "" {
			continue
		}
		blocks := splitSystemReminderBlocks(text)
		for j := len(blocks) - 1; j >= 0; j-- {
			candidate := strings.TrimSpace(blocks[j])
			if candidate == "" || strings.HasPrefix(candidate, "<system-reminder>") {
				continue
			}
			return candidate
		}
	}
	return ""
}

func splitSystemReminderBlocks(text string) []string {
	parts := make([]string, 0)
	for {
		start := strings.Index(text, "<system-reminder>")
		if start < 0 {
			parts = append(parts, text)
			return parts
		}
		if start > 0 {
			parts = append(parts, text[:start])
		}
		end := strings.Index(text[start:], "</system-reminder>")
		if end < 0 {
			parts = append(parts, text[start:])
			return parts
		}
		end += start + len("</system-reminder>")
		parts = append(parts, text[start:end])
		text = text[end:]
	}
}

func extractClaudeCodeUserInstructions(text string) string {
	idx := strings.Index(text, "Contents of ")
	if idx < 0 {
		return ""
	}
	section := text[idx:]
	if end := strings.Index(section, "IMPORTANT: this context may or may not be relevant"); end >= 0 {
		section = section[:end]
	}
	lines := strings.Split(section, "\n")
	kept := make([]string, 0, 8)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Contents of ") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "Always respond") || strings.Contains(trimmed, "Chinese") || strings.Contains(trimmed, "中文") {
			kept = append(kept, trimmed)
		} else if strings.HasPrefix(trimmed, "Project override:") {
			kept = append(kept, trimmed)
		}
		if len(kept) >= 3 {
			break
		}
	}
	return strings.Join(kept, "\n")
}

func firstRegexpGroup(pattern *regexp.Regexp, text string) string {
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
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

func decodeToolChoice(raw json.RawMessage) interface{} {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	if m, ok := value.(map[string]interface{}); ok {
		kind, _ := m["type"].(string)
		if kind == "auto" || kind == "required" || kind == "none" || kind == "any" {
			return kind
		}
		if kind == "tool" {
			if name, _ := m["name"].(string); name != "" {
				return map[string]interface{}{"type": "tool", "name": name}
			}
		}
	}
	return value
}

// describeAnthropicImage renders an image source (base64 or url) as a compact
// text marker. The caller inlines the result into the prompt.
func describeAnthropicImage(src *AnthropicImageSource) string {
	if src == nil {
		return "[image]"
	}
	return describeImage(src.MediaType, src.Data, src.URL)
}
