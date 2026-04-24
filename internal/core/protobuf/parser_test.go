package protobuf

import "testing"

func TestParseTrajectoryStepsPrefersModifiedResponse(t *testing.T) {
	planner := make([]byte, 0)
	planner = append(planner, EncodeStringField(1, "draft")...)
	planner = append(planner, EncodeStringField(3, "thinking")...)
	planner = append(planner, EncodeStringField(8, "final response")...)

	step := make([]byte, 0)
	step = append(step, EncodeVarintField(1, 15)...)
	step = append(step, EncodeMessageField(20, planner)...)

	resp := EncodeMessageField(1, step)
	got := ParseTrajectorySteps(resp)

	if got.Text != "final response" {
		t.Fatalf("Text = %q, want %q", got.Text, "final response")
	}
	if got.Thinking != "thinking" {
		t.Fatalf("Thinking = %q, want %q", got.Thinking, "thinking")
	}
}

func TestParseTrajectoryStepsExtractsAdditionalToolCallSources(t *testing.T) {
	custom := make([]byte, 0)
	custom = append(custom, EncodeStringField(1, "recipe-1")...)
	custom = append(custom, EncodeStringField(2, `{"path":"a.txt"}`)...)
	custom = append(custom, EncodeStringField(4, "read_file")...)

	proposalInner := make([]byte, 0)
	proposalInner = append(proposalInner, EncodeStringField(1, "call-proposal")...)
	proposalInner = append(proposalInner, EncodeStringField(2, "search")...)
	proposalInner = append(proposalInner, EncodeStringField(3, `{"q":"abc"}`)...)
	proposal := EncodeMessageField(1, proposalInner)

	choiceA := make([]byte, 0)
	choiceA = append(choiceA, EncodeStringField(1, "call-a")...)
	choiceA = append(choiceA, EncodeStringField(2, "tool_a")...)
	choiceA = append(choiceA, EncodeStringField(3, `{"n":1}`)...)
	choiceB := make([]byte, 0)
	choiceB = append(choiceB, EncodeStringField(1, "call-b")...)
	choiceB = append(choiceB, EncodeStringField(2, "tool_b")...)
	choiceB = append(choiceB, EncodeStringField(3, `{"n":2}`)...)
	choice := make([]byte, 0)
	choice = append(choice, EncodeMessageField(1, choiceA)...)
	choice = append(choice, EncodeMessageField(1, choiceB)...)
	choice = append(choice, EncodeVarintField(2, 1)...)

	step := make([]byte, 0)
	step = append(step, EncodeVarintField(1, 15)...)
	step = append(step, EncodeMessageField(45, custom)...)
	step = append(step, EncodeMessageField(49, proposal)...)
	step = append(step, EncodeMessageField(50, choice)...)

	got := ParseTrajectorySteps(EncodeMessageField(1, step))
	if len(got.ToolCalls) != 3 {
		t.Fatalf("ToolCalls len = %d, want 3", len(got.ToolCalls))
	}

	want := map[string]string{
		"read_file": `{"path":"a.txt"}`,
		"search":    `{"q":"abc"}`,
		"tool_b":    `{"n":2}`,
	}
	for _, tc := range got.ToolCalls {
		args, ok := want[tc.Name]
		if !ok {
			t.Fatalf("unexpected tool call %q", tc.Name)
		}
		if tc.Arguments != args {
			t.Fatalf("%s arguments = %q, want %q", tc.Name, tc.Arguments, args)
		}
	}
}

func TestParsePlannerToolCallsPreservesGenericCallID(t *testing.T) {
	toolPayload := make([]byte, 0)
	toolPayload = append(toolPayload, EncodeStringField(1, "arbitrary-id-123")...)
	toolPayload = append(toolPayload, EncodeStringField(2, "search")...)
	toolPayload = append(toolPayload, EncodeStringField(3, `{"q":"x"}`)...)

	planner := EncodeMessageField(7, toolPayload)
	calls := parsePlannerToolCalls(planner)
	if len(calls) != 1 {
		t.Fatalf("calls len = %d, want 1", len(calls))
	}
	if calls[0].CallID != "arbitrary-id-123" {
		t.Fatalf("CallID = %q, want %q", calls[0].CallID, "arbitrary-id-123")
	}
}
