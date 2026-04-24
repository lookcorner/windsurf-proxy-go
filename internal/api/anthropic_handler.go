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
	"windsurf-proxy-go/internal/redact"
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

	body, r, entry := startAudit("anthropic", w, r)

	apiKey := h.validateAnthropicAuth(w, r)
	if apiKey == "" {
		h.recordRequest("", "", "", false, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, 0, 0, "invalid or missing API key")
		entry.Finish(http.StatusUnauthorized, fmt.Errorf("invalid or missing API key"))
		return
	}

	var req AnthropicMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.recordRequest("", "", "", false, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, 0, 0, "invalid request body")
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
		h.recordRequest(req.Model, "", "", req.Stream, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, 0, 0, "model not supported")
		writeAnthropicError(w, http.StatusBadRequest,
			"invalid_request_error",
			fmt.Sprintf("Model '%s' not supported", req.Model))
		entry.Finish(http.StatusBadRequest, fmt.Errorf("model %q not supported", req.Model))
		return
	}

	if !h.keys.IsModelAllowed(apiKey, internalModel) {
		h.recordRequest(req.Model, "", "", req.Stream, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, 0, 0, "model not allowed for this key")
		writeAnthropicError(w, http.StatusForbidden,
			"permission_error",
			fmt.Sprintf("Model '%s' not allowed for this key", req.Model))
		entry.Finish(http.StatusForbidden, fmt.Errorf("model not allowed for this key"))
		return
	}

	if !h.keys.CheckRateLimit(apiKey) {
		h.recordRequest(req.Model, "", "", req.Stream, "error",
			int(time.Since(t0).Milliseconds()), 0, 0, 0, 0, 0, "rate limit exceeded")
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
	requestMetrics := anthropicRequestMetrics(body, &req, messagesRaw, toolsList)

	resolved := core.ResolveModel(internalModel)
	cacheKey := makeResponseCacheKey("anthropic", apiKey, body)

	hasTools := tool_adapter.HasToolUsage(messagesRaw, toolsList)
	messages := convertMessages(messagesRaw)
	if hasTools {
		messages = tool_adapter.BuildToolPrompt(toolsList, messagesRaw, decodeToolChoice(req.ToolChoice))
	}

	log.Printf("[Anthropic] Request: model=%s (internal=%s) stream=%v messages=%d tools=%d hasTools=%v",
		req.Model, internalModel, req.Stream, len(messages), len(toolsList), hasTools)
	log.Printf("[Anthropic] Request metrics: body_bytes=%d raw_system_bytes=%d flattened_system_bytes=%d final_system_bytes=%d non_system_bytes=%d tool_schema_bytes=%d",
		requestMetrics.BodyBytes,
		requestMetrics.RawSystemBytes,
		requestMetrics.FlattenedSystemBytes,
		requestMetrics.FinalSystemBytes,
		requestMetrics.NonSystemBytes,
		requestMetrics.ToolSchemaBytes,
	)

	if req.Stream {
		h.handleAnthropicStream(w, r, messages, resolved, req.Model, cacheKey, hasTools)
	} else {
		h.handleAnthropicNonStream(w, r, messages, resolved, req.Model, cacheKey, hasTools)
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
	cacheKey string,
	toolAdapted bool,
) {
	w.Header().Set("Content-Type", "application/json")

	entry := audit.FromContext(r.Context())

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	attempts := h.retryAttempts()
	triedRoutes := make(map[string]bool)
	triedAccounts := make(map[string]bool)
	var lastError string
	var lastInstance string
	var lastAccount string
	t0 := time.Now()
	promptTokens := tokenizer.CountMessagesTokens(messages, displayModel)
	turns, promptChars := conversationDiagnostics(messages)
	if cachedText, ok := getCachedResponse(cacheKey); ok {
		completionTokens := tokenizer.CountTextTokens(cachedText, displayModel)
		resp := AnthropicMessagesResponse{
			ID:    "msg_" + generateID(),
			Type:  "message",
			Role:  "assistant",
			Model: displayModel,
			Content: []AnthropicOutBlock{{
				Type: "text",
				Text: cachedText,
			}},
			Usage: AnthropicUsage{
				InputTokens:  promptTokens,
				OutputTokens: completionTokens,
			},
			StopReason: "end_turn",
		}
		respBytes, _ := json.Marshal(&resp)
		_, _ = w.Write(respBytes)
		entry.SetResponseBody(respBytes)
		h.recordRequest(displayModel, "", "", false, "ok", int(time.Since(t0).Milliseconds()), turns, promptChars, promptTokens, completionTokens, promptTokens+completionTokens, "")
		entry.Finish(http.StatusOK, nil)
		return
	}

	for attempt := 0; attempt < attempts; attempt++ {
		target, err := h.selectRequestTarget(ctx, messages, triedRoutes, triedAccounts, resolved.ModelName)
		if err != nil {
			if lastError == "" {
				lastError = err.Error()
			}
			status := finalErrorStatus(lastError)
			h.recordRequest(displayModel, lastInstance, lastAccount, false, "error",
				int(time.Since(t0).Milliseconds()), turns, promptChars, promptTokens, 0, promptTokens, lastError)
			writeAnthropicError(w, status,
				anthropicErrorType(status),
				fmt.Sprintf("All instances failed: %s", lastError))
			entry.Finish(status, fmt.Errorf("all instances failed: %s", lastError))
			return
		}

		triedRoutes[target.routeKey()] = true
		lastInstance = target.instanceName()
		lastAccount = target.accountLabel()
		entry.SetUpstreamTarget(fmt.Sprintf("%s:%d", target.Instance.Host, target.Instance.Port))

		stream, err := target.Instance.Client.ChatStream(
			ctx,
			target.Instance.Name,
			target.AccountID,
			target.APIKey,
			messages,
			resolved.EnumValue,
			resolved.ModelName,
			target.Instance.Version,
		)
		if err != nil {
			if retry, handled := h.recoverLocalInstanceAuth(target, err, triedRoutes); handled {
				lastError = err.Error()
				log.Printf("[Anthropic] Instance '%s' auth failed (attempt %d/%d): %v",
					target.Instance.Name, attempt+1, attempts, err)
				if retry {
					continue
				}
				if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
					lastError = ctx.Err().Error()
					break
				}
				continue
			}
			rememberFailedAccount(triedAccounts, target)
			h.markTargetError(target, err, resolved.ModelName)
			h.releaseTarget(target)
			lastError = err.Error()
			log.Printf("[Anthropic] Instance '%s' failed (attempt %d/%d): %v",
				target.Instance.Name, attempt+1, attempts, err)
			if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
				lastError = ctx.Err().Error()
				break
			}
			continue
		}

		var (
			contentParts []string
			toolCalls    []protobuf.CascadeToolCall
			streamErr    error
		)
		for event := range stream {
			if streamErr = streamEventError(event.Type, event.Data); streamErr != nil {
				break
			}
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
		if streamErr != nil {
			rememberFailedAccount(triedAccounts, target)
			h.markTargetError(target, streamErr, resolved.ModelName)
			h.releaseTarget(target)
			lastError = streamErr.Error()
			log.Printf("[Anthropic] Instance '%s' stream failed (attempt %d/%d): %v",
				target.Instance.Name, attempt+1, attempts, streamErr)
			if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
				lastError = ctx.Err().Error()
				break
			}
			continue
		}

		h.markTargetSuccess(target)
		h.releaseTarget(target)

		fullContent := strings.Join(contentParts, "")
		adaptedCalls := []tool_adapter.ToolCall{}
		if toolAdapted && len(toolCalls) == 0 {
			fullContent, adaptedCalls = normalizeToolAdapterOutput(fullContent, true)
		} else {
			fullContent = redact.SanitizeText(fullContent)
		}
		completionTokens := tokenizer.CountTextTokens(fullContent, displayModel)

		blocks := make([]AnthropicOutBlock, 0, 1+len(toolCalls))
		if fullContent != "" {
			blocks = append(blocks, AnthropicOutBlock{
				Type: "text",
				Text: fullContent,
			})
		}
		for i, tc := range toolCalls {
			sanitizedArgs := redact.SanitizeText(tc.Arguments)
			var input map[string]interface{}
			if sanitizedArgs != "" {
				_ = json.Unmarshal([]byte(sanitizedArgs), &input)
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
			completionTokens += tokenizer.CountTextTokens(sanitizedArgs, displayModel)
		}
		if len(toolCalls) == 0 {
			for i, tc := range adaptedCalls {
				args := tc.Arguments
				var input map[string]interface{}
				if args != "" {
					_ = json.Unmarshal([]byte(args), &input)
				}
				if input == nil {
					input = map[string]interface{}{}
				}
				id := tc.ID
				if id == "" {
					id = fmt.Sprintf("toolu_%s_%d", uuid.New().String()[:8], i)
				}
				blocks = append(blocks, AnthropicOutBlock{
					Type:  "tool_use",
					ID:    id,
					Name:  tc.Name,
					Input: input,
				})
				completionTokens += tokenizer.CountTextTokens(args, displayModel)
			}
		}

		// At least one block is required by the Anthropic schema.
		if len(blocks) == 0 {
			blocks = append(blocks, AnthropicOutBlock{Type: "text", Text: ""})
		}

		stopReason := "end_turn"
		if len(toolCalls) > 0 || len(adaptedCalls) > 0 {
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
		h.recordRequest(displayModel, target.Instance.Name, target.accountLabel(), false, "ok",
			int(duration), turns, promptChars, promptTokens, completionTokens, total, "")

		respBytes, _ := json.Marshal(&resp)
		_, _ = w.Write(respBytes)
		entry.SetResponseBody(respBytes)
		if len(toolCalls) == 0 && len(adaptedCalls) == 0 {
			putCachedResponse(cacheKey, fullContent)
		}
		log.Printf("[Anthropic] Complete: model=%s tokens=%d duration=%dms",
			displayModel, total, duration)
		entry.Finish(http.StatusOK, nil)
		return
	}

	if lastError == "" {
		lastError = "request failed"
	}
	status := finalErrorStatus(lastError)
	h.recordRequest(displayModel, lastInstance, lastAccount, false, "error",
		int(time.Since(t0).Milliseconds()), turns, promptChars, promptTokens, 0, promptTokens, lastError)
	writeAnthropicError(w, status,
		anthropicErrorType(status),
		fmt.Sprintf("All instances failed after %d attempts: %s", attempts, lastError))
	entry.Finish(status, fmt.Errorf("all instances failed: %s", lastError))
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
	cacheKey string,
	toolAdapted bool,
) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	entry := audit.FromContext(r.Context())
	sse := NewAnthropicSSEWriter(w)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	attempts := h.retryAttempts()
	triedRoutes := make(map[string]bool)
	triedAccounts := make(map[string]bool)
	var lastError string
	var lastInstance string
	var lastAccount string
	t0 := time.Now()
	promptTokens := tokenizer.CountMessagesTokens(messages, displayModel)
	turns, promptChars := conversationDiagnostics(messages)

	messageID := "msg_" + generateID()
	if cachedText, ok := getCachedResponse(cacheKey); ok {
		completionTokens := tokenizer.CountTextTokens(cachedText, displayModel)
		_ = sse.WriteMessageStart(messageID, displayModel, promptTokens)
		_ = sse.WriteBlockStart(0, AnthropicOutBlock{Type: "text", Text: ""})
		if cachedText != "" {
			_ = sse.WriteTextDelta(0, cachedText)
			entry.AppendStreamText(cachedText)
		}
		_ = sse.WriteBlockStop(0)
		_ = sse.WriteMessageDelta("end_turn", completionTokens)
		_ = sse.WriteMessageStop()
		h.recordRequest(displayModel, "", "", true, "ok", int(time.Since(t0).Milliseconds()), turns, promptChars, promptTokens, completionTokens, promptTokens+completionTokens, "")
		entry.Finish(http.StatusOK, nil)
		return
	}

	for attempt := 0; attempt < attempts; attempt++ {
		target, err := h.selectRequestTarget(ctx, messages, triedRoutes, triedAccounts, resolved.ModelName)
		if err != nil {
			if lastError == "" {
				lastError = err.Error()
			}
			status := finalErrorStatus(lastError)
			h.recordRequest(displayModel, lastInstance, lastAccount, true, "error",
				int(time.Since(t0).Milliseconds()), turns, promptChars, promptTokens, 0, promptTokens, lastError)
			sse.WriteError(anthropicErrorType(status),
				fmt.Sprintf("All instances failed: %s", redact.SanitizeText(lastError)))
			entry.Finish(status, fmt.Errorf("all instances failed: %s", lastError))
			return
		}

		triedRoutes[target.routeKey()] = true
		lastInstance = target.instanceName()
		lastAccount = target.accountLabel()
		entry.SetUpstreamTarget(fmt.Sprintf("%s:%d", target.Instance.Host, target.Instance.Port))

		stream, err := target.Instance.Client.ChatStream(
			ctx,
			target.Instance.Name,
			target.AccountID,
			target.APIKey,
			messages,
			resolved.EnumValue,
			resolved.ModelName,
			target.Instance.Version,
		)
		if err != nil {
			if retry, handled := h.recoverLocalInstanceAuth(target, err, triedRoutes); handled {
				lastError = err.Error()
				log.Printf("[Anthropic] Instance '%s' auth failed (attempt %d/%d): %v",
					target.Instance.Name, attempt+1, attempts, err)
				if retry {
					continue
				}
				if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
					lastError = ctx.Err().Error()
					break
				}
				continue
			}
			rememberFailedAccount(triedAccounts, target)
			h.markTargetError(target, err, resolved.ModelName)
			h.releaseTarget(target)
			lastError = err.Error()
			log.Printf("[Anthropic] Instance '%s' failed (attempt %d/%d): %v",
				target.Instance.Name, attempt+1, attempts, err)
			if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
				lastError = ctx.Err().Error()
				break
			}
			continue
		}

		var (
			contentParts        []string
			toolCalls           []protobuf.CascadeToolCall
			adaptedStreamCalls  []tool_adapter.ToolCall
			rawAdaptedParts     []string
			completionTokens    int
			streamErr           error
			textBlockOpen       bool
			nextBlockIndex      = 0
			textBlockIndex      = -1
			hasToolCall         bool
			lastToolBlockIdx    = -1
			textSanitizer       = redact.NewPathSanitizer()
			adaptedStreamParser = tool_adapter.NewStructuredResponseStreamParser()
			messageStarted      bool
		)
		ensureMessageStarted := func() {
			if messageStarted {
				return
			}
			_ = sse.WriteMessageStart(messageID, displayModel, promptTokens)
			messageStarted = true
		}

		emitText := func(text string) {
			clean := textSanitizer.Feed(text)
			if clean == "" {
				return
			}
			if !textBlockOpen {
				ensureMessageStarted()
				textBlockIndex = nextBlockIndex
				nextBlockIndex++
				_ = sse.WriteBlockStart(textBlockIndex, AnthropicOutBlock{
					Type: "text", Text: "",
				})
				textBlockOpen = true
			}
			_ = sse.WriteTextDelta(textBlockIndex, clean)
			contentParts = append(contentParts, clean)
			entry.AppendStreamText(clean)
		}
		emitAdaptedToolCall := func(tc tool_adapter.ToolCall) {
			if textBlockOpen {
				_ = sse.WriteBlockStop(textBlockIndex)
				textBlockOpen = false
			}
			idx := nextBlockIndex
			nextBlockIndex++
			lastToolBlockIdx = idx
			toolUseID := tc.ID
			if toolUseID == "" {
				toolUseID = fmt.Sprintf("toolu_%s_%d", uuid.New().String()[:8], idx)
			}
			args := tc.Arguments
			if args == "" || args == "null" {
				args = "{}"
			}
			ensureMessageStarted()
			_ = sse.WriteBlockStart(idx, AnthropicOutBlock{
				Type:  "tool_use",
				ID:    toolUseID,
				Name:  tc.Name,
				Input: map[string]interface{}{},
			})
			_ = sse.WriteInputJSONDelta(idx, args)
			_ = sse.WriteBlockStop(idx)
			entry.AppendStreamText(fmt.Sprintf("\n[tool_use %s %s]", tc.Name, args))
			adaptedStreamCalls = append(adaptedStreamCalls, tc)
			hasToolCall = true
		}

		pingTicker := time.NewTicker(15 * time.Second)
		defer pingTicker.Stop()

	streamLoop:
		for {
			select {
			case <-pingTicker.C:
				if messageStarted {
					_ = sse.WritePing()
				}
			case event, ok := <-stream:
				if !ok {
					break streamLoop
				}
				if streamErr = streamEventError(event.Type, event.Data); streamErr != nil {
					break streamLoop
				}
				switch event.Type {
				case "text":
					text, ok := event.Data.(string)
					if !ok || text == "" {
						continue
					}
					if toolAdapted {
						rawAdaptedParts = append(rawAdaptedParts, text)
						streamText, streamCalls := adaptedStreamParser.Feed(text)
						if streamText != "" {
							emitText(streamText)
						}
						for _, tc := range streamCalls {
							emitAdaptedToolCall(tc)
						}
						continue
					}
					emitText(text)

				case "tool_call":
					tc, ok := event.Data.(protobuf.CascadeToolCall)
					if !ok {
						continue
					}
					if toolAdapted {
						toolCalls = append(toolCalls, tc)
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
					args := redact.SanitizeText(tc.Arguments)

					_ = sse.WriteBlockStart(idx, AnthropicOutBlock{
						Type:  "tool_use",
						ID:    toolUseID,
						Name:  tc.Name,
						Input: map[string]interface{}{},
					})
					if args == "" || args == "null" {
						args = "{}"
					}
					_ = sse.WriteInputJSONDelta(idx, args)
					_ = sse.WriteBlockStop(idx)
					entry.AppendStreamText(fmt.Sprintf("\n[tool_use %s %s]", tc.Name, args))

					completionTokens += tokenizer.CountTextTokens(args, displayModel)
					hasToolCall = true
				}
			}
		}
		if streamErr != nil {
			rememberFailedAccount(triedAccounts, target)
			h.markTargetError(target, streamErr, resolved.ModelName)
			h.releaseTarget(target)
			lastError = streamErr.Error()
			log.Printf("[Anthropic] Instance '%s' stream failed (attempt %d/%d): %v",
				target.Instance.Name, attempt+1, attempts, streamErr)
			if messageStarted {
				sse.WriteError("api_error", redact.SanitizeText(lastError))
				entry.Finish(http.StatusBadGateway, streamErr)
				return
			}
			if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
				lastError = ctx.Err().Error()
				break
			}
			continue
		}
		if tail := textSanitizer.Flush(); tail != "" {
			if !textBlockOpen {
				ensureMessageStarted()
				textBlockIndex = nextBlockIndex
				nextBlockIndex++
				_ = sse.WriteBlockStart(textBlockIndex, AnthropicOutBlock{Type: "text", Text: ""})
				textBlockOpen = true
			}
			_ = sse.WriteTextDelta(textBlockIndex, tail)
			contentParts = append(contentParts, tail)
			entry.AppendStreamText(tail)
		}

		if textBlockOpen {
			_ = sse.WriteBlockStop(textBlockIndex)
			textBlockOpen = false
		}
		_ = lastToolBlockIdx // currently unused but reserved for follow-up deltas

		h.markTargetSuccess(target)
		h.releaseTarget(target)

		fullContent := strings.Join(contentParts, "")
		adaptedCalls := adaptedStreamCalls
		if toolAdapted {
			rawContent := strings.Join(rawAdaptedParts, "")
			if len(toolCalls) == 0 {
				normalizedContent, normalizedCalls := normalizeToolAdapterOutput(rawContent, true)
				emittedContent := fullContent
				switch {
				case emittedContent == "":
					if normalizedContent != "" {
						ensureMessageStarted()
						idx := nextBlockIndex
						nextBlockIndex++
						_ = sse.WriteBlockStart(idx, AnthropicOutBlock{Type: "text", Text: ""})
						_ = sse.WriteTextDelta(idx, normalizedContent)
						_ = sse.WriteBlockStop(idx)
						entry.AppendStreamText(normalizedContent)
						fullContent = normalizedContent
					}
				case strings.HasPrefix(normalizedContent, emittedContent):
					tail := normalizedContent[len(emittedContent):]
					if tail != "" {
						ensureMessageStarted()
						idx := nextBlockIndex
						nextBlockIndex++
						_ = sse.WriteBlockStart(idx, AnthropicOutBlock{Type: "text", Text: ""})
						_ = sse.WriteTextDelta(idx, tail)
						_ = sse.WriteBlockStop(idx)
						entry.AppendStreamText(tail)
						fullContent = normalizedContent
					} else {
						fullContent = normalizedContent
					}
				case normalizedContent != emittedContent:
					fullContent = normalizedContent
				}
				for _, tc := range normalizedCalls[len(adaptedStreamCalls):] {
					emitAdaptedToolCall(tc)
				}
				adaptedCalls = normalizedCalls
			} else {
				fullContent = redact.SanitizeText(fullContent)
			}
			if len(toolCalls) > 0 {
				for idx, tc := range toolCalls {
					args := redact.SanitizeText(tc.Arguments)
					completionTokens += tokenizer.CountTextTokens(args, displayModel)
					entry.AppendStreamText(fmt.Sprintf("\n[tool_use %s %s]", tc.Name, args))
					blockIdx := nextBlockIndex + idx
					toolUseID := fmt.Sprintf("toolu_%s_%d", uuid.New().String()[:8], blockIdx)
					ensureMessageStarted()
					_ = sse.WriteBlockStart(blockIdx, AnthropicOutBlock{
						Type:  "tool_use",
						ID:    toolUseID,
						Name:  tc.Name,
						Input: map[string]interface{}{},
					})
					if args == "" || args == "null" {
						args = "{}"
					}
					_ = sse.WriteInputJSONDelta(blockIdx, args)
					_ = sse.WriteBlockStop(blockIdx)
				}
				nextBlockIndex += len(toolCalls)
				hasToolCall = len(toolCalls) > 0
			}
		}
		completionTokens += tokenizer.CountTextTokens(fullContent, displayModel)
		for _, tc := range adaptedCalls {
			completionTokens += tokenizer.CountTextTokens(tc.Arguments, displayModel)
		}

		stopReason := "end_turn"
		if hasToolCall {
			stopReason = "tool_use"
		}

		ensureMessageStarted()
		_ = sse.WriteMessageDelta(stopReason, completionTokens)
		_ = sse.WriteMessageStop()
		if !hasToolCall {
			putCachedResponse(cacheKey, fullContent)
		}

		duration := time.Since(t0).Milliseconds()
		total := promptTokens + completionTokens
		h.recordRequest(displayModel, target.Instance.Name, target.accountLabel(), true, "ok",
			int(duration), turns, promptChars, promptTokens, completionTokens, total, "")
		log.Printf("[Anthropic] Stream complete: model=%s tokens=%d duration=%dms",
			displayModel, total, duration)
		entry.Finish(http.StatusOK, nil)
		return
	}

	if lastError == "" {
		lastError = "request failed"
	}
	status := finalErrorStatus(lastError)
	h.recordRequest(displayModel, lastInstance, lastAccount, true, "error",
		int(time.Since(t0).Milliseconds()), turns, promptChars, promptTokens, 0, promptTokens, lastError)
	sse.WriteError(anthropicErrorType(status),
		fmt.Sprintf("All instances failed after %d attempts: %s", attempts, redact.SanitizeText(lastError)))
	entry.Finish(status, fmt.Errorf("all instances failed: %s", lastError))
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
			Message: redact.SanitizeText(message),
		},
	})
}
