// Package api - Anthropic Messages API (/v1/messages) types.
//
// Reference: https://docs.anthropic.com/en/api/messages
//
// This file defines the wire-level request/response shapes. Conversion
// to/from the internal (OpenAI-style) representation lives in
// anthropic_adapter.go.
package api

import "encoding/json"

// ============================================================================
// Request
// ============================================================================

// AnthropicMessagesRequest is the POST /v1/messages request body.
type AnthropicMessagesRequest struct {
	Model         string                 `json:"model"`
	MaxTokens     int                    `json:"max_tokens"`
	Messages      []AnthropicMessage     `json:"messages"`
	System        json.RawMessage        `json:"system,omitempty"` // string | []AnthropicBlock
	Stream        bool                   `json:"stream,omitempty"`
	Temperature   *float64               `json:"temperature,omitempty"`
	TopP          *float64               `json:"top_p,omitempty"`
	TopK          *int                   `json:"top_k,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	Tools         []AnthropicTool        `json:"tools,omitempty"`
	ToolChoice    json.RawMessage        `json:"tool_choice,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Thinking      json.RawMessage        `json:"thinking,omitempty"` // optional extended thinking opts
}

// AnthropicMessage is a single entry in the messages[] array.
// Content can be either a plain string or an array of content blocks.
type AnthropicMessage struct {
	Role    string          `json:"role"` // "user" | "assistant"
	Content json.RawMessage `json:"content"`
}

// AnthropicBlock is a content block used both in requests and responses.
// Different block types use different subsets of these fields.
type AnthropicBlock struct {
	Type string `json:"type"`

	// type=text
	Text string `json:"text,omitempty"`

	// type=tool_use (assistant side)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// type=tool_result (user side)
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"` // string | []AnthropicBlock
	IsError   bool            `json:"is_error,omitempty"`

	// type=image
	Source *AnthropicImageSource `json:"source,omitempty"`

	// type=thinking (extended thinking responses)
	Thinking string `json:"thinking,omitempty"`
}

// AnthropicImageSource describes an image attached to a message.
type AnthropicImageSource struct {
	Type      string `json:"type"` // "base64" | "url"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// AnthropicTool is a tool definition provided by the caller.
type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ============================================================================
// Response (non-stream)
// ============================================================================

// AnthropicMessagesResponse is the POST /v1/messages response body
// (non-streaming).
type AnthropicMessagesResponse struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"` // always "message"
	Role         string              `json:"role"` // always "assistant"
	Model        string              `json:"model"`
	Content      []AnthropicOutBlock `json:"content"`
	StopReason   string              `json:"stop_reason"`             // "end_turn" | "tool_use" | "max_tokens" | "stop_sequence"
	StopSequence *string             `json:"stop_sequence,omitempty"` // nil unless stop_reason=="stop_sequence"
	Usage        AnthropicUsage      `json:"usage"`
}

// AnthropicOutBlock is an output block returned in Content.
type AnthropicOutBlock struct {
	Type  string                 `json:"type"`            // "text" | "tool_use" | "thinking"
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`

	// type=thinking
	Thinking string `json:"thinking,omitempty"`
}

// AnthropicUsage is the usage block in the response.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ============================================================================
// Error
// ============================================================================

// AnthropicErrorResponse matches Anthropic's error wire format.
type AnthropicErrorResponse struct {
	Type  string               `json:"type"` // always "error"
	Error AnthropicErrorDetail `json:"error"`
}

// AnthropicErrorDetail is the inner error payload.
type AnthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ============================================================================
// Streaming events
// ============================================================================
//
// Anthropic streams emit named SSE events. The sequence for a successful
// response looks like:
//
//     event: message_start           { message: {id, role, model, content:[], usage:{input_tokens,output_tokens:0}}}
//     event: content_block_start     { index, content_block:{type:"text", text:""} }
//     event: content_block_delta     { index, delta:{type:"text_delta", text:"..."} }
//     ...
//     event: content_block_stop      { index }
//     event: message_delta           { delta:{stop_reason, stop_sequence}, usage:{output_tokens} }
//     event: message_stop            {}
//
// For tool calls an additional block is opened with
// content_block:{type:"tool_use", id, name, input:{}} and each delta uses
// delta:{type:"input_json_delta", partial_json:"..."}.

// AnthropicStreamMessageStart is the payload for event:message_start.
type AnthropicStreamMessageStart struct {
	Type    string                       `json:"type"` // "message_start"
	Message AnthropicStreamStartMessage  `json:"message"`
}

// AnthropicStreamStartMessage is the inner message placeholder carried by message_start.
type AnthropicStreamStartMessage struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"` // "message"
	Role         string              `json:"role"` // "assistant"
	Model        string              `json:"model"`
	Content      []AnthropicOutBlock `json:"content"`
	StopReason   *string             `json:"stop_reason"`
	StopSequence *string             `json:"stop_sequence"`
	Usage        AnthropicUsage      `json:"usage"`
}

// AnthropicStreamBlockStart is the payload for event:content_block_start.
type AnthropicStreamBlockStart struct {
	Type         string            `json:"type"` // "content_block_start"
	Index        int               `json:"index"`
	ContentBlock AnthropicOutBlock `json:"content_block"`
}

// AnthropicStreamBlockDelta is the payload for event:content_block_delta.
type AnthropicStreamBlockDelta struct {
	Type  string                      `json:"type"` // "content_block_delta"
	Index int                         `json:"index"`
	Delta AnthropicStreamContentDelta `json:"delta"`
}

// AnthropicStreamContentDelta is the delta body inside content_block_delta.
type AnthropicStreamContentDelta struct {
	Type        string `json:"type"`                   // "text_delta" | "input_json_delta" | "thinking_delta"
	Text        string `json:"text,omitempty"`         // text_delta
	PartialJSON string `json:"partial_json,omitempty"` // input_json_delta
	Thinking    string `json:"thinking,omitempty"`     // thinking_delta
}

// AnthropicStreamBlockStop is the payload for event:content_block_stop.
type AnthropicStreamBlockStop struct {
	Type  string `json:"type"` // "content_block_stop"
	Index int    `json:"index"`
}

// AnthropicStreamMessageDelta is the payload for event:message_delta.
type AnthropicStreamMessageDelta struct {
	Type  string                          `json:"type"` // "message_delta"
	Delta AnthropicStreamMessageDeltaInfo `json:"delta"`
	Usage AnthropicUsage                  `json:"usage"`
}

// AnthropicStreamMessageDeltaInfo carries final stop_reason / stop_sequence.
type AnthropicStreamMessageDeltaInfo struct {
	StopReason   string  `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence"`
}

// AnthropicStreamMessageStop is the payload for event:message_stop.
type AnthropicStreamMessageStop struct {
	Type string `json:"type"` // "message_stop"
}
