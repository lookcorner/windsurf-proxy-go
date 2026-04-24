package protobuf

import (
	"strings"
	"testing"
)

func TestBuildStartCascadeRequestUsesCurrentMetadataAndTrajectory(t *testing.T) {
	req := BuildStartCascadeRequest("api-key", "1.2.3")

	fields := ParseFields(req)
	if got := GetVarintField(fields, 4); got != 1 {
		t.Fatalf("source = %d, want 1", got)
	}
	if got := GetVarintField(fields, 5); got != 1 {
		t.Fatalf("trajectory_type = %d, want 1", got)
	}

	metaFields := ParseFields(GetMessageField(fields, 1))
	if got := GetStringField(metaFields, 1); got != "windsurf" {
		t.Fatalf("ide_name = %q, want %q", got, "windsurf")
	}
	if got := GetStringField(metaFields, 2); got != "1.2.3" {
		t.Fatalf("extension_version = %q, want %q", got, "1.2.3")
	}
	if got := GetStringField(metaFields, 3); got != "api-key" {
		t.Fatalf("api_key = %q, want %q", got, "api-key")
	}
	if got := GetStringField(metaFields, 4); got != "en" {
		t.Fatalf("locale = %q, want %q", got, "en")
	}
	if got := GetStringField(metaFields, 5); got == "" {
		t.Fatalf("os field missing")
	}
	if got := GetStringField(metaFields, 8); got == "" {
		t.Fatalf("hardware field missing")
	}
	if got := GetVarintField(metaFields, 9); got == 0 {
		t.Fatalf("request_id field missing")
	}
	if got := GetStringField(metaFields, 10); got == "" {
		t.Fatalf("session_id field missing")
	}
	if got := GetStringField(metaFields, 12); got != "windsurf" {
		t.Fatalf("extension_name = %q, want %q", got, "windsurf")
	}
}

func TestEncodeChatMessageUsesAssistantActionField(t *testing.T) {
	assistant := ParseFields(EncodeChatMessage("hello", ChatMessageSourceAssistant, "conv-1"))
	if HasField(assistant, 5) {
		t.Fatalf("assistant message unexpectedly used intent field 5")
	}
	actionFields := ParseFields(GetMessageField(assistant, 6))
	genericFields := ParseFields(GetMessageField(actionFields, 1))
	if got := GetStringField(genericFields, 1); got != "hello" {
		t.Fatalf("assistant action text = %q, want %q", got, "hello")
	}

	user := ParseFields(EncodeChatMessage("hello", ChatMessageSourceUser, "conv-1"))
	intentFields := ParseFields(GetMessageField(user, 5))
	genericFields = ParseFields(GetMessageField(intentFields, 1))
	if got := GetStringField(genericFields, 1); got != "hello" {
		t.Fatalf("user intent text = %q, want %q", got, "hello")
	}
}

func TestBuildChatRequestConcatenatesSystemAndRewritesToolMessages(t *testing.T) {
	req := BuildChatRequest(
		"api-key",
		[]map[string]string{
			{"role": "system", "content": "sys-1"},
			{"role": "system", "content": "sys-2"},
			{"role": "assistant", "content": "assistant text"},
			{"role": "tool", "content": "tool output", "tool_call_id": "call-1"},
		},
		123,
		"model-name",
		"1.0.0",
	)

	fields := ParseFields(req)
	if got := GetStringField(fields, 3); got != "sys-1\nsys-2" {
		t.Fatalf("system_prompt_override = %q, want %q", got, "sys-1\nsys-2")
	}

	var chatMessages [][]byte
	for _, f := range fields {
		if f.FieldNumber == 2 && f.WireType == 2 {
			if msg, ok := f.Value.([]byte); ok {
				chatMessages = append(chatMessages, msg)
			}
		}
	}
	if len(chatMessages) != 2 {
		t.Fatalf("chat message count = %d, want 2", len(chatMessages))
	}

	assistantFields := ParseFields(chatMessages[0])
	if got := GetVarintField(assistantFields, 2); got != ChatMessageSourceAssistant {
		t.Fatalf("assistant source = %d, want %d", got, ChatMessageSourceAssistant)
	}
	actionFields := ParseFields(GetMessageField(assistantFields, 6))
	genericFields := ParseFields(GetMessageField(actionFields, 1))
	if got := GetStringField(genericFields, 1); got != "assistant text" {
		t.Fatalf("assistant text = %q, want %q", got, "assistant text")
	}

	toolFields := ParseFields(chatMessages[1])
	if got := GetVarintField(toolFields, 2); got != ChatMessageSourceUser {
		t.Fatalf("rewritten tool source = %d, want %d", got, ChatMessageSourceUser)
	}
	intentFields := ParseFields(GetMessageField(toolFields, 5))
	genericFields = ParseFields(GetMessageField(intentFields, 1))
	if got := GetStringField(genericFields, 1); got != "[tool result for call-1]: tool output" {
		t.Fatalf("rewritten tool text = %q", got)
	}
}

func TestBuildSendCascadeMessageRequestUsesCascadeConfigOverrides(t *testing.T) {
	req := BuildSendCascadeMessageRequest(
		"cascade-1",
		"user text",
		123,
		"claude-opus-4-7-low",
		"api-key",
		"1.0.0",
		"Be concise.",
	)

	fields := ParseFields(req)
	if got := GetStringField(fields, 1); got != "cascade-1" {
		t.Fatalf("cascade_id = %q, want %q", got, "cascade-1")
	}

	itemFields := ParseFields(GetMessageField(fields, 2))
	if got := GetStringField(itemFields, 1); got != "user text" {
		t.Fatalf("item text = %q, want %q", got, "user text")
	}

	cfgFields := ParseFields(GetMessageField(fields, 5))
	plannerFields := ParseFields(GetMessageField(cfgFields, 1))
	if got := GetStringField(plannerFields, 35); got != "claude-opus-4-7-low" {
		t.Fatalf("requested_model_uid = %q", got)
	}
	if got := GetStringField(plannerFields, 34); got != "claude-opus-4-7-low" {
		t.Fatalf("plan_model_uid = %q", got)
	}
	if got := GetVarintField(plannerFields, 6); got != cascadeMaxOutputTokens {
		t.Fatalf("max_output_tokens = %d, want %d", got, cascadeMaxOutputTokens)
	}
	if got := GetVarintField(plannerFields, 1); got != 123 {
		t.Fatalf("plan_model_deprecated = %d, want 123", got)
	}
	reqModelFields := ParseFields(GetMessageField(plannerFields, 15))
	if got := GetVarintField(reqModelFields, 1); got != 123 {
		t.Fatalf("requested_model_deprecated = %d, want 123", got)
	}

	convFields := ParseFields(GetMessageField(plannerFields, 2))
	if got := GetVarintField(convFields, 4); got != cascadePlannerModeNoTool {
		t.Fatalf("planner_mode = %d, want %d", got, cascadePlannerModeNoTool)
	}
	if got := GetStringField(ParseFields(GetMessageField(convFields, 10)), 2); got != "No tools are available." {
		t.Fatalf("tool_calling override = %q", got)
	}
	additional := GetStringField(ParseFields(GetMessageField(convFields, 12)), 2)
	if !strings.Contains(additional, "Be concise.") {
		t.Fatalf("additional instructions missing system prompt: %q", additional)
	}
	if !strings.Contains(additional, "You have no tools") {
		t.Fatalf("additional instructions missing no-tool guidance: %q", additional)
	}
}

func TestBuildSendCascadeMessageRequestSkipsNoToolOverrideForStructuredToolProtocol(t *testing.T) {
	systemPrompt := `Return exactly one JSON object and nothing else.
{"action":"tool_call","tool_calls":[{"name":"search","arguments":{"q":"value"}}]}`

	req := BuildSendCascadeMessageRequest(
		"cascade-1",
		"user text",
		0,
		"claude-opus-4-7-low",
		"api-key",
		"1.0.0",
		systemPrompt,
	)

	fields := ParseFields(req)
	cfgFields := ParseFields(GetMessageField(fields, 5))
	plannerFields := ParseFields(GetMessageField(cfgFields, 1))
	convFields := ParseFields(GetMessageField(plannerFields, 2))

	if HasField(convFields, 10) {
		t.Fatalf("unexpected no-tool override for structured tool protocol")
	}

	additional := GetStringField(ParseFields(GetMessageField(convFields, 12)), 2)
	if additional != systemPrompt {
		t.Fatalf("additional instructions = %q, want original system prompt", additional)
	}
}

func TestBuildSendCascadeMessageRequestEnablesNativeCascadeTools(t *testing.T) {
	req := BuildSendCascadeMessageRequest(
		"cascade-1",
		"user text",
		0,
		"claude-opus-4-7-medium",
		"api-key",
		"1.0.0",
		NativeCascadeToolsMarker,
	)

	fields := ParseFields(req)
	cfgFields := ParseFields(GetMessageField(fields, 5))
	plannerFields := ParseFields(GetMessageField(cfgFields, 1))
	convFields := ParseFields(GetMessageField(plannerFields, 2))

	if got := GetVarintField(convFields, 4); got != cascadePlannerModeDefault {
		t.Fatalf("planner_mode = %d, want default", got)
	}
	if HasField(convFields, 10) {
		t.Fatalf("unexpected no-tool override for native Cascade tools")
	}
	if HasField(convFields, 12) {
		t.Fatalf("unexpected additional override for native Cascade tools")
	}
	if HasField(plannerFields, 11) {
		t.Fatalf("unexpected code changes override for native Cascade tools")
	}
}
