// Package api provides OpenAI-compatible API types and handlers.
package api

import (
	"time"

	"github.com/google/uuid"
)

// ChatMessage represents a message in the chat completion request.
type ChatMessage struct {
	Role         string                 `json:"role"`
	Content      interface{}            `json:"content"` // string or array
	Name         string                 `json:"name,omitempty"`
	ToolCallID   string                 `json:"tool_call_id,omitempty"`
	ToolCalls    []AssistantToolCall    `json:"tool_calls,omitempty"`
}

// ToolDefinition represents a tool definition.
type ToolDefinition struct {
	Type     string            `json:"type"`
	Function *ToolCallFunction `json:"function,omitempty"`
}

// ToolCallFunction represents function definition.
type ToolCallFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// AssistantToolCall represents a tool call from assistant.
type AssistantToolCall struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Function FunctionCallInfo `json:"function"`
}

// FunctionCallInfo represents function call info.
type FunctionCallInfo struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionRequest represents OpenAI chat completion request.
type ChatCompletionRequest struct {
	Model            string          `json:"model"`
	Messages         []ChatMessage   `json:"messages"`
	Temperature      *float64        `json:"temperature,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	Stream           bool            `json:"stream,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	N                *int            `json:"n,omitempty"`
	Stop             interface{}     `json:"stop,omitempty"` // string or []string
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	Tools            []ToolDefinition `json:"tools,omitempty"`
	ToolChoice       interface{}     `json:"tool_choice,omitempty"`
}

// ChatCompletionResponse represents OpenAI chat completion response.
type ChatCompletionResponse struct {
	ID      string    `json:"id"`
	Object  string    `json:"object"`
	Created int64     `json:"created"`
	Model   string    `json:"model"`
	Choices []Choice  `json:"choices"`
	Usage   UsageInfo `json:"usage"`
}

// Choice represents a choice in the response.
type Choice struct {
	Index        int           `json:"index"`
	Message      MessageContent `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// MessageContent represents message content.
type MessageContent struct {
	Role      string             `json:"role"`
	Content   string             `json:"content,omitempty"`
	ToolCalls []AssistantToolCall `json:"tool_calls,omitempty"`
}

// UsageInfo represents token usage information.
type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk represents a streaming chunk.
type StreamChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChoiceDelta `json:"choices,omitempty"`
	Usage   *UsageInfo    `json:"usage,omitempty"`
}

// ChoiceDelta represents delta in streaming chunk.
type ChoiceDelta struct {
	Index        int          `json:"index"`
	Delta        DeltaContent `json:"delta"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

// DeltaContent represents delta content.
type DeltaContent struct {
	Role      string             `json:"role,omitempty"`
	Content   string             `json:"content,omitempty"`
	ToolCalls []DeltaToolCall    `json:"tool_calls,omitempty"`
}

// DeltaToolCall represents delta tool call.
type DeltaToolCall struct {
	Index    int             `json:"index"`
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type"`
	Function FunctionCallInfo `json:"function"`
}

// ModelInfo represents model information.
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelListResponse represents model list response.
type ModelListResponse struct {
	Object string       `json:"object"`
	Data   []ModelInfo  `json:"data"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail represents error detail.
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// NewChatCompletionResponse creates a new chat completion response.
func NewChatCompletionResponse(model string, content string, promptTokens int, completionTokens int) *ChatCompletionResponse {
	return &ChatCompletionResponse{
		ID:      "chatcmpl-" + generateID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: MessageContent{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: UsageInfo{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

// NewStreamChunk creates a new stream chunk.
func NewStreamChunk(model string, delta DeltaContent, finishReason string) *StreamChunk {
	return &StreamChunk{
		ID:      "chatcmpl-" + generateID(),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChoiceDelta{
			{
				Index:        0,
				Delta:        delta,
				FinishReason: finishReason,
			},
		},
	}
}

func generateID() string {
	return uuid.New().String()[:12]
}