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

// ToolInstruction is the tool-calling format instruction injected into prompts.
const toolCallInstruction = `
[TOOL CALLING FORMAT]
You have access to tools. When you want to use a tool, you MUST output ONLY a single JSON object (no other text before or after):
{"action":"tool_call","tool_calls":[{"name":"TOOL_NAME","arguments":{"param":"value"}}]}

When you want to give a final text answer to the user, output ONLY:
{"action":"final","content":"your answer here"}

Available tools:
%s

CRITICAL RULES:
- Your ENTIRE response must be exactly ONE JSON object. Nothing else.
- Do NOT wrap JSON in markdown code blocks.
- Do NOT add any text before or after the JSON.
- Arguments must match each tool's parameter schema.
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
	if len(description) > 120 {
		description = description[:120]
	}

	params, _ := funcDef["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	required, _ := params["required"].([]interface{})

	if len(props) > 0 {
		paramParts := []string{}
		for pname, pdef := range props {
			ptype := "any"
			if pdefMap, ok := pdef.(map[string]interface{}); ok {
				if t, ok := pdefMap["type"].(string); ok {
					ptype = t
				}
			}
			marker := ""
			for _, r := range required {
				if r == pname {
					marker = "*"
					break
				}
			}
			paramParts = append(paramParts, fmt.Sprintf("%s%s:%s", pname, marker, ptype))
		}
		paramsStr := strings.Join(paramParts, ", ")
		if description != "" {
			return fmt.Sprintf("- %s(%s): %s", name, paramsStr, description)
		}
		return fmt.Sprintf("- %s(%s)", name, paramsStr)
	}

	if description != "" {
		return fmt.Sprintf("- %s: %s", name, description)
	}
	return fmt.Sprintf("- %s", name)
}

// BuildToolPrompt builds messages with tool-calling instructions.
func BuildToolPrompt(
	tools []map[string]interface{},
	messages []map[string]interface{},
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
	instruction := fmt.Sprintf(toolCallInstruction, toolList)

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
					"action":    "tool_call",
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

	return result
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
	// Log first 500 chars for debugging
	log.Printf("[Tool parse] raw output (first 500): %s", truncate(output, 500))

	// Try to find JSON object in the output
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start != -1 && end > start {
		jsonText := output[start : end+1]
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(jsonText), &parsed); err == nil {
			action, _ := parsed["action"].(string)

			// Final answer
			if action == "final" {
				if content, ok := parsed["content"].(string); ok {
					return ToolCallPlan{
						Action:    "final",
						ToolCalls: []ToolCall{},
						Content:   content,
					}
				}
			}

			// Tool calls
			if action == "tool_call" {
				if tcArr, ok := parsed["tool_calls"].([]interface{}); ok {
					calls := []ToolCall{}
					for i, tc := range tcArr {
						if tcMap, ok := tc.(map[string]interface{}); ok {
							name, _ := tcMap["name"].(string)
							if name != "" {
								args := normalizeArguments(tcMap["arguments"])
								argsJSON, _ := json.Marshal(args)
								calls = append(calls, ToolCall{
									ID:        fmt.Sprintf("call_%d_%d", time.Now().Unix(), i),
									Name:      name,
									Arguments: string(argsJSON),
								})
							}
						}
					}
					if len(calls) > 0 {
						return ToolCallPlan{
							Action:    "tool_call",
							ToolCalls: calls,
							Content:   "",
						}
					}
				}
			}
		}
	}

	// Fallback: try <tool> tag format
	re := regexp.MustCompile(`<tool\s+([\w.\-]+)\s+(\{[\s\S]*?\})(?=\s*(?:<tool|$))`)
	matches := re.FindAllStringSubmatch(output, -1)
	if len(matches) > 0 {
		calls := []ToolCall{}
		for i, m := range matches {
			name := m[1]
			var args interface{}
			if err := json.Unmarshal([]byte(m[2]), &args); err == nil {
				args = normalizeArguments(args)
			} else {
				args = map[string]interface{}{}
			}
			argsJSON, _ := json.Marshal(args)
			calls = append(calls, ToolCall{
				ID:        fmt.Sprintf("call_%d_%d", time.Now().Unix(), i),
				Name:      name,
				Arguments: string(argsJSON),
			})
		}
		if len(calls) > 0 {
			return ToolCallPlan{
				Action:    "tool_call",
				ToolCalls: calls,
				Content:   "",
			}
		}
	}

	// Fallback: return as plain content
	log.Printf("[Tool parse] parsing failed, returning as plain content")
	return ToolCallPlan{
		Action:    "final",
		ToolCalls: []ToolCall{},
		Content:   output,
	}
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