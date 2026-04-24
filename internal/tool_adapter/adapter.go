// Package tool_adapter provides tool calling adaptation for OpenAI-compatible API.
package tool_adapter

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// ToolCall represents a single tool call extracted from model output.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolCallPlan represents parsed result from model output.
type ToolCallPlan struct {
	Action    string     `json:"action"` // "tool_call" or "final"
	ToolCalls []ToolCall `json:"tool_calls"`
	Content   string     `json:"content"`
}

var (
	streamActionPattern  = regexp.MustCompile(`"action"\s*:\s*"(final|tool_calls?)"`)
	streamContentPattern = regexp.MustCompile(`"content"\s*:\s*"`)
)

// StructuredResponseStreamParser incrementally parses the JSON-only response
// format used by BuildToolPrompt. It can stream "final" content before the
// whole JSON object closes, while still buffering tool_call plans until the
// full object is available.
type StructuredResponseStreamParser struct {
	raw string

	jsonStart int
	action    string

	contentStart int
	contentScan  int
	contentEmit  int

	escaped       bool
	unicodeDigits int

	done         bool
	emittedCalls bool
}

// NewStructuredResponseStreamParser creates a parser for tool-adapted stream output.
func NewStructuredResponseStreamParser() *StructuredResponseStreamParser {
	return &StructuredResponseStreamParser{
		jsonStart:    -1,
		contentStart: -1,
	}
}

// Feed consumes a streamed text delta and returns any user-visible text plus
// any fully-parsed tool calls detected so far.
func (p *StructuredResponseStreamParser) Feed(delta string) (string, []ToolCall) {
	if delta == "" {
		return "", nil
	}
	p.raw += delta
	if p.done {
		return "", nil
	}

	if p.jsonStart < 0 {
		if idx := strings.IndexByte(p.raw, '{'); idx >= 0 {
			p.jsonStart = idx
		}
	}
	if p.jsonStart < 0 {
		return "", nil
	}

	p.detectAction()

	if p.action == "final" {
		p.detectContentStart()
		if p.contentStart >= 0 {
			return p.decodeAvailableFinalContent(), nil
		}
	}

	if (p.action == "tool_call" || p.action == "tool_calls") && !p.emittedCalls {
		if jsonText, _, ok := extractJSONObject(p.raw, p.jsonStart); ok {
			if plan, ok := decodeStructuredToolPlan(jsonText); ok && plan.Action == "tool_call" && len(plan.ToolCalls) > 0 {
				p.done = true
				p.emittedCalls = true
				return "", plan.ToolCalls
			}
		}
	}

	return "", nil
}

func (p *StructuredResponseStreamParser) detectAction() {
	if p.action != "" || p.jsonStart < 0 {
		return
	}
	if matches := streamActionPattern.FindStringSubmatch(p.raw[p.jsonStart:]); len(matches) == 2 {
		p.action = matches[1]
	}
}

func (p *StructuredResponseStreamParser) detectContentStart() {
	if p.action != "final" || p.contentStart >= 0 || p.jsonStart < 0 {
		return
	}
	if loc := streamContentPattern.FindStringIndex(p.raw[p.jsonStart:]); loc != nil {
		p.contentStart = p.jsonStart + loc[1]
		p.contentScan = p.contentStart
		p.contentEmit = p.contentStart
	}
}

func (p *StructuredResponseStreamParser) decodeAvailableFinalContent() string {
	if p.contentStart < 0 || p.done {
		return ""
	}

	safeEnd := p.contentEmit
	i := p.contentScan
	for i < len(p.raw) {
		ch := p.raw[i]

		if p.unicodeDigits > 0 {
			p.unicodeDigits--
			i++
			if p.unicodeDigits == 0 {
				safeEnd = i
			}
			continue
		}

		if p.escaped {
			p.escaped = false
			if ch == 'u' {
				p.unicodeDigits = 4
				i++
				continue
			}
			i++
			safeEnd = i
			continue
		}

		switch ch {
		case '\\':
			p.escaped = true
			i++
		case '"':
			p.contentScan = i + 1
			p.done = true
			return p.decodeJSONStringChunk(safeEnd)
		default:
			i++
			safeEnd = i
		}
	}

	p.contentScan = i
	return p.decodeJSONStringChunk(safeEnd)
}

func (p *StructuredResponseStreamParser) decodeJSONStringChunk(safeEnd int) string {
	if safeEnd <= p.contentEmit {
		return ""
	}
	rawChunk := p.raw[p.contentEmit:safeEnd]
	p.contentEmit = safeEnd

	var decoded string
	if err := json.Unmarshal([]byte(`"`+rawChunk+`"`), &decoded); err != nil {
		return ""
	}
	return decoded
}

// ToolInstruction is the tool-calling format instruction injected into prompts.
const toolCallInstruction = `
Return exactly one JSON object and nothing else.

Tool call:
{"action":"tool_call","tool_calls":[{"name":"TOOL_NAME","arguments":{"param":"value"}}]}

Final answer:
{"action":"final","content":"your answer here"}

Tools:
%s

%s

Rules: JSON only. No markdown. Arguments must match each tool schema.
`

// summarizeTool summarizes a single tool definition compactly.
func summarizeTool(tool map[string]interface{}) string {
	funcDef, ok := tool["function"].(map[string]interface{})
	if !ok {
		return "- unknown"
	}

	name, _ := funcDef["name"].(string)
	if name == "" {
		name = "unknown"
	}

	description, _ := funcDef["description"].(string)
	params, _ := funcDef["parameters"].(map[string]interface{})
	lines := []string{fmt.Sprintf("- %s", name)}
	if description != "" {
		lines[0] += ": " + description
	}
	if len(params) > 0 {
		if schema, err := json.MarshalIndent(params, "", "  "); err == nil {
			lines = append(lines, "  parameters schema:")
			for _, line := range strings.Split(string(schema), "\n") {
				lines = append(lines, "  "+line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// BuildToolPrompt builds messages with tool-calling instructions.
func BuildToolPrompt(
	tools []map[string]interface{},
	messages []map[string]interface{},
	toolChoice interface{},
) []map[string]string {
	// Build tool list
	toolList := "(none)"
	if len(tools) > 0 {
		toolParts := []string{}
		for _, t := range tools {
			toolParts = append(toolParts, summarizeTool(t))
		}
		toolList = strings.Join(toolParts, "\n")
	}

	// Format instruction
	instruction := fmt.Sprintf(toolCallInstruction, toolList, buildToolChoiceInstruction(toolChoice))

	result := []map[string]string{}

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content := extractTextParts(msg["content"])

		if role == "system" {
			// Append tool-calling instructions to system prompt
			result = append(result, map[string]string{
				"role":    "system",
				"content": content + instruction,
			})
		} else if role == "tool" {
			// Convert tool result to user message
			name, _ := msg["name"].(string)
			toolCallID, _ := msg["tool_call_id"].(string)
			truncated := content
			if len(truncated) > 2000 {
				truncated = truncated[:2000]
			}
			result = append(result, map[string]string{
				"role":    "user",
				"content": fmt.Sprintf("[Tool result for %s (id=%s)]: %s", name, toolCallID, truncated),
			})
		} else if role == "assistant" {
			// Check for tool_calls
			if toolCalls, ok := msg["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
				// Convert assistant tool_calls to text
				calls := []map[string]interface{}{}
				for _, tc := range toolCalls {
					if tcMap, ok := tc.(map[string]interface{}); ok {
						funcData, _ := tcMap["function"].(map[string]interface{})
						name, _ := funcData["name"].(string)
						argsStr, _ := funcData["arguments"].(string)
						var args interface{}
						if argsStr != "" {
							json.Unmarshal([]byte(argsStr), &args)
						} else {
							args = map[string]interface{}{}
						}
						calls = append(calls, map[string]interface{}{
							"name":      name,
							"arguments": args,
						})
					}
				}
				tcText, _ := json.Marshal(map[string]interface{}{
					"action":     "tool_call",
					"tool_calls": calls,
				})
				result = append(result, map[string]string{
					"role":    "assistant",
					"content": string(tcText),
				})
			} else {
				result = append(result, map[string]string{
					"role":    role,
					"content": content,
				})
			}
		} else {
			result = append(result, map[string]string{
				"role":    role,
				"content": content,
			})
		}
	}

	if len(tools) > 0 && !hasSystemMessage(result) {
		if len(result) == 0 {
			result = append(result, map[string]string{
				"role":    "user",
				"content": instruction,
			})
		} else {
			result[0]["content"] = instruction + "\n\n" + result[0]["content"]
		}
	}

	return result
}

func hasSystemMessage(messages []map[string]string) bool {
	for _, msg := range messages {
		if msg["role"] == "system" {
			return true
		}
	}
	return false
}

func buildToolChoiceInstruction(toolChoice interface{}) string {
	mode, forceName := resolveToolChoice(toolChoice)
	switch mode {
	case "required":
		if forceName != "" {
			return fmt.Sprintf("Tool choice: you MUST call the tool %q and no other tool.", forceName)
		}
		return "Tool choice: you MUST emit at least one tool_call response."
	case "none":
		return "Tool choice: do not call tools; respond with action=final."
	default:
		return "Tool choice: if a listed tool is relevant, prefer calling it over guessing."
	}
}

func resolveToolChoice(toolChoice interface{}) (mode string, forceName string) {
	switch v := toolChoice.(type) {
	case string:
		switch v {
		case "required", "any":
			return "required", ""
		case "none":
			return "none", ""
		default:
			return "auto", ""
		}
	case map[string]interface{}:
		typeName, _ := v["type"].(string)
		switch typeName {
		case "required", "any":
			return "required", ""
		case "none":
			return "none", ""
		case "function":
			if fn, ok := v["function"].(map[string]interface{}); ok {
				if name, _ := fn["name"].(string); name != "" {
					return "required", name
				}
			}
			return "required", ""
		case "tool":
			if name, _ := v["name"].(string); name != "" {
				return "required", name
			}
			return "required", ""
		default:
			return "auto", ""
		}
	default:
		return "auto", ""
	}
}

// extractTextParts extracts text from OpenAI message content.
func extractTextParts(content interface{}) string {
	if content == nil {
		return ""
	}

	if str, ok := content.(string); ok {
		return str
	}

	if arr, ok := content.([]interface{}); ok {
		parts := []string{}
		for _, p := range arr {
			if pMap, ok := p.(map[string]interface{}); ok {
				if pMap["type"] == "text" {
					if text, ok := pMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}

	return fmt.Sprintf("%v", content)
}

// normalizeArguments recursively normalizes tool arguments.
func normalizeArguments(raw interface{}) interface{} {
	if raw == nil {
		return map[string]interface{}{}
	}

	if str, ok := raw.(string); ok {
		trimmed := strings.TrimSpace(str)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			var parsed interface{}
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return normalizeArguments(parsed)
			}
		}
		return raw
	}

	if arr, ok := raw.([]interface{}); ok {
		result := []interface{}{}
		for _, item := range arr {
			result = append(result, normalizeArguments(item))
		}
		return result
	}

	if m, ok := raw.(map[string]interface{}); ok {
		result := map[string]interface{}{}
		for k, v := range m {
			result[k] = normalizeArguments(v)
		}
		return result
	}

	return raw
}

// ParseToolResponse parses model output to extract tool calls or final answer.
func ParseToolResponse(output string) ToolCallPlan {
	if plan, prefixLen, ok := parseStructuredToolResponse(output); ok {
		log.Printf(
			"[Tool parse] structured response parsed: action=%s tool_calls=%d content_len=%d prefix_len=%d",
			plan.Action, len(plan.ToolCalls), len(plan.Content), prefixLen,
		)
		return plan
	}

	// Fallback: try legacy "<tool name {json}" format without regex lookahead.
	if calls := parseTaggedToolCalls(output); len(calls) > 0 {
		log.Printf("[Tool parse] legacy tagged tool calls parsed: count=%d", len(calls))
		return ToolCallPlan{
			Action:    "tool_call",
			ToolCalls: calls,
			Content:   "",
		}
	}

	// Fallback: return as plain content
	log.Printf("[Tool parse] no structured response found; returning plain content (len=%d)", len(output))
	return ToolCallPlan{
		Action:    "final",
		ToolCalls: []ToolCall{},
		Content:   output,
	}
}

func parseStructuredToolResponse(output string) (ToolCallPlan, int, bool) {
	for start := 0; start < len(output); {
		idx := strings.IndexByte(output[start:], '{')
		if idx == -1 {
			break
		}
		jsonStart := start + idx
		jsonText, next, ok := extractJSONObject(output, jsonStart)
		if !ok {
			start = jsonStart + 1
			continue
		}
		if plan, ok := decodeStructuredToolPlan(jsonText); ok {
			return plan, jsonStart, true
		}
		start = next
	}
	return ToolCallPlan{}, 0, false
}

func decodeStructuredToolPlan(jsonText string) (ToolCallPlan, bool) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonText), &parsed); err != nil {
		return ToolCallPlan{}, false
	}

	action, _ := parsed["action"].(string)
	switch action {
	case "final":
		content, _ := parsed["content"].(string)
		return ToolCallPlan{
			Action:    "final",
			ToolCalls: []ToolCall{},
			Content:   content,
		}, true
	case "tool_call", "tool_calls":
		tcArr, ok := parsed["tool_calls"].([]interface{})
		if !ok {
			return ToolCallPlan{}, false
		}
		calls := []ToolCall{}
		for i, tc := range tcArr {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := tcMap["name"].(string)
			if name == "" {
				continue
			}
			args := normalizeArguments(tcMap["arguments"])
			argsJSON, _ := json.Marshal(args)
			calls = append(calls, ToolCall{
				ID:        fmt.Sprintf("call_%d_%d", time.Now().Unix(), i),
				Name:      name,
				Arguments: string(argsJSON),
			})
		}
		if len(calls) == 0 {
			return ToolCallPlan{}, false
		}
		return ToolCallPlan{
			Action:    "tool_call",
			ToolCalls: calls,
			Content:   "",
		}, true
	default:
		return ToolCallPlan{}, false
	}
}

func parseTaggedToolCalls(output string) []ToolCall {
	calls := []ToolCall{}
	for pos := 0; pos < len(output); {
		idx := strings.Index(output[pos:], "<tool")
		if idx == -1 {
			break
		}

		cursor := pos + idx + len("<tool")
		cursor = skipToolWhitespace(output, cursor)
		nameStart := cursor
		for cursor < len(output) && isToolNameChar(output[cursor]) {
			cursor++
		}
		if cursor == nameStart {
			pos = pos + idx + len("<tool")
			continue
		}

		name := output[nameStart:cursor]
		cursor = skipToolWhitespace(output, cursor)
		if cursor >= len(output) || output[cursor] != '{' {
			pos = cursor
			continue
		}

		argsText, next, ok := extractJSONObject(output, cursor)
		if !ok {
			pos = cursor + 1
			continue
		}

		var args interface{}
		if err := json.Unmarshal([]byte(argsText), &args); err == nil {
			args = normalizeArguments(args)
		} else {
			args = map[string]interface{}{}
		}
		argsJSON, _ := json.Marshal(args)
		calls = append(calls, ToolCall{
			ID:        fmt.Sprintf("call_%d_%d", time.Now().Unix(), len(calls)),
			Name:      name,
			Arguments: string(argsJSON),
		})

		pos = next
	}
	return calls
}

func skipToolWhitespace(s string, pos int) int {
	for pos < len(s) {
		switch s[pos] {
		case ' ', '\n', '\r', '\t':
			pos++
		default:
			return pos
		}
	}
	return pos
}

func isToolNameChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_' || ch == '.' || ch == '-'
}

func extractJSONObject(s string, start int) (string, int, bool) {
	if start >= len(s) || s[start] != '{' {
		return "", start, false
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], i + 1, true
			}
		}
	}

	return "", start, false
}

// HasToolUsage checks if the request involves tool calling.
func HasToolUsage(messages []map[string]interface{}, tools []map[string]interface{}) bool {
	if len(tools) > 0 {
		return true
	}

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if role == "tool" {
			return true
		}
		if role == "assistant" {
			if _, ok := msg["tool_calls"]; ok {
				return true
			}
		}
	}

	return false
}

// truncate truncates a string to max length.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
