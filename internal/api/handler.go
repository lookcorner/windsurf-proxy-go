// Package api provides OpenAI-compatible REST API endpoints.
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
	"windsurf-proxy-go/internal/balancer"
	"windsurf-proxy-go/internal/config"
	"windsurf-proxy-go/internal/core"
	"windsurf-proxy-go/internal/core/protobuf"
	"windsurf-proxy-go/internal/keys"
	"windsurf-proxy-go/internal/tokenizer"
	"windsurf-proxy-go/internal/tool_adapter"

	"github.com/google/uuid"
)

// Handler holds the API handlers.
type Handler struct {
	balancer        *balancer.LoadBalancer
	keys            *keys.Manager
	config          *config.Config
	requestRecorder RequestRecorder
}

// RequestRecorder records request metrics for the management UI.
type RequestRecorder func(
	model, instance string,
	stream bool,
	status string,
	durationMs, promptTokens, completionTokens, totalTokens int,
	err string,
)

// NewHandler creates a new API handler.
func NewHandler(bal *balancer.LoadBalancer, km *keys.Manager, cfg *config.Config, recorder RequestRecorder) *Handler {
	return &Handler{
		balancer:        bal,
		keys:            km,
		config:          cfg,
		requestRecorder: recorder,
	}
}

// RegisterRoutes registers API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("/v1/messages", h.handleAnthropicMessages)
	mux.HandleFunc("/v1/models", h.handleModels)
	mux.HandleFunc("/v1/models/", h.handleModel)
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/", h.handleNotFound)
}

// handleChatCompletions handles POST /v1/chat/completions.
func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	t0 := time.Now()

	_, r, entry := startAudit("openai", w, r)

	// Validate API key
	apiKey := h.validateAuth(w, r)
	if apiKey == "" {
		h.recordRequest("", "", false, "error", int(time.Since(t0).Milliseconds()), 0, 0, 0, "invalid or missing API key")
		entry.Finish(http.StatusUnauthorized, fmt.Errorf("invalid or missing API key"))
		return
	}

	// Parse request
	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.recordRequest("", "", false, "error", int(time.Since(t0).Milliseconds()), 0, 0, 0, "invalid request body")
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request_error")
		entry.Finish(http.StatusBadRequest, err)
		return
	}
	entry.SetModel(req.Model, req.Model)
	entry.SetStream(req.Stream)

	// Validate model
	if !core.IsModelSupported(req.Model) {
		h.recordRequest(req.Model, "", req.Stream, "error", int(time.Since(t0).Milliseconds()), 0, 0, 0, "model not supported")
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("Model '%s' not supported. Use GET /v1/models for the list.", req.Model),
			"invalid_request_error")
		entry.Finish(http.StatusBadRequest, fmt.Errorf("model %q not supported", req.Model))
		return
	}

	// Check model allowed for this key
	if !h.keys.IsModelAllowed(apiKey, req.Model) {
		h.recordRequest(req.Model, "", req.Stream, "error", int(time.Since(t0).Milliseconds()), 0, 0, 0, "model not allowed for this key")
		writeError(w, http.StatusForbidden,
			fmt.Sprintf("Model '%s' not allowed for this key", req.Model),
			"invalid_request_error")
		entry.Finish(http.StatusForbidden, fmt.Errorf("model not allowed for this key"))
		return
	}

	// Check rate limit
	if !h.keys.CheckRateLimit(apiKey) {
		h.recordRequest(req.Model, "", req.Stream, "error", int(time.Since(t0).Milliseconds()), 0, 0, 0, "rate limit exceeded")
		writeError(w, http.StatusTooManyRequests, "Rate limit exceeded", "rate_limit_error")
		entry.Finish(http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded"))
		return
	}

	// Resolve model
	resolved := core.ResolveModel(req.Model)

	// Convert tools to map format
	toolsList := []map[string]interface{}{}
	for _, t := range req.Tools {
		toolMap := map[string]interface{}{
			"type": t.Type,
		}
		if t.Function != nil {
			toolMap["function"] = map[string]interface{}{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			}
		}
		toolsList = append(toolsList, toolMap)
	}

	// Convert messages to map format
	messagesRaw := []map[string]interface{}{}
	for _, m := range req.Messages {
		msgMap := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		if m.Name != "" {
			msgMap["name"] = m.Name
		}
		if m.ToolCallID != "" {
			msgMap["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			tcList := []map[string]interface{}{}
			for _, tc := range m.ToolCalls {
				tcList = append(tcList, map[string]interface{}{
					"id":   tc.ID,
					"type": tc.Type,
					"function": map[string]interface{}{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				})
			}
			msgMap["tool_calls"] = tcList
		}
		messagesRaw = append(messagesRaw, msgMap)
	}

	// Check if tool calling is involved
	hasTools := tool_adapter.HasToolUsage(messagesRaw, toolsList)

	// Build tool-adapted messages if needed
	messages := convertMessages(messagesRaw)
	if hasTools {
		messages = tool_adapter.BuildToolPrompt(toolsList, messagesRaw)
	}

	log.Printf("[API] Request: model=%s stream=%v messages=%d tools=%d hasTools=%v",
		req.Model, req.Stream, len(messages), len(toolsList), hasTools)

	// Handle request with retry
	if req.Stream {
		h.handleStreamWithRetry(w, r, messages, resolved, req.Model, toolsList)
	} else {
		h.handleNonStreamWithRetry(w, r, messages, resolved, req.Model, toolsList)
	}
}

// handleStreamWithRetry handles streaming with retry logic.
func (h *Handler) handleStreamWithRetry(w http.ResponseWriter, r *http.Request,
	messages []map[string]string, resolved core.ResolvedModel, displayModel string, toolsList []map[string]interface{}) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	entry := audit.FromContext(r.Context())
	sse := NewSSEWriter(w)

	// First chunk: role
	first := NewStreamChunk(displayModel, DeltaContent{Role: "assistant"}, "")
	sse.WriteJSON(first)

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
			// No more healthy instances
			if lastError == "" {
				lastError = err.Error()
			}
			h.recordRequest(displayModel, lastInstance, true, "error", int(time.Since(t0).Milliseconds()), promptTokens, 0, promptTokens, lastError)
			sse.WriteError(fmt.Sprintf("All instances failed: %s", lastError), "server_error")
			sse.WriteDone()
			entry.Finish(http.StatusServiceUnavailable, fmt.Errorf("all instances failed: %s", lastError))
			return
		}

		tried[inst.Name] = true
		lastInstance = inst.Name
		entry.SetUpstreamTarget(fmt.Sprintf("%s:%d", inst.Host, inst.Port))

		stream, err := inst.Client.ChatStream(ctx, inst.APIKey, messages, resolved.EnumValue, resolved.ModelName, inst.Version)
		if err != nil {
			h.balancer.MarkError(inst, err.Error())
			h.balancer.ReleaseInstance(inst)
			lastError = err.Error()
			log.Printf("[API] Instance '%s' failed (attempt %d/%d): %v", inst.Name, attempt+1, attempts, err)
			if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
				lastError = ctx.Err().Error()
				break
			}
			continue
		}

		// Process stream
		contentParts := make([]string, 0)
		toolCalls := make([]protobuf.CascadeToolCall, 0)
		toolCallIndex := 0

		for event := range stream {
			if event.Type == "text" {
				if text, ok := event.Data.(string); ok {
					contentParts = append(contentParts, text)
					entry.AppendStreamText(text)
					chunk := NewStreamChunk(displayModel, DeltaContent{Content: text}, "")
					sse.WriteJSON(chunk)
				}
			} else if event.Type == "tool_call" {
				if tc, ok := event.Data.(protobuf.CascadeToolCall); ok {
					toolCalls = append(toolCalls, tc)

					// Map Cascade tool to OpenAI-compatible tool
					mappedName, mappedArgs := mapCascadeTool(tc.Name, tc.Arguments)

					// Emit tool_call delta
					dt := DeltaToolCall{
						Index: toolCallIndex,
						ID:    fmt.Sprintf("call_%s_%d", uuid.New().String()[:8], toolCallIndex),
						Type:  "function",
						Function: FunctionCallInfo{
							Name:      mappedName,
							Arguments: mappedArgs,
						},
					}
					toolCallIndex++

					delta := DeltaContent{ToolCalls: []DeltaToolCall{dt}}
					chunk := NewStreamChunk(displayModel, delta, "")
					sse.WriteJSON(chunk)
					entry.AppendStreamText(fmt.Sprintf("\n[tool_call %s %s]", mappedName, mappedArgs))

					log.Printf("[API] Tool call: %s -> %s", tc.Name, mappedName)
				}
			} else if event.Type == "tool_result" {
				if tr, ok := event.Data.(protobuf.CascadeToolResult); ok {
					// Tool results are not sent in stream, just logged
					log.Printf("[API] Tool result: %s", tr.ToolURI)
				}
			}
		}

		h.balancer.MarkSuccess(inst)
		h.balancer.ReleaseInstance(inst)

		// Calculate usage
		fullContent := strings.Join(contentParts, "")
		completionTokens := tokenizer.CountTextTokens(fullContent, displayModel)

		// Add tool call tokens
		for _, tc := range toolCalls {
			completionTokens += tokenizer.CountTextTokens(tc.Arguments, displayModel)
		}

		usage := &UsageInfo{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		}
		usageChunk := &StreamChunk{
			ID:      "chatcmpl-" + generateID(),
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   displayModel,
			Usage:   usage,
		}
		sse.WriteJSON(usageChunk)

		// Final chunk
		finishReason := "stop"
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		}
		done := NewStreamChunk(displayModel, DeltaContent{}, finishReason)
		sse.WriteJSON(done)
		sse.WriteDone()

		duration := time.Since(t0).Milliseconds()
		h.recordRequest(displayModel, inst.Name, true, "ok", int(duration), promptTokens, completionTokens, usage.TotalTokens, "")
		log.Printf("[API] Stream complete: model=%s tokens=%d duration=%dms", displayModel, usage.TotalTokens, duration)
		entry.Finish(http.StatusOK, nil)
		return
	}

	// All retries exhausted
	if lastError == "" {
		lastError = "request failed"
	}
	h.recordRequest(displayModel, lastInstance, true, "error", int(time.Since(t0).Milliseconds()), promptTokens, 0, promptTokens, lastError)
	sse.WriteError(fmt.Sprintf("All instances failed after %d retries: %s", maxRetries, lastError), "server_error")
	sse.WriteDone()
	entry.Finish(http.StatusServiceUnavailable, fmt.Errorf("all instances failed: %s", lastError))
}

// handleNonStreamWithRetry handles non-streaming with retry logic.
func (h *Handler) handleNonStreamWithRetry(w http.ResponseWriter, r *http.Request,
	messages []map[string]string, resolved core.ResolvedModel, displayModel string, toolsList []map[string]interface{}) {
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
			h.recordRequest(displayModel, lastInstance, false, "error", int(time.Since(t0).Milliseconds()), promptTokens, 0, promptTokens, lastError)
			writeError(w, http.StatusServiceUnavailable,
				fmt.Sprintf("All instances failed: %s", lastError), "server_error")
			entry.Finish(http.StatusServiceUnavailable, fmt.Errorf("all instances failed: %s", lastError))
			return
		}

		tried[inst.Name] = true
		lastInstance = inst.Name
		entry.SetUpstreamTarget(fmt.Sprintf("%s:%d", inst.Host, inst.Port))

		stream, err := inst.Client.ChatStream(ctx, inst.APIKey, messages, resolved.EnumValue, resolved.ModelName, inst.Version)
		if err != nil {
			h.balancer.MarkError(inst, err.Error())
			h.balancer.ReleaseInstance(inst)
			lastError = err.Error()
			log.Printf("[API] Instance '%s' failed (attempt %d/%d): %v", inst.Name, attempt+1, attempts, err)
			if attempt+1 < attempts && !waitRetryDelay(ctx, h.config.Balancing.RetryDelay) {
				lastError = ctx.Err().Error()
				break
			}
			continue
		}

		// Collect all content
		contentParts := make([]string, 0)
		toolCalls := make([]protobuf.CascadeToolCall, 0)

		for event := range stream {
			if event.Type == "text" {
				if text, ok := event.Data.(string); ok {
					contentParts = append(contentParts, text)
				}
			} else if event.Type == "tool_call" {
				if tc, ok := event.Data.(protobuf.CascadeToolCall); ok {
					toolCalls = append(toolCalls, tc)
				}
			}
		}

		h.balancer.MarkSuccess(inst)
		h.balancer.ReleaseInstance(inst)

		// Build response
		fullContent := strings.Join(contentParts, "")
		completionTokens := tokenizer.CountTextTokens(fullContent, displayModel)

		// Add tool calls
		tcList := []AssistantToolCall{}
		for i, tc := range toolCalls {
			mappedName, mappedArgs := mapCascadeTool(tc.Name, tc.Arguments)
			tcList = append(tcList, AssistantToolCall{
				ID:   fmt.Sprintf("call_%s_%d", uuid.New().String()[:8], i),
				Type: "function",
				Function: FunctionCallInfo{
					Name:      mappedName,
					Arguments: mappedArgs,
				},
			})
			completionTokens += tokenizer.CountTextTokens(mappedArgs, displayModel)
		}

		finishReason := "stop"
		if len(tcList) > 0 {
			finishReason = "tool_calls"
		}

		resp := &ChatCompletionResponse{
			ID:      "chatcmpl-" + generateID(),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   displayModel,
			Choices: []Choice{
				{
					Index: 0,
					Message: MessageContent{
						Role:      "assistant",
						Content:   fullContent,
						ToolCalls: tcList,
					},
					FinishReason: finishReason,
				},
			},
			Usage: UsageInfo{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				TotalTokens:      promptTokens + completionTokens,
			},
		}

		h.recordRequest(displayModel, inst.Name, false, "ok", int(time.Since(t0).Milliseconds()), promptTokens, completionTokens, resp.Usage.TotalTokens, "")
		respBytes, _ := json.Marshal(resp)
		_, _ = w.Write(respBytes)
		entry.SetResponseBody(respBytes)
		duration := time.Since(t0).Milliseconds()
		log.Printf("[API] Complete: model=%s tokens=%d duration=%dms", displayModel, resp.Usage.TotalTokens, duration)
		entry.Finish(http.StatusOK, nil)
		return
	}

	if lastError == "" {
		lastError = "request failed"
	}
	h.recordRequest(displayModel, lastInstance, false, "error", int(time.Since(t0).Milliseconds()), promptTokens, 0, promptTokens, lastError)
	writeError(w, http.StatusServiceUnavailable,
		fmt.Sprintf("All instances failed after %d retries: %s", maxRetries, lastError), "server_error")
	entry.Finish(http.StatusServiceUnavailable, fmt.Errorf("all instances failed: %s", lastError))
}

// mapCascadeTool maps Cascade internal tool names to OpenAI-compatible equivalents.
func mapCascadeTool(cascadeName string, cascadeArgs string) (string, string) {
	var args map[string]interface{}
	if cascadeArgs != "" {
		json.Unmarshal([]byte(cascadeArgs), &args)
	}
	if args == nil {
		args = make(map[string]interface{})
	}

	// Cascade tool → OpenAI tool mapping
	switch cascadeName {
	case "read_file":
		if filePath, ok := args["file_path"].(string); ok {
			return "Read", mustJSON(map[string]interface{}{"file_path": filePath})
		}
		if path, ok := args["Path"].(string); ok {
			return "Read", mustJSON(map[string]interface{}{"file_path": path})
		}
	case "write_to_file":
		return "Write", mustJSON(map[string]interface{}{
			"file_path": args["TargetFile"],
			"content":   args["CodeContent"],
		})
	case "edit":
		return "Edit", mustJSON(map[string]interface{}{
			"file_path":  args["file_path"],
			"old_string": args["old_string"],
			"new_string": args["new_string"],
		})
	case "run_command":
		cmd, _ := args["CommandLine"].(string)
		if cmd == "" {
			cmd, _ = args["command"].(string)
		}
		return "Bash", mustJSON(map[string]interface{}{"command": cmd})
	case "list_dir":
		path, _ := args["DirectoryPath"].(string)
		if path == "" {
			path = "."
		}
		return "Bash", mustJSON(map[string]interface{}{"command": "ls -la " + path})
	case "grep_search":
		query, _ := args["Query"].(string)
		searchPath, _ := args["SearchPath"].(string)
		if searchPath == "" {
			searchPath = "."
		}
		return "Bash", mustJSON(map[string]interface{}{"command": fmt.Sprintf("grep -r '%s' %s", query, searchPath)})
	case "find_by_name":
		pattern, _ := args["Pattern"].(string)
		searchDir, _ := args["SearchDirectory"].(string)
		if searchDir == "" {
			searchDir = "."
		}
		return "Bash", mustJSON(map[string]interface{}{"command": fmt.Sprintf("find %s -name '%s'", searchDir, pattern)})
	}

	// Fallback: keep original
	return cascadeName, cascadeArgs
}

func mustJSON(v map[string]interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// handleModels handles GET /v1/models.
func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate API key
	if h.validateAuth(w, r) == "" {
		return
	}

	models := core.GetSupportedModels()
	data := make([]ModelInfo, 0)
	for _, m := range models {
		data = append(data, ModelInfo{
			ID:      m,
			Object:  "model",
			Created: 1700000000,
			OwnedBy: "windsurf",
		})
	}

	resp := ModelListResponse{
		Object: "list",
		Data:   data,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleModel handles GET /v1/models/{model_id}.
func (h *Handler) handleModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.validateAuth(w, r) == "" {
		return
	}

	modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	if !core.IsModelSupported(modelID) {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("Model '%s' not found", modelID),
			"invalid_request_error")
		return
	}

	resp := ModelInfo{
		ID:      modelID,
		Object:  "model",
		Created: 1700000000,
		OwnedBy: "windsurf",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleHealth handles GET /health.
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleNotFound handles 404.
func (h *Handler) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "Not found", "invalid_request_error")
}

// validateAuth validates Authorization header and returns API key.
func (h *Handler) validateAuth(w http.ResponseWriter, r *http.Request) string {
	auth := r.Header.Get("Authorization")
	entry := h.keys.Validate(auth)
	if entry == nil {
		writeError(w, http.StatusUnauthorized, "Invalid or missing API key", "invalid_request_error")
		return ""
	}
	return entry.Key
}

// Helper functions

func convertMessages(msgs []map[string]interface{}) []map[string]string {
	result := make([]map[string]string, 0)
	for _, m := range msgs {
		content := ""
		switch v := m["content"].(type) {
		case string:
			content = v
		case []interface{}:
			// Handle array content (for multimodal). The upstream gRPC
			// transport currently only carries text, so image_url parts are
			// reduced to a descriptive marker such as "[image: image/png,
			// 124 KB]" so the model at least knows an image was attached.
			var textParts strings.Builder
			for _, part := range v {
				p, ok := part.(map[string]interface{})
				if !ok {
					continue
				}
				switch p["type"] {
				case "text":
					if text, ok := p["text"].(string); ok {
						if textParts.Len() > 0 {
							textParts.WriteString("\n")
						}
						textParts.WriteString(text)
					}
				case "image_url":
					marker := describeOpenAIImagePart(p)
					if marker != "" {
						if textParts.Len() > 0 {
							textParts.WriteString("\n")
						}
						textParts.WriteString(marker)
					}
				}
			}
			content = textParts.String()
		}
		name, _ := m["name"].(string)
		role, ok := m["role"].(string)
		if !ok || role == "" {
			continue
		}
		result = append(result, map[string]string{
			"role":    role,
			"content": content,
			"name":    name,
		})
	}
	return result
}

// describeOpenAIImagePart renders an OpenAI-style image_url content part
// (either {"url": "https://..."} or {"url": "data:image/png;base64,..."}) as
// a compact textual marker. The caller inlines the marker into the prompt.
func describeOpenAIImagePart(part map[string]interface{}) string {
	iu, ok := part["image_url"]
	if !ok {
		return "[image]"
	}

	switch v := iu.(type) {
	case string:
		return describeImage("", "", v)
	case map[string]interface{}:
		urlStr, _ := v["url"].(string)
		return describeImage("", "", urlStr)
	default:
		return "[image]"
	}
}

func (h *Handler) recordRequest(
	model, instance string,
	stream bool,
	status string,
	durationMs, promptTokens, completionTokens, totalTokens int,
	err string,
) {
	if h.requestRecorder == nil {
		return
	}

	h.requestRecorder(model, instance, stream, status, durationMs, promptTokens, completionTokens, totalTokens, err)
}

func retryAttempts(maxRetries int) int {
	if maxRetries < 0 {
		return 1
	}
	return maxRetries + 1
}

func retryDelayDuration(retryDelay float64) time.Duration {
	if retryDelay <= 0 {
		return 0
	}
	return time.Duration(retryDelay * float64(time.Second))
}

func waitRetryDelay(ctx context.Context, retryDelay float64) bool {
	delay := retryDelayDuration(retryDelay)
	if delay <= 0 {
		return true
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func writeError(w http.ResponseWriter, code int, message string, errType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorDetail{
			Message: message,
			Type:    errType,
		},
	})
}
