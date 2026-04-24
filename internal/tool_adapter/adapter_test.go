package tool_adapter

import (
	"strings"
	"testing"
)

func TestStructuredResponseStreamParserStreamsFinalContent(t *testing.T) {
	parser := NewStructuredResponseStreamParser()

	text, calls := parser.Feed(`{"action":"final","content":"hel`)
	if text != "hel" {
		t.Fatalf("first chunk text = %q, want %q", text, "hel")
	}
	if len(calls) != 0 {
		t.Fatalf("first chunk calls = %d, want 0", len(calls))
	}

	text, calls = parser.Feed(`lo\nwo`)
	if text != "lo\nwo" {
		t.Fatalf("second chunk text = %q, want %q", text, "lo\nwo")
	}
	if len(calls) != 0 {
		t.Fatalf("second chunk calls = %d, want 0", len(calls))
	}

	text, calls = parser.Feed(`rld"}`)
	if text != "rld" {
		t.Fatalf("third chunk text = %q, want %q", text, "rld")
	}
	if len(calls) != 0 {
		t.Fatalf("third chunk calls = %d, want 0", len(calls))
	}
}

func TestStructuredResponseStreamParserWaitsForFullToolCallPlan(t *testing.T) {
	parser := NewStructuredResponseStreamParser()

	text, calls := parser.Feed(`{"action":"tool_call","tool_calls":[{"name":"search"`)
	if text != "" {
		t.Fatalf("partial tool text = %q, want empty", text)
	}
	if len(calls) != 0 {
		t.Fatalf("partial tool calls = %d, want 0", len(calls))
	}

	text, calls = parser.Feed(`,"arguments":{"q":"123"}}]}`)
	if text != "" {
		t.Fatalf("final tool text = %q, want empty", text)
	}
	if len(calls) != 1 {
		t.Fatalf("final tool calls = %d, want 1", len(calls))
	}
	if calls[0].Name != "search" {
		t.Fatalf("tool name = %q, want %q", calls[0].Name, "search")
	}
	if calls[0].Arguments != `{"q":"123"}` {
		t.Fatalf("tool arguments = %q, want %q", calls[0].Arguments, `{"q":"123"}`)
	}
}

func TestBuildToolPromptPreservesFullSchemaDetails(t *testing.T) {
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "search",
				"description": "Search weather data with enum and nested object requirements.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"unit": map[string]interface{}{
							"type": "string",
							"enum": []interface{}{"celsius", "fahrenheit"},
						},
						"filters": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"city": map[string]interface{}{"type": "string"},
							},
						},
					},
					"required": []interface{}{"unit"},
				},
			},
		},
	}
	messages := []map[string]interface{}{
		{"role": "system", "content": "System prompt"},
		{"role": "user", "content": "hello"},
	}

	got := BuildToolPrompt(tools, messages, nil)
	if len(got) != 2 {
		t.Fatalf("BuildToolPrompt returned %d messages, want 2", len(got))
	}
	system := got[0]["content"]
	for _, want := range []string{
		`"enum": [`,
		`"filters": {`,
		`"required": [`,
		`"unit"`,
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, system)
		}
	}
}

func TestBuildToolPromptRespectsRequiredToolChoice(t *testing.T) {
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "search",
				"description": "Search things.",
				"parameters":  map[string]interface{}{"type": "object"},
			},
		},
	}
	messages := []map[string]interface{}{
		{"role": "system", "content": "System prompt"},
		{"role": "user", "content": "hello"},
	}

	got := BuildToolPrompt(tools, messages, map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "search",
		},
	})
	system := got[0]["content"]
	if !strings.Contains(system, `you MUST call the tool "search"`) {
		t.Fatalf("system prompt missing forced tool choice instruction:\n%s", system)
	}
}

func TestBuildToolPromptInjectsToolsWithoutSystemMessage(t *testing.T) {
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "search",
				"description": "Search things.",
				"parameters":  map[string]interface{}{"type": "object"},
			},
		},
	}
	messages := []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}

	got := BuildToolPrompt(tools, messages, nil)
	if len(got) != 1 {
		t.Fatalf("BuildToolPrompt returned %d messages, want 1", len(got))
	}
	content := got[0]["content"]
	for _, want := range []string{"Return exactly one JSON object", "search", "hello"} {
		if !strings.Contains(content, want) {
			t.Fatalf("user content missing %q:\n%s", want, content)
		}
	}
}

func TestStructuredResponseStreamParserAcceptsPluralToolCallsAction(t *testing.T) {
	parser := NewStructuredResponseStreamParser()
	text, calls := parser.Feed(`{"action":"tool_calls","tool_calls":[{"name":"Read"`)
	if text != "" || len(calls) != 0 {
		t.Fatalf("partial plural action text=%q calls=%d, want empty/0", text, len(calls))
	}
	text, calls = parser.Feed(`,"arguments":{"file_path":"README.md"}}]}`)
	if text != "" {
		t.Fatalf("plural action text = %q, want empty", text)
	}
	if len(calls) != 1 || calls[0].Name != "Read" {
		t.Fatalf("plural action calls = %#v", calls)
	}
}

func TestParseToolResponseAcceptsPluralToolCallsAction(t *testing.T) {
	plan := ParseToolResponse(`{"action":"tool_calls","tool_calls":[{"name":"Bash","arguments":{"command":"ls"}}]}`)
	if plan.Action != "tool_call" || len(plan.ToolCalls) != 1 || plan.ToolCalls[0].Name != "Bash" {
		t.Fatalf("plan = %#v", plan)
	}
}
