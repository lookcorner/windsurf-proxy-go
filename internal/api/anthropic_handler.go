package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"windsurf-proxy-go/internal/audit"
	"windsurf-proxy-go/internal/core"
	"windsurf-proxy-go/internal/core/protobuf"
	"windsurf-proxy-go/internal/tokenizer"
	"windsurf-proxy-go/internal/tool_adapter"

	"github.com/google/uuid"
)

// handleAnthropicMessages handles POST /v1/messages.
//
// Protocol shape matches https://docs.anthropic.com/en/api/messages. The
// request is translated into the proxy's internal representation, then
// routed through the same balancer/gRPC pipeline as the OpenAI endpoint
// before the response is re-encoded in Anthropic form.
func (h *Handler) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed,
			"invalid_request_error", "Method not allowed")
		return
	}

	t0 := time.Now()

	_, r, entry := startAudit("anthropic", w, r)

	apiKey := h.validateAnthropicAuth(w, r)
	if apiKey == "" {
		h.recordRequest("", "", false, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, "invalid or missing API key")
		entry.Finish(http.StatusUnauthorized, fmt.Errorf("invalid or missing API key"))
		return
	}

	var req AnthropicMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.recordRequest("", "", false, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, "invalid request body")
		writeAnthropicError(w, http.StatusBadRequest,
			"invalid_request_error", "invalid request body: "+err.Error())
		entry.Finish(http.StatusBadRequest, err)
		return
	}
	entry.SetStream(req.Stream)

	if req.Model == "" {
		writeAnthropicError(w, http.StatusBadRequest,
			"invalid_request_error", "'model' is required")
		entry.Finish(http.StatusBadRequest, fmt.Errorf("model required"))
		return
	}
	if req.MaxTokens <= 0 {
		writeAnthropicError(w, http.StatusBadRequest,
			"invalid_request_error", "'max_tokens' is required and must be > 0")
		entry.Finish(http.StatusBadRequest, fmt.Errorf("max_tokens required"))
		return
	}

	// Resolve Anthropic alias (e.g. claude-3-5-sonnet-20241022 → claude-3.5-sonnet).
	internalModel := core.ResolveAnthropicAlias(req.Model)
	entry.SetModel(req.Model, internalModel)
	if !core.IsModelSupported(internalModel) {
		h.recordRequest(req.Model, "", req.Stream, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, "model not supported")
		writeAnthropicError(w, http.StatusBadRequest,
			"invalid_request_error",
			fmt.Sprintf("Model '%s' not supported", req.Model))
		entry.Finish(http.StatusBadRequest, fmt.Errorf("model %q not supported", req.Model))
		return
	}

	if !h.keys.IsModelAllowed(apiKey, internalModel) {
		h.recordRequest(req.Model, "", req.Stream, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, "model not allowed for this key")
		writeAnthropicError(w, http.StatusForbidden,
			"permission_error",
			fmt.Sprintf("Model '%s' not allowed for this key", req.Model))
		entry.Finish(http.StatusForbidden, fmt.Errorf("model not allowed for this key"))
		return
	}

	if !h.keys.CheckRateLimit(apiKey) {
		h.recordRequest(req.Model, "", req.Stream, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, "rate limit exceeded")
		writeAnthropicError(w, http.StatusTooManyRequests,
			"rate_limit_error", "Rate limit exceeded")
		entry.Finish(http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded"))
		return
	}

	messagesRaw, toolsList, err := convertAnthropicRequest(&req)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest,
			"invalid_request_error", err.Error())
		entry.Finish(http.StatusBadRequest, err)
		return
	}

	resolved := core.ResolveModel(internalModel)

	hasTools := tool_adapter.HasToolUsage(messagesRaw, toolsList)
	messages := convertMessages(messagesRaw)
	if hasTools {
		messages = tool_adapter.BuildToolPrompt(toolsList, messagesRaw)
	}

	log.Printf("[Anthropic] Request: model=%s (internal=%s) stream=%v messages=%d tools=%d hasTools=%v",
		req.Model, internalModel, req.Stream, len(messages), len(toolsList), hasTools)

	if req.Stream {
		h.handleAnthropicStream(w, r, messages, resolved, req.Model)
	} else {
		h.handleAnthropicNonStream(w, r, messages, resolved, req.Model)
	}
}

// ============================================================================
// Non-streaming
// ============================================================================

func (h *Handler) handleAnthropicNonStream(
	w http.ResponseWriter,
	r *http.Request,
	messages []map[string]string,
	resolved core.ResolvedModel,
	displayModel string,
) {
	w.Header().Set("Content-Type", "application/json")

	entry := audit.FromContext(r.Context())

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	maxRetries := h.config.Balancing.MaxRetries
	attempts := retryAttempts(maxRetries)
	tried := make(map[string]bool)
	var lastError string
	var lastInstance string
	t0 := time.Now()
	promptTokens := tokenizer.CountMessagesTokens(messages, displayModel)

	for attempt := 0; attempt < attempts; attempt++ {
		inst, err := h.balancer.GetInstance(tried)
		if err != nil {
			if lastError == "" {
				lastError = err.Error()
			}
			h.recordRequest(displayModel, lastInstance, false, "error",
				int(time.Since(t0).Milliseconds()), promptTokens, 0, promptTokens, lastError)
			writeAnthropicError(w, http.StatusServiceUnavailable,
				"overloaded_error",
				fmt.Sprintf("All instances failed: %s", lastError))
			entry.Finish(http.StatusServiceUnavailable, fmt.Errorf("all instances failed: %s", lastError))
			return
		}

		tried[inst.Name] = true
		lastInstance = inst.Name
		entry.SetUpstreamTarget(fmt.Sprintf("%s:%d", inst.Host, inst.Port))

		stream, err := inst.Client.ChatStream(ctx, inst.APIKey, messages,
			resolved.EnumValue, resolved.ModelName, inst.Version)
		if err != nil {
			h.balancer.MarkError(inst, err.Error())
			h.balancer.ReleaseInstance(inst)
			lastError = err.Error()
			log.Printf("[Anthropic] Instance '%s' failed (attempt %d/%d): %v",
				inst.Name, attempt+1, attempts, err)
			if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
				lastError = ctx.Err().Error()
				break
			}
			continue
		}

		var (
			contentParts []string
			toolCalls    []protobuf.CascadeToolCall
		)
		for event := range stream {
			switch event.Type {
			case "text":
				if text, ok := event.Data.(string); ok {
					contentParts = append(contentParts, text)
				}
			case "tool_call":
				if tc, ok := event.Data.(protobuf.CascadeToolCall); ok {
					toolCalls = append(toolCalls, tc)
				}
			}
		}

		h.balancer.MarkSuccess(inst)
		h.balancer.ReleaseInstance(inst)

		fullContent := strings.Join(contentParts, "")
		completionTokens := tokenizer.CountTextTokens(fullContent, displayModel)

		blocks := make([]AnthropicOutBlock, 0, 1+len(toolCalls))
		if fullContent != "" {
			blocks = append(blocks, AnthropicOutBlock{
				Type: "text",
				Text: fullContent,
			})
		}
		for i, tc := range toolCalls {
			var input map[string]interface{}
			if tc.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Arguments), &input)
			}
			if input == nil {
				input = map[string]interface{}{}
			}
			blocks = append(blocks, AnthropicOutBlock{
				Type:  "tool_use",
				ID:    fmt.Sprintf("toolu_%s_%d", uuid.New().String()[:8], i),
				Name:  tc.Name,
				Input: input,
			})
			completionTokens += tokenizer.CountTextTokens(tc.Arguments, displayModel)
		}

		// At least one block is required by the Anthropic schema.
		if len(blocks) == 0 {
			blocks = append(blocks, AnthropicOutBlock{Type: "text", Text: ""})
		}

		stopReason := "end_turn"
		if len(toolCalls) > 0 {
			stopReason = "tool_use"
		}

		resp := AnthropicMessagesResponse{
			ID:      "msg_" + generateID(),
			Type:    "message",
			Role:    "assistant",
			Model:   displayModel,
			Content: blocks,
			Usage: AnthropicUsage{
				InputTokens:  promptTokens,
				OutputTokens: completionTokens,
			},
			StopReason: stopReason,
		}

		duration := time.Since(t0).Milliseconds()
		total := promptTokens + completionTokens
		h.recordRequest(displayModel, inst.Name, false, "ok",
			int(duration), promptTokens, completionTokens, total, "")

		respBytes, _ := json.Marshal(&resp)
		_, _ = w.Write(respBytes)
		entry.SetResponseBody(respBytes)
		log.Printf("[Anthropic] Complete: model=%s tokens=%d duration=%dms",
			displayModel, total, duration)
		entry.Finish(http.StatusOK, nil)
		return
	}

	if lastError == "" {
		lastError = "request failed"
	}
	h.recordRequest(displayModel, lastInstance, false, "error",
		int(time.Since(t0).Milliseconds()), promptTokens, 0, promptTokens, lastError)
	writeAnthropicError(w, http.StatusServiceUnavailable,
		"overloaded_error",
		fmt.Sprintf("All instances failed after %d retries: %s", maxRetries, lastError))
	entry.Finish(http.StatusServiceUnavailable, fmt.Errorf("all instances failed: %s", lastError))
}

// ============================================================================
// Streaming
// ============================================================================

func (h *Handler) handleAnthropicStream(
	w http.ResponseWriter,
	r *http.Request,
	messages []map[string]string,
	resolved core.ResolvedModel,
	displayModel string,
) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	entry := audit.FromContext(r.Context())
	sse := NewAnthropicSSEWriter(w)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	maxRetries := h.config.Balancing.MaxRetries
	attempts := retryAttempts(maxRetries)
	tried := make(map[string]bool)
	var lastError string
	var lastInstance string
	t0 := time.Now()
	promptTokens := tokenizer.CountMessagesTokens(messages, displayModel)

	messageID := "msg_" + generateID()

	for attempt := 0; attempt < attempts; attempt++ {
		inst, err := h.balancer.GetInstance(tried)
		if err != nil {
			if lastError == "" {
				lastError = err.Error()
			}
			h.recordRequest(displayModel, lastInstance, true, "error",
				int(time.Since(t0).Milliseconds()), promptTokens, 0, promptTokens, lastError)
			sse.WriteError("overloaded_error",
				fmt.Sprintf("All instances failed: %s", lastError))
			entry.Finish(http.StatusServiceUnavailable, fmt.Errorf("all instances failed: %s", lastError))
			return
		}

		tried[inst.Name] = true
		lastInstance = inst.Name
		entry.SetUpstreamTarget(fmt.Sprintf("%s:%d", inst.Host, inst.Port))

		stream, err := inst.Client.ChatStream(ctx, inst.APIKey, messages,
			resolved.EnumValue, resolved.ModelName, inst.Version)
		if err != nil {
			h.balancer.MarkError(inst, err.Error())
			h.balancer.ReleaseInstance(inst)
			lastError = err.Error()
			log.Printf("[Anthropic] Instance '%s' failed (attempt %d/%d): %v",
				inst.Name, attempt+1, attempts, err)
			if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
				lastError = ctx.Err().Error()
				break
			}
			continue
		}

		// Send message_start before we emit any block.
		_ = sse.WriteMessageStart(messageID, displayModel, promptTokens)

		var (
			contentParts      []string
			completionTokens  int
			textBlockOpen     bool
			nextBlockIndex    = 0
			textBlockIndex    = -1
			hasToolCall       bool
			lastToolBlockIdx  = -1
		)

		for event := range stream {
			switch event.Type {
			case "text":
				text, ok := event.Data.(string)
				if !ok || text == "" {
					continue
				}
				if !textBlockOpen {
					textBlockIndex = nextBlockIndex
					nextBlockIndex++
					_ = sse.WriteBlockStart(textBlockIndex, AnthropicOutBlock{
						Type: "text", Text: "",
					})
					textBlockOpen = true
				}
				_ = sse.WriteTextDelta(textBlockIndex, text)
				contentParts = append(contentParts, text)
				entry.AppendStreamText(text)

			case "tool_call":
				tc, ok := event.Data.(protobuf.CascadeToolCall)
				if !ok {
					continue
				}
				// Close any open text block before opening a tool_use block.
				if textBlockOpen {
					_ = sse.WriteBlockStop(textBlockIndex)
					textBlockOpen = false
				}
				idx := nextBlockIndex
				nextBlockIndex++
				lastToolBlockIdx = idx
				toolUseID := fmt.Sprintf("toolu_%s_%d", uuid.New().String()[:8], idx)

				_ = sse.WriteBlockStart(idx, AnthropicOutBlock{
					Type:  "tool_use",
					ID:    toolUseID,
					Name:  tc.Name,
					Input: map[string]interface{}{},
				})
				args := tc.Arguments
				if args == "" || args == "null" {
					args = "{}"
				}
				_ = sse.WriteInputJSONDelta(idx, args)
				_ = sse.WriteBlockStop(idx)
				entry.AppendStreamText(fmt.Sprintf("\n[tool_use %s %s]", tc.Name, args))

				completionTokens += tokenizer.CountTextTokens(tc.Arguments, displayModel)
				hasToolCall = true
			}
		}

		if textBlockOpen {
			_ = sse.WriteBlockStop(textBlockIndex)
			textBlockOpen = false
		}
		_ = lastToolBlockIdx // currently unused but reserved for follow-up deltas

		h.balancer.MarkSuccess(inst)
		h.balancer.ReleaseInstance(inst)

		fullContent := strings.Join(contentParts, "")
		completionTokens += tokenizer.CountTextTokens(fullContent, displayModel)

		stopReason := "end_turn"
		if hasToolCall {
			stopReason = "tool_use"
		}

		_ = sse.WriteMessageDelta(stopReason, completionTokens)
		_ = sse.WriteMessageStop()

		duration := time.Since(t0).Milliseconds()
		total := promptTokens + completionTokens
		h.recordRequest(displayModel, inst.Name, true, "ok",
			int(duration), promptTokens, completionTokens, total, "")
		log.Printf("[Anthropic] Stream complete: model=%s tokens=%d duration=%dms",
			displayModel, total, duration)
		entry.Finish(http.StatusOK, nil)
		return
	}

	if lastError == "" {
		lastError = "request failed"
	}
	h.recordRequest(displayModel, lastInstance, true, "error",
		int(time.Since(t0).Milliseconds()), promptTokens, 0, promptTokens, lastError)
	sse.WriteError("overloaded_error",
		fmt.Sprintf("All instances failed after %d retries: %s", maxRetries, lastError))
	entry.Finish(http.StatusServiceUnavailable, fmt.Errorf("all instances failed: %s", lastError))
}

// ============================================================================
// Auth & error helpers
// ============================================================================

// validateAnthropicAuth looks up the API key from either x-api-key (Anthropic
// native) or Authorization: Bearer (OpenAI-compatible). Returns the matched
// key string, or "" after writing an error response.
func (h *Handler) validateAnthropicAuth(w http.ResponseWriter, r *http.Request) string {
	token := r.Header.Get("x-api-key")
	if token == "" {
		token = r.Header.Get("X-Api-Key")
	}
	if token == "" {
		token = r.Header.Get("Authorization")
	}

	entry := h.keys.Validate(token)
	if entry == nil {
		writeAnthropicError(w, http.StatusUnauthorized,
			"authentication_error", "Invalid or missing API key")
		return ""
	}
	return entry.Key
}

func writeAnthropicError(w http.ResponseWriter, code int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(AnthropicErrorResponse{
		Type: "error",
		Error: AnthropicErrorDetail{
			Type:    errType,
			Message: message,
		},
	})
}
