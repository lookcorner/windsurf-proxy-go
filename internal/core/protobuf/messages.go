package protobuf

import (
	"time"

	"github.com/google/uuid"
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

	return buf
}

// EncodeChatMessage encodes a ChatMessage for RawGetChatMessageRequest.
//
// ChatMessage structure:
//   Field 1: message_id (string)
//   Field 2: source (enum)
//   Field 3: timestamp (Timestamp message)
//   Field 4: conversation_id (string)
//   Field 5: content — for ASSISTANT: plain string, else: ChatMessageIntent message
func EncodeChatMessage(content string, source int, conversationID string) []byte {
	buf := make([]byte, 0)

	buf = append(buf, EncodeStringField(1, uuid.New().String())...)
	buf = append(buf, EncodeVarintField(2, uint64(source))...)
	buf = append(buf, EncodeMessageField(3, EncodeTimestamp(time.Now().UnixMilli()))...)
	buf = append(buf, EncodeStringField(4, conversationID)...)

	if source == ChatMessageSourceAssistant {
		buf = append(buf, EncodeStringField(5, content)...)
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
//   1: metadata (Metadata)
//   4: source enum (1 = CORTEX_TRAJECTORY_SOURCE_CASCADE_CLIENT)
//   5: trajectory_type enum (4 = CORTEX_TRAJECTORY_TYPE_CASCADE)
//
// Returns the *unframed* protobuf message body. The 5-byte gRPC length
// prefix is added by the gRPC runtime via rawCodec.
func BuildStartCascadeRequest(apiKey string, version string) []byte {
	meta := EncodeMetadata(apiKey, version)
	buf := make([]byte, 0)

	buf = append(buf, EncodeMessageField(1, meta)...)
	buf = append(buf, EncodeVarintField(4, 1)...) // source
	buf = append(buf, EncodeVarintField(5, 4)...) // trajectory_type

	return buf
}

// BuildSendCascadeMessageRequest builds SendUserCascadeMessageRequest protobuf.
//
// Fields:
//   1: cascade_id (string)
//   2: items (repeated TextOrScopeItem)
//   3: metadata (Metadata)
//   5: cascade_config (CascadeConfig)
func BuildSendCascadeMessageRequest(
	cascadeID string,
	text string,
	modelUID string,
	apiKey string,
	version string,
	systemPrompt string,
) []byte {
	meta := EncodeMetadata(apiKey, version)

	// CascadeConversationalPlannerConfig (empty for basic chat)
	convPlanner := make([]byte, 0)

	// CascadePlannerConfig: field 2=conversational, field 35=requested_model_uid
	planner := make([]byte, 0)
	planner = append(planner, EncodeMessageField(2, convPlanner)...)
	planner = append(planner, EncodeStringField(35, modelUID)...)

	// CascadeConfig: field 1=planner_config
	cascadeCfg := EncodeMessageField(1, planner)

	// TextOrScopeItem: field 1=text (oneof chunk)
	prompt := text
	if systemPrompt != "" {
		prompt = "[System: " + systemPrompt + "]\n\n" + text
	}
	item := EncodeStringField(1, prompt)

	buf := make([]byte, 0)
	buf = append(buf, EncodeStringField(1, cascadeID)...)
	buf = append(buf, EncodeMessageField(2, item)...)
	buf = append(buf, EncodeMessageField(3, meta)...)
	buf = append(buf, EncodeMessageField(5, cascadeCfg)...)

	return buf
}

// BuildGetTrajectoryStepsRequest builds GetCascadeTrajectoryStepsRequest.
// Field 1 = cascade_id
func BuildGetTrajectoryStepsRequest(cascadeID string) []byte {
	return EncodeStringField(1, cascadeID)
}

// BuildChatRequest builds RawGetChatMessageRequest (legacy path).
//
// Fields:
//   1: metadata
//   2: chat_messages (repeated)
//   3: system_prompt_override
//   4: chat_model (enum)
//   5: chat_model_name
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
			systemPrompt = content
		} else {
			source := RoleToSource[role]
			if source == 0 {
				source = ChatMessageSourceUser
			}
			encoded := EncodeChatMessage(content, source, conversationID)
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