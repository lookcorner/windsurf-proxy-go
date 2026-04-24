package protobuf

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	cascadePlannerModeDefault = 1
	cascadePlannerModeNoTool  = 3

	sectionOverrideModeOverride = 1
	cascadeMaxOutputTokens      = 32768
	NativeCascadeToolsMarker    = "__windsurf_proxy_native_cascade_tools__"
)

// ChatMessageSource enum values (protobuf enum)
const (
	ChatMessageSourceUnspecified = 0
	ChatMessageSourceUser        = 1
	ChatMessageSourceSystem      = 2
	ChatMessageSourceAssistant   = 3
	ChatMessageSourceTool        = 4
)

// RoleToSource maps OpenAI roles to protobuf source enum.
var RoleToSource = map[string]int{
	"user":      ChatMessageSourceUser,
	"system":    ChatMessageSourceSystem,
	"assistant": ChatMessageSourceAssistant,
	"tool":      ChatMessageSourceTool,
}

// Metadata field numbers (discovered from Windsurf extension.js)
var defaultMetadataFields = map[string]int{
	"api_key":           3,
	"ide_name":          1,
	"ide_version":       7,
	"extension_version": 2,
	"session_id":        10,
	"locale":            4,
	"os":                5,
	"hardware":          8,
	"request_id":        9,
	"extension_name":    12,
}

// EncodeMetadata encodes the Metadata message.
func EncodeMetadata(apiKey string, version string) []byte {
	f := defaultMetadataFields
	buf := make([]byte, 0)

	buf = append(buf, EncodeStringField(f["api_key"], apiKey)...)
	buf = append(buf, EncodeStringField(f["ide_name"], "windsurf")...)
	buf = append(buf, EncodeStringField(f["ide_version"], version)...)
	buf = append(buf, EncodeStringField(f["extension_version"], version)...)
	buf = append(buf, EncodeStringField(f["session_id"], uuid.New().String())...)
	buf = append(buf, EncodeStringField(f["locale"], "en")...)
	buf = append(buf, EncodeStringField(f["os"], metadataOS())...)
	buf = append(buf, EncodeStringField(f["hardware"], metadataHardware())...)
	buf = append(buf, EncodeVarintField(f["request_id"], metadataRequestID())...)
	buf = append(buf, EncodeStringField(f["extension_name"], "windsurf")...)

	return buf
}

// EncodeChatMessage encodes a ChatMessage for RawGetChatMessageRequest.
//
// ChatMessage structure:
//
//	Field 1: message_id (string)
//	Field 2: source (enum)
//	Field 3: timestamp (Timestamp message)
//	Field 4: conversation_id (string)
//	Field 5: content intent — for USER/SYSTEM/TOOL: ChatMessageIntent message
//	Field 6: content action — for ASSISTANT: ChatMessageAction message
func EncodeChatMessage(content string, source int, conversationID string) []byte {
	buf := make([]byte, 0)

	buf = append(buf, EncodeStringField(1, uuid.New().String())...)
	buf = append(buf, EncodeVarintField(2, uint64(source))...)
	buf = append(buf, EncodeMessageField(3, EncodeTimestamp(time.Now().UnixMilli()))...)
	buf = append(buf, EncodeStringField(4, conversationID)...)

	if source == ChatMessageSourceAssistant {
		// Assistant messages must use ChatMessageAction { generic { text } }.
		actionGeneric := EncodeStringField(1, content)
		action := EncodeMessageField(1, actionGeneric)
		buf = append(buf, EncodeMessageField(6, action)...)
	} else {
		// ChatMessageIntent -> field 1: IntentGeneric -> field 1: text
		intentGeneric := EncodeStringField(1, content)
		intent := EncodeMessageField(1, intentGeneric)
		buf = append(buf, EncodeMessageField(5, intent)...)
	}

	return buf
}

// BuildStartCascadeRequest builds StartCascadeRequest protobuf.
//
// Fields:
//
//	1: metadata (Metadata)
//	4: source enum (1 = CORTEX_TRAJECTORY_SOURCE_CASCADE_CLIENT)
//	5: trajectory_type enum (1 = CORTEX_TRAJECTORY_TYPE_USER_MAINLINE)
//
// Returns the *unframed* protobuf message body. The 5-byte gRPC length
// prefix is added by the gRPC runtime via rawCodec.
func BuildStartCascadeRequest(apiKey string, version string) []byte {
	meta := EncodeMetadata(apiKey, version)
	buf := make([]byte, 0)

	buf = append(buf, EncodeMessageField(1, meta)...)
	buf = append(buf, EncodeVarintField(4, 1)...) // source
	buf = append(buf, EncodeVarintField(5, 1)...) // trajectory_type

	return buf
}

// BuildSendCascadeMessageRequest builds SendUserCascadeMessageRequest protobuf.
//
// Fields:
//
//	1: cascade_id (string)
//	2: items (repeated TextOrScopeItem)
//	3: metadata (Metadata)
//	5: cascade_config (CascadeConfig)
func BuildSendCascadeMessageRequest(
	cascadeID string,
	text string,
	modelEnum int,
	modelUID string,
	apiKey string,
	version string,
	systemPrompt string,
) []byte {
	meta := EncodeMetadata(apiKey, version)
	cascadeCfg := buildCascadeConfig(modelEnum, modelUID, systemPrompt)

	// TextOrScopeItem: field 1=text (oneof chunk)
	item := EncodeStringField(1, text)

	buf := make([]byte, 0)
	buf = append(buf, EncodeStringField(1, cascadeID)...)
	buf = append(buf, EncodeMessageField(2, item)...)
	buf = append(buf, EncodeMessageField(3, meta)...)
	buf = append(buf, EncodeMessageField(5, cascadeCfg)...)

	return buf
}

func buildCascadeConfig(modelEnum int, modelUID string, systemPrompt string) []byte {
	buf := make([]byte, 0)
	buf = append(buf, EncodeMessageField(1, buildCascadePlannerConfig(modelEnum, modelUID, systemPrompt))...)
	buf = append(buf, EncodeMessageField(5, EncodeBoolField(1, false))...)
	buf = append(buf, EncodeMessageField(7, buildCascadeBrainConfig())...)
	return buf
}

func buildCascadePlannerConfig(modelEnum int, modelUID string, systemPrompt string) []byte {
	buf := make([]byte, 0)
	buf = append(buf, EncodeMessageField(2, buildCascadeConversationalPlannerConfig(systemPrompt))...)

	if modelUID != "" {
		buf = append(buf, EncodeStringField(35, modelUID)...)
		buf = append(buf, EncodeStringField(34, modelUID)...)
	}
	if modelEnum > 0 {
		buf = append(buf, EncodeMessageField(15, EncodeVarintField(1, uint64(modelEnum)))...)
		buf = append(buf, EncodeVarintField(1, uint64(modelEnum))...)
	}

	buf = append(buf, EncodeVarintField(6, cascadeMaxOutputTokens)...)
	if !hasStructuredToolProtocol(systemPrompt) && !useNativeCascadeTools(systemPrompt) {
		buf = append(buf, EncodeMessageField(11, buildSectionOverride(""))...)
	}

	return buf
}

func buildCascadeConversationalPlannerConfig(systemPrompt string) []byte {
	buf := make([]byte, 0)
	if useNativeCascadeTools(systemPrompt) {
		buf = append(buf, EncodeVarintField(4, cascadePlannerModeDefault)...)
		return buf
	}
	mode := cascadePlannerModeNoTool
	buf = append(buf, EncodeVarintField(4, uint64(mode))...)

	if hasStructuredToolProtocol(systemPrompt) {
		if text := strings.TrimSpace(systemPrompt); text != "" {
			buf = append(buf, EncodeMessageField(12, buildSectionOverride(text))...)
		}
		buf = append(buf, EncodeMessageField(13, buildSectionOverride(
			"Use the caller-provided tool protocol when it is relevant. Do not claim that tools are unavailable if the instructions define them.",
		))...)
		return buf
	}

	buf = append(buf, EncodeMessageField(10, buildSectionOverride("No tools are available."))...)

	additional := strings.TrimSpace(systemPrompt)
	if additional != "" {
		additional += "\n\n"
	}
	additional += "You have no tools, no file access, and no command execution. Answer directly without pretending to inspect files or run commands."
	buf = append(buf, EncodeMessageField(12, buildSectionOverride(additional))...)
	buf = append(buf, EncodeMessageField(13, buildSectionOverride(
		"Answer directly and concisely. Do not present yourself as having IDE, filesystem, or terminal control.",
	))...)

	return buf
}

func buildSectionOverride(text string) []byte {
	buf := make([]byte, 0)
	buf = append(buf, EncodeVarintField(1, sectionOverrideModeOverride)...)
	buf = append(buf, EncodeStringField(2, text)...)
	return buf
}

func buildCascadeBrainConfig() []byte {
	buf := make([]byte, 0)
	buf = append(buf, EncodeVarintField(1, cascadePlannerModeDefault)...)
	buf = append(buf, EncodeMessageField(6, EncodeMessageField(6, []byte{}))...)
	return buf
}

func hasStructuredToolProtocol(systemPrompt string) bool {
	if systemPrompt == "" {
		return false
	}
	return strings.Contains(systemPrompt, `"action":"tool_call"`) &&
		strings.Contains(systemPrompt, `"tool_calls"`)
}

func useNativeCascadeTools(systemPrompt string) bool {
	return strings.Contains(systemPrompt, NativeCascadeToolsMarker)
}

// BuildGetTrajectoryStepsRequest builds GetCascadeTrajectoryStepsRequest.
// Field 1 = cascade_id
func BuildGetTrajectoryStepsRequest(cascadeID string) []byte {
	return EncodeStringField(1, cascadeID)
}

// BuildGetUserStatusRequest builds GetUserStatusRequest.
// Field 1 = metadata
func BuildGetUserStatusRequest(apiKey string, version string) []byte {
	return EncodeMessageField(1, EncodeMetadata(apiKey, version))
}

// BuildChatRequest builds RawGetChatMessageRequest (legacy path).
//
// Fields:
//
//	1: metadata
//	2: chat_messages (repeated)
//	3: system_prompt_override
//	4: chat_model (enum)
//	5: chat_model_name
func BuildChatRequest(
	apiKey string,
	messages []map[string]string,
	modelEnum int,
	modelName string,
	version string,
) []byte {
	conversationID := uuid.New().String()
	buf := make([]byte, 0)

	// Field 1: metadata
	buf = append(buf, EncodeMessageField(1, EncodeMetadata(apiKey, version))...)

	// Separate system messages
	var systemPrompt string
	for _, msg := range messages {
		role := msg["role"]
		content := msg["content"]
		if role == "system" {
			if systemPrompt != "" {
				systemPrompt += "\n"
			}
			systemPrompt += content
		} else {
			source := RoleToSource[role]
			text := content
			switch role {
			case "tool":
				// Legacy RawGetChatMessage rejects tool-role turns, so carry the
				// tool output back as a synthetic user utterance instead.
				source = ChatMessageSourceUser
				text = formatLegacyToolResult(msg["tool_call_id"], msg["name"], content)
			case "assistant":
				source = ChatMessageSourceAssistant
			default:
				if source == 0 {
					source = ChatMessageSourceUser
				}
			}
			encoded := EncodeChatMessage(text, source, conversationID)
			buf = append(buf, EncodeMessageField(2, encoded)...)
		}
	}

	// Field 3: system_prompt_override
	if systemPrompt != "" {
		buf = append(buf, EncodeStringField(3, systemPrompt)...)
	}

	// Field 4: chat_model (enum)
	buf = append(buf, EncodeVarintField(4, uint64(modelEnum))...)

	// Field 5: chat_model_name
	if modelName != "" {
		buf = append(buf, EncodeStringField(5, modelName)...)
	}

	// Return the unframed message body. gRPC runtime adds the 5-byte prefix.
	return buf
}

func metadataOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

func metadataHardware() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x86_64"
}

func metadataRequestID() uint64 {
	var buf [8]byte
	if _, err := cryptorand.Read(buf[:]); err == nil {
		return binary.BigEndian.Uint64(buf[:]) & ((1 << 48) - 1)
	}
	return uint64(time.Now().UnixNano()) & ((1 << 48) - 1)
}

func formatLegacyToolResult(toolCallID string, toolName string, content string) string {
	switch {
	case toolCallID != "":
		return "[tool result for " + toolCallID + "]: " + content
	case toolName != "":
		return "[tool result for " + toolName + "]: " + content
	default:
		return "[tool result]: " + content
	}
}
