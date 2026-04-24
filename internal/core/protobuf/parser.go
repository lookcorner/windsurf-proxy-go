package protobuf

import (
	"strconv"
	"strings"

	"github.com/google/uuid"

	"windsurf-proxy-go/internal/core"
)

// CascadeToolCall represents a tool call made by Cascade during a session.
type CascadeToolCall struct {
	CallID    string
	Name      string
	Arguments string // JSON string
}

// CascadeToolResult represents result from a tool execution (step type=8).
type CascadeToolResult struct {
	ToolName string // e.g. "read_file"
	ToolURI  string // e.g. file URI
	Output   string // the actual output content
}

// CascadeResult represents parsed result from GetCascadeTrajectorySteps.
type CascadeResult struct {
	Text        string            // Final model text response (step 15, f20.f1)
	Thinking    string            // Model reasoning / chain-of-thought (step 15, f20.f3)
	Error       string            // Error messages
	Done        bool              // Whether the session is complete (step 23)
	ToolCalls   []CascadeToolCall // Tool calls detected
	ToolResults []CascadeToolResult
}

// UserStatusSnapshot is the subset of GetUserStatusResponse needed for
// scheduling and account visibility.
type UserStatusSnapshot struct {
	PlanName                    string
	TeamsTier                   int
	MonthlyPromptCredits        int
	MonthlyFlowCredits          int
	UsedPromptCredits           int64
	UsedFlowCredits             int64
	AvailablePromptCredits      int
	AvailableFlowCredits        int
	DailyQuotaRemainingPercent  int
	WeeklyQuotaRemainingPercent int
	DailyQuotaResetUnix         int64
	WeeklyQuotaResetUnix        int64
	HideDailyQuota              bool
	HideWeeklyQuota             bool
	MaxPremiumChatMessages      int64
	QuotaExhausted              bool
	AllowedModels               []string
}

// ParseStartCascadeResponse extracts cascade_id (field 1) from StartCascadeResponse.
func ParseStartCascadeResponse(data []byte) string {
	fields := ParseFields(data)
	for _, f := range fields {
		if f.FieldNumber == 1 && f.WireType == 2 {
			if data, ok := f.Value.([]byte); ok {
				return string(data)
			}
		}
	}
	return ""
}

// ParseGetUserStatusResponse extracts plan/quota data from GetUserStatusResponse.
func ParseGetUserStatusResponse(data []byte) UserStatusSnapshot {
	result := UserStatusSnapshot{}
	fields := ParseFields(data)

	if planInfo := GetMessageField(fields, 2); len(planInfo) > 0 {
		parsePlanInfoFields(ParseFields(planInfo), &result)
	}

	if userStatus := GetMessageField(fields, 1); len(userStatus) > 0 {
		userFields := ParseFields(userStatus)
		if used := int64(GetVarintField(userFields, 28)); used > 0 {
			result.UsedPromptCredits = used
		}
		if used := int64(GetVarintField(userFields, 29)); used > 0 {
			result.UsedFlowCredits = used
		}
		if max := int64(GetVarintField(userFields, 35)); max > 0 {
			result.MaxPremiumChatMessages = max
		}
		if planStatus := GetMessageField(userFields, 13); len(planStatus) > 0 {
			parsePlanStatusFields(ParseFields(planStatus), &result)
		}
	}

	result.QuotaExhausted = userStatusQuotaExhausted(result)
	return result
}

// ParseTrajectorySteps parses GetCascadeTrajectoryStepsResponse.
//
// Response has repeated CortexTrajectoryStep (field 1).
// Each step has:
//
//	field 1:  step_type (varint)
//	field 4:  step status (varint: 8=in_progress, 3=complete)
//	field 5:  step metadata
//	field 14: tool execution result (step type=8)
//	field 19: request echo (step type=14)
//	field 20: planner response (step type=15)
//	  → sub f1: final text response
//	  → sub f3: model thinking/reasoning
//	  → sub f7: tool call request
//	field 24: error info
//	field 30: done indicator (step type=23)
//
// Step types:
//
//	8:  tool execution result
//	9:  tool call info
//	14: request echo / context
//	15: planner text response
//	23: done
//	34: memory/context retrieval
func ParseTrajectorySteps(data []byte) CascadeResult {
	result := CascadeResult{
		Text:        "",
		Thinking:    "",
		Error:       "",
		Done:        false,
		ToolCalls:   make([]CascadeToolCall, 0),
		ToolResults: make([]CascadeToolResult, 0),
	}

	replyParts := make([]string, 0)
	thinkingParts := make([]string, 0)
	errors := make([]string, 0)
	toolCalls := make([]CascadeToolCall, 0)
	toolResults := make([]CascadeToolResult, 0)

	topFields := ParseFields(data)

	for _, stepF := range topFields {
		if stepF.FieldNumber != 1 || stepF.WireType != 2 {
			continue
		}

		stepData, ok := stepF.Value.([]byte)
		if !ok {
			continue
		}

		stepFields := ParseFields(stepData)

		// Get step_type and step_status
		stepType := GetVarintField(stepFields, 1)
		_ = GetVarintField(stepFields, 4) // stepStatus

		// Some LS versions expose completion as a dedicated done step
		// (step_type=23), others only toggle the done marker at field 30.
		if stepType == 23 || HasField(stepFields, 30) {
			result.Done = true
		}

		// step_type=9 is a tool call
		if stepType == 9 {
			tc, tr := parseToolCallStep(stepFields)
			if tc != nil {
				if !containsToolCall(toolCalls, tc) {
					toolCalls = append(toolCalls, *tc)
				}
			}
			if tr != nil {
				if !containsToolResult(toolResults, tr) {
					toolResults = append(toolResults, *tr)
				}
			}
		}

		// step_type=8 is tool execution result
		if stepType == 8 {
			tr := parseToolResultStep(stepFields)
			if tr != nil {
				if !containsToolResult(toolResults, tr) {
					toolResults = append(toolResults, *tr)
				}
			}
		}

		for _, tc := range parseAdditionalStepToolCalls(stepFields) {
			if !containsToolCall(toolCalls, &tc) {
				toolCalls = append(toolCalls, tc)
			}
		}

		for _, sf := range stepFields {
			if (sf.FieldNumber == 24 || sf.FieldNumber == 31) && sf.WireType == 2 {
				errData, ok := sf.Value.([]byte)
				if !ok {
					continue
				}
				if errText := extractCascadeErrorText(errData, 0); errText != "" {
					errors = append(errors, errText)
				}
			}

			// Parse field 20 (planner_response)
			if sf.FieldNumber == 20 && sf.WireType == 2 {
				f20Data, ok := sf.Value.([]byte)
				if !ok {
					continue
				}
				replyText, thinkingText := parsePlannerResponseTexts(f20Data)
				if replyText != "" {
					replyParts = append(replyParts, replyText)
				}
				if thinkingText != "" {
					thinkingParts = append(thinkingParts, thinkingText)
				}
				for _, ptc := range parsePlannerToolCalls(f20Data) {
					if !containsToolCall(toolCalls, &ptc) {
						toolCalls = append(toolCalls, ptc)
					}
				}
			}
		}
	}

	result.Text = strings.Join(replyParts, "")
	result.Thinking = strings.Join(thinkingParts, "")
	result.Error = strings.Join(errors, "; ")
	result.ToolCalls = toolCalls
	result.ToolResults = toolResults

	return result
}

// parseToolCallPayload extracts call_id, tool_name, and arguments from a nested tool payload.
func parseToolCallPayload(data []byte) (callID, toolName, arguments string) {
	callID = ""
	toolName = ""
	arguments = "{}"

	fields := ParseFields(data)
	for _, sf := range fields {
		if sf.WireType != 2 {
			continue
		}
		valBytes, ok := sf.Value.([]byte)
		if !ok {
			continue
		}
		val := string(valBytes)

		if sf.FieldNumber == 1 && strings.TrimSpace(val) != "" {
			callID = val
		} else if sf.FieldNumber == 2 {
			toolName = val
		} else if sf.FieldNumber == 3 && strings.HasPrefix(val, "{") {
			arguments = val
		}
	}

	return callID, toolName, arguments
}

func parsePlannerResponseTexts(f20Data []byte) (replyText string, thinkingText string) {
	fields := ParseFields(f20Data)
	var responseText string
	var modifiedText string
	var thinkingParts []string
	for _, pf := range fields {
		if pf.WireType != 2 {
			continue
		}
		txtBytes, ok := pf.Value.([]byte)
		if !ok {
			continue
		}
		txt := string(txtBytes)
		if strings.TrimSpace(txt) == "" {
			continue
		}
		switch pf.FieldNumber {
		case 1:
			responseText = txt
		case 3:
			thinkingParts = append(thinkingParts, txt)
		case 8:
			modifiedText = txt
		}
	}
	if modifiedText != "" {
		replyText = modifiedText
	} else {
		replyText = responseText
	}
	thinkingText = strings.Join(thinkingParts, "")
	return replyText, thinkingText
}

func parsePlanInfoFields(fields []ProtoField, result *UserStatusSnapshot) {
	if result == nil {
		return
	}
	if tier := int(GetVarintField(fields, 1)); tier != 0 {
		result.TeamsTier = tier
	}
	if name := strings.TrimSpace(GetStringField(fields, 2)); name != "" {
		result.PlanName = name
	}
	if max := int64(GetVarintField(fields, 6)); max > 0 {
		result.MaxPremiumChatMessages = max
	}
	if monthly := int(GetVarintField(fields, 12)); monthly > 0 {
		result.MonthlyPromptCredits = monthly
	}
	if monthly := int(GetVarintField(fields, 13)); monthly > 0 {
		result.MonthlyFlowCredits = monthly
	}
	if GetVarintField(fields, 36) != 0 {
		result.HideDailyQuota = true
	}
	if GetVarintField(fields, 37) != 0 {
		result.HideWeeklyQuota = true
	}
	result.AllowedModels = mergeUniqueStrings(result.AllowedModels, parseAllowedModels(fields))
}

func parsePlanStatusFields(fields []ProtoField, result *UserStatusSnapshot) {
	if result == nil {
		return
	}
	if planInfo := GetMessageField(fields, 1); len(planInfo) > 0 {
		parsePlanInfoFields(ParseFields(planInfo), result)
	}
	if available := int(GetVarintField(fields, 8)); available > 0 || hasNumericField(fields, 8) {
		result.AvailablePromptCredits = available
	}
	if available := int(GetVarintField(fields, 9)); available > 0 || hasNumericField(fields, 9) {
		result.AvailableFlowCredits = available
	}
	if used := int64(GetVarintField(fields, 6)); used > result.UsedPromptCredits {
		result.UsedPromptCredits = used
	}
	if used := int64(GetVarintField(fields, 5)); used > result.UsedFlowCredits {
		result.UsedFlowCredits = used
	}
	if remaining := int(GetVarintField(fields, 14)); remaining > 0 || hasNumericField(fields, 14) {
		result.DailyQuotaRemainingPercent = remaining
	}
	if remaining := int(GetVarintField(fields, 15)); remaining > 0 || hasNumericField(fields, 15) {
		result.WeeklyQuotaRemainingPercent = remaining
	}
	if reset := int64(GetVarintField(fields, 17)); reset > 0 {
		result.DailyQuotaResetUnix = reset
	}
	if reset := int64(GetVarintField(fields, 18)); reset > 0 {
		result.WeeklyQuotaResetUnix = reset
	}
}

func hasNumericField(fields []ProtoField, fieldNumber int) bool {
	for _, f := range fields {
		if f.FieldNumber == fieldNumber && f.WireType == 0 {
			return true
		}
	}
	return false
}

func userStatusQuotaExhausted(result UserStatusSnapshot) bool {
	if result.AvailablePromptCredits == 0 && (result.MonthlyPromptCredits > 0 || result.UsedPromptCredits > 0) {
		return true
	}
	if result.AvailableFlowCredits == 0 && (result.MonthlyFlowCredits > 0 || result.UsedFlowCredits > 0) {
		return true
	}
	if !result.HideDailyQuota && result.DailyQuotaRemainingPercent == 0 && result.DailyQuotaResetUnix > 0 {
		return true
	}
	if !result.HideWeeklyQuota && result.WeeklyQuotaRemainingPercent == 0 && result.WeeklyQuotaResetUnix > 0 {
		return true
	}
	return false
}

func parseAllowedModels(fields []ProtoField) []string {
	models := make([]string, 0)
	for _, field := range fields {
		if field.FieldNumber != 21 || field.WireType != 2 {
			continue
		}
		entry, ok := field.Value.([]byte)
		if !ok || len(entry) == 0 {
			continue
		}
		configFields := ParseFields(entry)
		modelOrAlias := GetMessageField(configFields, 1)
		if len(modelOrAlias) == 0 {
			continue
		}
		modelFields := ParseFields(modelOrAlias)
		enumValue := GetVarintField(modelFields, 1)
		if enumValue == 0 {
			continue
		}
		models = mergeUniqueStrings(models, core.ModelNamesForEnum(core.ModelEnum(enumValue)))
	}
	return models
}

func mergeUniqueStrings(current []string, next []string) []string {
	if len(next) == 0 {
		return current
	}
	seen := make(map[string]bool, len(current)+len(next))
	out := make([]string, 0, len(current)+len(next))
	for _, item := range current {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	for _, item := range next {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseToolCallStep extracts tool call info from a step type=9.
func parseToolCallStep(stepFields []ProtoField) (*CascadeToolCall, *CascadeToolResult) {
	var toolCall *CascadeToolCall
	var toolResult *CascadeToolResult
	var toolName string

	// Extract tool name from field 5
	for _, sf := range stepFields {
		if sf.FieldNumber == 5 && sf.WireType == 2 {
			innerData, ok := sf.Value.([]byte)
			if !ok {
				continue
			}
			inner := ParseFields(innerData)
			for _, f := range inner {
				if f.FieldNumber == 4 {
					callID, name, args := parseToolCallPayload(f.Value.([]byte))
					if name != "" {
						toolName = name
						toolCall = &CascadeToolCall{
							CallID:    callID,
							Name:      name,
							Arguments: args,
						}
						if toolCall.CallID == "" {
							toolCall.CallID = "tool-" + uuid.New().String()[:8]
						}
					}
				}
			}
		}
	}

	// Extract tool result from field 15 (list_dir results may be here)
	for _, sf := range stepFields {
		if sf.FieldNumber == 15 && sf.WireType == 2 {
			innerData, ok := sf.Value.([]byte)
			if !ok {
				continue
			}
			inner := ParseFields(innerData)
			var uri string
			entries := make([]string, 0)
			for _, f := range inner {
				if f.FieldNumber == 1 && f.WireType == 2 {
					if data, ok := f.Value.([]byte); ok {
						uri = string(data)
					}
				} else if f.FieldNumber == 3 && f.WireType == 2 {
					entryData, ok := f.Value.([]byte)
					if !ok {
						continue
					}
					entryFields := ParseFields(entryData)
					var entryName string
					var entrySize uint64
					var isDir bool
					for _, ef := range entryFields {
						if ef.FieldNumber == 1 && ef.WireType == 2 {
							if data, ok := ef.Value.([]byte); ok {
								entryName = string(data)
							}
						} else if ef.FieldNumber == 2 && ef.WireType == 0 {
							isDir = true
						} else if ef.FieldNumber == 3 && ef.WireType == 0 {
							isDir = true
						} else if ef.FieldNumber == 4 && ef.WireType == 0 {
							entrySize = ef.Value.(uint64)
						}
					}
					if entryName != "" {
						if isDir {
							entries = append(entries, "[DIR] "+entryName)
						} else if entrySize > 0 {
							entries = append(entries, "[FILE] "+entryName+" ("+strconv.FormatUint(entrySize, 10)+" bytes)")
						} else {
							entries = append(entries, entryName)
						}
					}
				}
			}
			if len(entries) > 0 {
				output := strings.Join(entries, "\n")
				toolResult = &CascadeToolResult{
					ToolName: toolName,
					ToolURI:  uri,
					Output:   output,
				}
			}
		}
	}

	return toolCall, toolResult
}

// parseToolResultStep extracts tool execution result from step type=8.
func parseToolResultStep(stepFields []ProtoField) *CascadeToolResult {
	var toolName string

	// Try to get tool name from field 5
	for _, sf := range stepFields {
		if sf.FieldNumber == 5 && sf.WireType == 2 {
			innerData, ok := sf.Value.([]byte)
			if !ok {
				continue
			}
			inner := ParseFields(innerData)
			for _, inner2 := range inner {
				if inner2.FieldNumber == 4 && inner2.WireType == 2 {
					_, toolName, _ = parseToolCallPayload(inner2.Value.([]byte))
				}
			}
		}
	}

	// Parse field 14 (read_file / command output)
	for _, sf := range stepFields {
		if sf.FieldNumber == 14 && sf.WireType == 2 {
			innerData, ok := sf.Value.([]byte)
			if !ok {
				continue
			}
			inner := ParseFields(innerData)
			var uri string
			var output string
			for _, f := range inner {
				if f.WireType != 2 {
					continue
				}
				if data, ok := f.Value.([]byte); ok {
					if f.FieldNumber == 1 {
						uri = string(data)
					} else if f.FieldNumber == 4 {
						output = string(data)
					}
				}
			}
			if output != "" {
				return &CascadeToolResult{
					ToolName: toolName,
					ToolURI:  uri,
					Output:   output,
				}
			}
		}

		// Parse field 15 (list_dir output)
		if sf.FieldNumber == 15 && sf.WireType == 2 {
			innerData, ok := sf.Value.([]byte)
			if !ok {
				continue
			}
			inner := ParseFields(innerData)
			var uri string
			entries := make([]string, 0)
			for _, f := range inner {
				if f.FieldNumber == 1 && f.WireType == 2 {
					if data, ok := f.Value.([]byte); ok {
						uri = string(data)
					}
				} else if f.FieldNumber == 3 && f.WireType == 2 {
					entryData, ok := f.Value.([]byte)
					if !ok {
						continue
					}
					entryFields := ParseFields(entryData)
					var entryName string
					var isDir bool
					for _, ef := range entryFields {
						if ef.FieldNumber == 1 && ef.WireType == 2 {
							if data, ok := ef.Value.([]byte); ok {
								entryName = string(data)
							}
						} else if ef.FieldNumber == 2 && ef.WireType == 0 {
							isDir = true
						} else if ef.FieldNumber == 3 && ef.WireType == 0 {
							isDir = true
						}
					}
					if entryName != "" {
						if isDir {
							entries = append(entries, "[DIR] "+entryName)
						} else {
							entries = append(entries, entryName)
						}
					}
				}
			}
			if len(entries) > 0 {
				return &CascadeToolResult{
					ToolName: toolName,
					ToolURI:  uri,
					Output:   strings.Join(entries, "\n"),
				}
			}
		}
	}

	return nil
}

// parsePlannerToolCalls extracts tool calls from step type=15 f20.f7.
func parsePlannerToolCalls(f20Data []byte) []CascadeToolCall {
	toolCalls := make([]CascadeToolCall, 0)
	fields := ParseFields(f20Data)

	for _, pf := range fields {
		if pf.FieldNumber == 7 && pf.WireType == 2 {
			toolData, ok := pf.Value.([]byte)
			if !ok {
				continue
			}
			callID, toolName, arguments := parseToolCallPayload(toolData)
			if toolName != "" {
				if callID == "" {
					callID = "tool-" + uuid.New().String()[:8]
				}
				toolCalls = append(toolCalls, CascadeToolCall{
					CallID:    callID,
					Name:      toolName,
					Arguments: arguments,
				})
			}
		}
	}

	return toolCalls
}

func parseAdditionalStepToolCalls(stepFields []ProtoField) []CascadeToolCall {
	toolCalls := make([]CascadeToolCall, 0)
	for _, sf := range stepFields {
		if sf.WireType != 2 {
			continue
		}
		data, ok := sf.Value.([]byte)
		if !ok {
			continue
		}
		switch sf.FieldNumber {
		case 45:
			if tc := parseCustomToolCall(data); tc != nil {
				toolCalls = append(toolCalls, *tc)
			}
		case 47:
			fields := ParseFields(data)
			if call := parseChatToolCall(GetMessageField(fields, 2)); call != nil {
				toolCalls = append(toolCalls, *call)
			}
		case 49:
			fields := ParseFields(data)
			if call := parseChatToolCall(GetMessageField(fields, 1)); call != nil {
				toolCalls = append(toolCalls, *call)
			}
		case 50:
			fields := ParseFields(data)
			choiceIdx := int(GetVarintField(fields, 2))
			candidates := make([]CascadeToolCall, 0)
			for _, f := range fields {
				if f.FieldNumber != 1 || f.WireType != 2 {
					continue
				}
				if data, ok := f.Value.([]byte); ok {
					if call := parseChatToolCall(data); call != nil {
						candidates = append(candidates, *call)
					}
				}
			}
			if len(candidates) > 0 {
				if choiceIdx < 0 || choiceIdx >= len(candidates) {
					choiceIdx = 0
				}
				toolCalls = append(toolCalls, candidates[choiceIdx])
			}
		}
	}
	return toolCalls
}

func parseCustomToolCall(data []byte) *CascadeToolCall {
	fields := ParseFields(data)
	callID := strings.TrimSpace(GetStringField(fields, 1))
	args := strings.TrimSpace(GetStringField(fields, 2))
	name := strings.TrimSpace(GetStringField(fields, 4))
	if name == "" {
		name = callID
	}
	if name == "" {
		return nil
	}
	if callID == "" {
		callID = "tool-" + uuid.New().String()[:8]
	}
	if args == "" {
		args = "{}"
	}
	return &CascadeToolCall{
		CallID:    callID,
		Name:      name,
		Arguments: args,
	}
}

func parseChatToolCall(data []byte) *CascadeToolCall {
	if len(data) == 0 {
		return nil
	}
	fields := ParseFields(data)
	callID := strings.TrimSpace(GetStringField(fields, 1))
	name := strings.TrimSpace(GetStringField(fields, 2))
	args := strings.TrimSpace(GetStringField(fields, 3))
	if name == "" {
		return nil
	}
	if callID == "" {
		callID = "tool-" + uuid.New().String()[:8]
	}
	if args == "" {
		args = "{}"
	}
	return &CascadeToolCall{
		CallID:    callID,
		Name:      name,
		Arguments: args,
	}
}

func extractCascadeErrorText(data []byte, depth int) string {
	if depth > 4 || len(data) == 0 {
		return ""
	}
	fields := ParseFields(data)
	for _, f := range fields {
		if f.WireType != 2 {
			continue
		}
		valBytes, ok := f.Value.([]byte)
		if !ok || len(valBytes) == 0 {
			continue
		}
		val := strings.TrimSpace(string(valBytes))
		if val != "" && isPrintable(val) {
			if len(val) > 200 {
				return val[:200]
			}
			return val
		}
		if nested := extractCascadeErrorText(valBytes, depth+1); nested != "" {
			return nested
		}
	}
	return ""
}

// ExtractTextFromResponse extracts text from RawGetChatMessageResponse.
func ExtractTextFromResponse(payload []byte) string {
	fields := ParseFields(payload)
	deltaMsg := GetMessageField(fields, 1)
	if deltaMsg == nil {
		return ""
	}

	msgFields := ParseFields(deltaMsg)
	return GetStringField(msgFields, 5)
}

// Helper functions

func containsToolCall(calls []CascadeToolCall, tc *CascadeToolCall) bool {
	for _, c := range calls {
		if c.Name == tc.Name && c.Arguments == tc.Arguments {
			return true
		}
	}
	return false
}

func containsToolResult(results []CascadeToolResult, tr *CascadeToolResult) bool {
	for _, r := range results {
		if r.ToolURI == tr.ToolURI && r.Output == tr.Output {
			return true
		}
	}
	return false
}

func isPrintable(s string) bool {
	for _, c := range s {
		if c < 32 && c != '\n' && c != '\r' && c != '\t' {
			return false
		}
		if c > 126 && c < 160 {
			return false
		}
	}
	return true
}

// ParseStreamingChunks parses gRPC-framed streaming data into text chunks.
func ParseStreamingChunks(data []byte) []string {
	payloads := GRPCUnframe(data)
	texts := make([]string, 0)
	for _, payload := range payloads {
		text := ExtractTextFromResponse(payload)
		if text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}
