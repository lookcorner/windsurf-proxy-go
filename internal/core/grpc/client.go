// Package grpc provides gRPC client for Windsurf language server.
// Uses raw protobuf bytes without .proto files.
package grpc

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"windsurf-proxy-go/internal/audit"
	"windsurf-proxy-go/internal/core"
	"windsurf-proxy-go/internal/core/protobuf"
)

const (
	GRPCService = "exa.language_server_pb.LanguageServerService"

	// Cascade polling config
	CascadePollInterval           = 150 * time.Millisecond
	CascadeMaxPolls               = 2000
	CascadeInitialWait            = 300 * time.Millisecond
	CascadeMaxAutoContinue        = 8
	CascadeSendBaseTimeout        = 60 * time.Second
	CascadeSendMaxTimeout         = 90 * time.Second
	CascadeSendTimeoutStep        = 10 * time.Second
	CascadeSendTimeoutStepBytes   = 8000
	CascadeStablePolls            = 8
	CascadeForcedStablePolls      = 40
	CascadePendingNativeToolPolls = 400

	CascadeContinuePrompt      = "Continue the current task using the existing conversation context and prior tool results. Do not repeat completed tool calls unless necessary."
	CascadeContinueIdleRetries = 12
	CascadeContinueIdleDelay   = 250 * time.Millisecond
)

// StreamEvent represents a stream event from Cascade.
type StreamEvent struct {
	Type string
	Data interface{}
}

type UserStatus = protobuf.UserStatusSnapshot

func cascadeSendTimeout(text string, systemPrompt string) time.Duration {
	promptBytes := len(text) + len(systemPrompt)
	timeout := CascadeSendBaseTimeout + time.Duration(promptBytes/CascadeSendTimeoutStepBytes)*CascadeSendTimeoutStep
	if timeout > CascadeSendMaxTimeout {
		return CascadeSendMaxTimeout
	}
	return timeout
}

func isCascadeExecutorNotIdle(err error) bool {
	return err != nil && strings.Contains(err.Error(), "executor is not idle")
}

// WindsurfGrpcClient is a gRPC client for Windsurf language server.
type WindsurfGrpcClient struct {
	Host      string
	Port      int
	CSRFToken string
	channel   *grpc.ClientConn
	mu        sync.Mutex
	panelMu   sync.Mutex
	panelInit map[string]string
}

// NewWindsurfGrpcClient creates a new gRPC client.
func NewWindsurfGrpcClient(host string, port int, csrfToken string) *WindsurfGrpcClient {
	return &WindsurfGrpcClient{
		Host:      host,
		Port:      port,
		CSRFToken: csrfToken,
		panelInit: make(map[string]string),
	}
}

// ensureChannel ensures the gRPC channel is connected.
func (c *WindsurfGrpcClient) ensureChannel() (*grpc.ClientConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.channel == nil {
		channel, err := grpc.Dial(
			fmt.Sprintf("%s:%d", c.Host, c.Port),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return nil, err
		}
		c.channel = channel
	}
	return c.channel, nil
}

// Close closes the gRPC connection.
func (c *WindsurfGrpcClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.channel != nil {
		return c.channel.Close()
	}
	return nil
}

// rawCall makes a raw gRPC unary call with hand-built protobuf bytes.
//
// Internally we install rawCodec via grpc.ForceCodec so the gRPC runtime
// itself handles HTTP/2 + gRPC frame headers (5-byte length prefix, trailers,
// etc.) and simply hands our Marshal/Unmarshal the raw payload. The protobuf
// package must therefore return *unframed* bytes — framing is the library's
// job, not the caller's.
func (c *WindsurfGrpcClient) rawCall(ctx context.Context, method string, req []byte) ([]byte, error) {
	channel, err := c.ensureChannel()
	if err != nil {
		return nil, err
	}

	ctx = c.withAuthMetadata(ctx)

	target := fmt.Sprintf("%s:%d", c.Host, c.Port)
	t0 := time.Now()

	var resp []byte
	err = channel.Invoke(
		ctx,
		"/"+GRPCService+"/"+method,
		req,
		&resp,
		grpc.ForceCodec(rawCodec{}),
	)

	audit.FromContext(ctx).RecordUpstreamCall(target, method, time.Since(t0).Milliseconds(), err)

	if err != nil {
		return nil, err
	}
	return resp, nil
}

// withAuthMetadata attaches the Windsurf CSRF token to outgoing gRPC
// metadata. language_server rejects requests without it with
// "Unauthenticated: missing CSRF token".
func (c *WindsurfGrpcClient) withAuthMetadata(ctx context.Context) context.Context {
	if c.CSRFToken == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "x-codeium-csrf-token", c.CSRFToken)
}

// CascadeStart starts a Cascade session and returns cascade_id.
//
// parentCtx is used as the parent of the gRPC timeout context so that
// audit data attached upstream (via audit.WithEntry) propagates into
// the gRPC call. Pass context.Background() if not threading audit
// state.
func (c *WindsurfGrpcClient) CascadeStart(parentCtx context.Context, apiKey string, version string) (string, error) {
	req := protobuf.BuildStartCascadeRequest(apiKey, version)

	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()

	resp, err := c.rawCall(ctx, "StartCascade", req)
	if err != nil {
		return "", err
	}

	cascadeID := protobuf.ParseStartCascadeResponse(resp)
	if cascadeID == "" {
		return "", fmt.Errorf("StartCascade returned no cascade_id")
	}

	return cascadeID, nil
}

// CascadeSend sends a user message into an existing Cascade session.
func (c *WindsurfGrpcClient) CascadeSend(
	parentCtx context.Context,
	cascadeID string,
	text string,
	modelEnum core.ModelEnum,
	modelUID string,
	apiKey string,
	version string,
	systemPrompt string,
) error {
	req := protobuf.BuildSendCascadeMessageRequest(
		cascadeID, text, int(modelEnum), modelUID, apiKey, version, systemPrompt,
	)

	timeout := cascadeSendTimeout(text, systemPrompt)
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	_, err := c.rawCall(ctx, "SendUserCascadeMessage", req)
	return err
}

// CascadePoll polls for trajectory steps.
func (c *WindsurfGrpcClient) CascadePoll(parentCtx context.Context, cascadeID string) (*protobuf.CascadeResult, error) {
	req := protobuf.BuildGetTrajectoryStepsRequest(cascadeID)

	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()

	resp, err := c.rawCall(ctx, "GetCascadeTrajectorySteps", req)
	if err != nil {
		return nil, err
	}

	result := protobuf.ParseTrajectorySteps(resp)
	return &result, nil
}

// GetUserStatus fetches plan/quota data for the api key currently routed
// through this LS transport.
func (c *WindsurfGrpcClient) GetUserStatus(parentCtx context.Context, apiKey string, version string) (*UserStatus, error) {
	req := protobuf.BuildGetUserStatusRequest(apiKey, version)

	ctx, cancel := context.WithTimeout(parentCtx, 8*time.Second)
	defer cancel()

	resp, err := c.rawCall(ctx, "GetUserStatus", req)
	if err != nil {
		return nil, err
	}

	status := protobuf.ParseGetUserStatusResponse(resp)
	return &status, nil
}

// ChatStream sends a chat request and yields StreamEvent tuples.
func (c *WindsurfGrpcClient) ChatStream(
	ctx context.Context,
	instanceName string,
	accountID string,
	apiKey string,
	messages []map[string]string,
	modelEnum core.ModelEnum,
	modelName string,
	version string,
) (<-chan StreamEvent, error) {
	out := make(chan StreamEvent, 100)

	modelUID := core.GetModelUID(modelName)
	log.Printf("[gRPC] ChatStream -> %s:%d model=%s uid=%s", c.Host, c.Port, modelName, modelUID)

	if modelUID != "" {
		go c.cascadeProducer(ctx, out, instanceName, accountID, apiKey, messages, modelEnum, modelUID, version)
	} else {
		log.Printf("No model UID for '%s', using RawGetChatMessage", modelName)
		go c.rawStreamProducer(ctx, out, apiKey, messages, modelEnum, modelName, version)
	}

	return out, nil
}

// cwdRegex matches working directory in system prompt
var cwdRegex = regexp.MustCompile(`(?i)(?:Primary working directory|Current working directory|cwd|working directory)[:\s]+(/[^\n\r]+)`)

// cascadeProducer runs the full Cascade session and pushes events to the channel.
func (c *WindsurfGrpcClient) cascadeProducer(
	ctx context.Context,
	out chan<- StreamEvent,
	instanceName string,
	accountID string,
	apiKey string,
	messages []map[string]string,
	modelEnum core.ModelEnum,
	modelUID string,
	version string,
) {
	defer close(out)

	// Separate system prompt
	var systemPrompt string
	var nonSystem []map[string]string
	for _, msg := range messages {
		role := msg["role"]
		if role == "system" {
			systemPrompt = msg["content"]
		} else {
			nonSystem = append(nonSystem, msg)
		}
	}

	// Extract working directory from system prompt
	cwd := ""
	if match := cwdRegex.FindStringSubmatch(systemPrompt); match != nil {
		cwd = strings.TrimSpace(match[1])
	}

	nativeCascadeTools := strings.Contains(systemPrompt, protobuf.NativeCascadeToolsMarker)

	// Build user text from all non-system messages
	var userParts []string
	for _, msg := range nonSystem {
		role := msg["role"]
		content := msg["content"]
		if role == "assistant" {
			// Check if assistant made tool calls
			if toolCalls, ok := msg["tool_calls"]; ok && toolCalls != "" {
				userParts = append(userParts, "[Assistant called tools: "+toolCalls+"]")
			} else if content != "" {
				userParts = append(userParts, "[Assistant]: "+content)
			}
		} else if role == "tool" {
			name := msg["name"]
			userParts = append(userParts, "[Tool result ("+name+")]: "+content)
		} else {
			if content != "" {
				userParts = append(userParts, content)
			}
		}
	}
	userText := strings.Join(userParts, "\n")

	// Prepend working directory if available
	if cwd != "" {
		userText = "[Working directory: " + cwd + "]\n\n" + userText
	}

	sendTimeout := cascadeSendTimeout(userText, systemPrompt)
	log.Printf("[Cascade] user_text length=%d, system_prompt length=%d, cwd=%s, msgs=%d send_timeout=%s",
		len(userText), len(systemPrompt), cwd, len(nonSystem), sendTimeout)

	if err := c.ensureCascadePanelState(apiKey, version, accountID); err != nil {
		log.Printf("[Cascade] panel init failed: %v", err)
		out <- StreamEvent{Type: "error", Data: err.Error()}
		return
	}

	// GetCascadeTrajectorySteps returns the cumulative trajectory for the
	// entire cascade session. Reusing cascade_id across API turns causes
	// old text/tool calls/done markers to bleed into the new turn, so each
	// request starts a fresh session and carries history in userText.
	cascadeID, err := c.CascadeStart(ctx, apiKey, version)
	if err != nil {
		log.Printf("[Cascade] start failed: %v", err)
		out <- StreamEvent{Type: "error", Data: err.Error()}
		return
	}
	log.Printf("[Cascade] started: %s", cascadeID)

	// Step 2: Send message
	err = c.CascadeSend(ctx, cascadeID, userText, modelEnum, modelUID, apiKey, version, systemPrompt)
	if isPanelStateMissing(err) {
		c.invalidateCascadePanelState(accountID)
		if initErr := c.ensureCascadePanelState(apiKey, version, accountID); initErr != nil {
			err = initErr
		} else {
			log.Printf("[Cascade] panel state initialized on demand, retrying send")
			err = c.CascadeSend(ctx, cascadeID, userText, modelEnum, modelUID, apiKey, version, systemPrompt)
		}
	}
	if err != nil {
		log.Printf("[Cascade] send failed: %v", err)
		out <- StreamEvent{Type: "error", Data: err.Error()}
		return
	}
	log.Printf("[Cascade] message sent (model=%s)", modelUID)

	// Step 3: Poll for response
	firstVisibleStart := time.Now()
	time.Sleep(CascadeInitialWait)

	prevText := ""
	prevThinking := ""
	seenToolCalls := make(map[string]bool)
	seenToolResults := make(map[string]bool)
	emittedAny := false
	sawModelOutput := false
	firstVisibleLogged := false
	contentStableCount := 0
	autoContinueCount := 0
	roundToolCallCount := 0
	lastToolActivityPoll := 0
	isTextlessModel := !strings.HasPrefix(modelUID, "swe-")

	logFirstVisible := func(kind string, size int) {
		if firstVisibleLogged {
			return
		}
		firstVisibleLogged = true
		log.Printf("[Cascade] first visible %s after %s (%d chars)", kind, time.Since(firstVisibleStart), size)
	}

	for i := 0; i < CascadeMaxPolls; i++ {
		pollCount := i + 1

		select {
		case <-ctx.Done():
			return
		default:
		}

		result, err := c.CascadePoll(ctx, cascadeID)
		if err != nil {
			log.Printf("[Cascade] poll failed: %v", err)
			out <- StreamEvent{Type: "error", Data: err.Error()}
			return
		}

		if result.Error != "" {
			log.Printf("[Cascade] error: %s", result.Error)
			out <- StreamEvent{Type: "error", Data: result.Error}
			return
		}

		// Log first few polls and important events
		if pollCount <= 3 || result.Done || result.Error != "" {
			log.Printf("[Cascade] poll #%d: text=%d thinking=%d done=%v tc=%d tr=%d",
				pollCount, len(result.Text), len(result.Thinking),
				result.Done, len(result.ToolCalls), len(result.ToolResults))
		}

		// Emit tool calls
		for _, tc := range result.ToolCalls {
			sig := tc.Name + ":" + tc.Arguments
			if seenToolCalls[sig] {
				continue
			}
			seenToolCalls[sig] = true
			roundToolCallCount++
			lastToolActivityPoll = pollCount
			args := tc.Arguments
			if len(args) > 100 {
				args = args[:100] + "..."
			}
			log.Printf("[Cascade] tool_call: %s(%s)", tc.Name, args)
			logFirstVisible("tool_call", len(tc.Arguments))
			if !nativeCascadeTools {
				out <- StreamEvent{Type: "tool_call", Data: tc}
			}
		}

		// Emit tool results
		for _, tr := range result.ToolResults {
			sig := tr.ToolURI + ":" + tr.Output
			if seenToolResults[sig] {
				continue
			}
			seenToolResults[sig] = true
			lastToolActivityPoll = pollCount
			log.Printf("[Cascade] tool_result: %s (%d chars)", tr.ToolName, len(tr.Output))
			if !nativeCascadeTools {
				out <- StreamEvent{Type: "tool_result", Data: tr}
			}
		}

		// Emit thinking delta (for textless models)
		if len(result.Thinking) > len(prevThinking) {
			delta := result.Thinking[len(prevThinking):]
			prevThinking = result.Thinking
			sawModelOutput = true
			if isTextlessModel {
				logFirstVisible("thinking", len(delta))
				if !nativeCascadeTools {
					out <- StreamEvent{Type: "text", Data: delta}
					emittedAny = true
				}
			}
			contentStableCount = 0
		}

		// Emit text delta
		if len(result.Text) > len(prevText) {
			delta := result.Text[len(prevText):]
			prevText = result.Text
			logFirstVisible("text", len(delta))
			if !nativeCascadeTools {
				out <- StreamEvent{Type: "text", Data: delta}
				emittedAny = true
			}
			sawModelOutput = true
			contentStableCount = 0
		} else {
			contentStableCount++
		}

		pendingNativeTool := nativeCascadeTools && len(seenToolCalls) > len(seenToolResults)
		if pendingNativeTool {
			pollsSinceToolActivity := pollCount - lastToolActivityPoll
			if strings.TrimSpace(prevText) == "" {
				if pollsSinceToolActivity >= CascadePendingNativeToolPolls {
					err := fmt.Errorf("native Cascade tool call timed out before final text")
					log.Printf("[Cascade] %v", err)
					out <- StreamEvent{Type: "error", Data: err.Error()}
					return
				}
				time.Sleep(CascadePollInterval)
				continue
			}
			if pollsSinceToolActivity < CascadeForcedStablePolls {
				time.Sleep(CascadePollInterval)
				continue
			}
		}

		forcedStable := !result.Done &&
			sawModelOutput &&
			roundToolCallCount == 0 &&
			contentStableCount >= CascadeForcedStablePolls

		// End session when content stabilized. Some LS builds never surface
		// the final done marker even after the answer stops changing; in that
		// case we force completion after a longer quiet window instead of
		// holding the client open until timeout.
		if (result.Done && contentStableCount >= CascadeStablePolls) || forcedStable {
			// Native Cascade often surfaces a transient done marker after a tool
			// result but before the final answer. Nudge it to continue, but keep
			// buffering output so Claude Code only sees the final response.
			if result.Done &&
				nativeCascadeTools &&
				strings.TrimSpace(result.Text) == "" {
				if autoContinueCount >= CascadeMaxAutoContinue {
					err := fmt.Errorf("native Cascade completed tool calls without final text")
					log.Printf("[Cascade] %v", err)
					out <- StreamEvent{Type: "error", Data: err.Error()}
					return
				}

				roundToolCallCount = 0
				contentStableCount = 0
				log.Printf("[Cascade] auto-continue native round %d for model=%s after tool-only marker",
					autoContinueCount+1, modelUID)
				err = c.CascadeSend(ctx, cascadeID, CascadeContinuePrompt, modelEnum, modelUID, apiKey, version, systemPrompt)
				if isCascadeExecutorNotIdle(err) {
					log.Printf("[Cascade] native auto-continue deferred; executor is still running")
					time.Sleep(CascadeInitialWait)
					continue
				}
				if err != nil {
					log.Printf("[Cascade] native auto-continue send failed: %v", err)
				} else {
					autoContinueCount++
				}
				time.Sleep(CascadeInitialWait)
				continue
			}

			// Auto-continue for non-native models that only return tool calls without text.
			if result.Done &&
				!pendingNativeTool &&
				!strings.HasPrefix(modelUID, "swe-") &&
				autoContinueCount < CascadeMaxAutoContinue &&
				roundToolCallCount > 0 &&
				strings.TrimSpace(result.Text) == "" {
				autoContinueCount++
				roundToolCallCount = 0
				contentStableCount = 0

				log.Printf("[Cascade] auto-continue round %d for model=%s after tool-only completion",
					autoContinueCount, modelUID)

				for retry := 0; retry <= CascadeContinueIdleRetries; retry++ {
					err = c.CascadeSend(ctx, cascadeID, CascadeContinuePrompt, modelEnum, modelUID, apiKey, version, systemPrompt)
					if !isCascadeExecutorNotIdle(err) {
						break
					}
					time.Sleep(CascadeContinueIdleDelay)
				}
				if err != nil {
					log.Printf("[Cascade] auto-continue send failed: %v", err)
				}
				time.Sleep(CascadeInitialWait)
				continue
			}

			if nativeCascadeTools && strings.TrimSpace(prevText) != "" {
				out <- StreamEvent{Type: "text", Data: prevText}
				emittedAny = true
			}

			// Final fallback: if no text was ever emitted but thinking exists, emit it.
			// Native Cascade tool mode suppresses thinking because it is often only a
			// progress sentence, not the final answer.
			if !nativeCascadeTools && !emittedAny && result.Thinking != "" {
				logFirstVisible("thinking_fallback", len(result.Thinking))
				out <- StreamEvent{Type: "text", Data: result.Thinking}
			}
			if forcedStable {
				log.Printf("[Cascade] content stable for %d polls without done marker, forcing session end", contentStableCount)
			} else {
				log.Printf("[Cascade] content stabilized after %d polls, ending session", contentStableCount)
			}
			return
		}

		time.Sleep(CascadePollInterval)
	}

	log.Printf("[Cascade] max polls reached")
}

// rawStreamProducer handles RawGetChatMessage server streaming.
func (c *WindsurfGrpcClient) rawStreamProducer(
	ctx context.Context,
	out chan<- StreamEvent,
	apiKey string,
	messages []map[string]string,
	modelEnum core.ModelEnum,
	modelName string,
	version string,
) {
	defer close(out)

	req := protobuf.BuildChatRequest(apiKey, messages, int(modelEnum), modelName, version)

	// RawGetChatMessage uses server streaming
	// We'll implement this using raw gRPC stream handling
	streamCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	// Connect and send streaming request
	channel, err := c.ensureChannel()
	if err != nil {
		out <- StreamEvent{Type: "error", Data: err.Error()}
		return
	}

	sd := &grpc.StreamDesc{
		ServerStreams: true,
	}

	target := fmt.Sprintf("%s:%d", c.Host, c.Port)
	streamT0 := time.Now()
	stream, err := channel.NewStream(
		c.withAuthMetadata(streamCtx),
		sd,
		"/"+GRPCService+"/RawGetChatMessage",
		grpc.ForceCodec(rawCodec{}),
	)
	audit.FromContext(ctx).RecordUpstreamCall(target, "RawGetChatMessage", time.Since(streamT0).Milliseconds(), err)
	if err != nil {
		out <- StreamEvent{Type: "error", Data: err.Error()}
		return
	}

	if err := stream.SendMsg(req); err != nil {
		out <- StreamEvent{Type: "error", Data: "send failed: " + err.Error()}
		return
	}
	// No more client messages on this unary-send / server-stream call.
	if err := stream.CloseSend(); err != nil {
		log.Printf("[Raw] close send error: %v", err)
	}

	// Receive server stream. With rawCodec, each RecvMsg yields one already-
	// unframed protobuf payload — no manual GRPCUnframe and no fixed buffer.
	for {
		var resp []byte
		err := stream.RecvMsg(&resp)
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Printf("[Raw] recv error: %v", err)
			out <- StreamEvent{Type: "error", Data: err.Error()}
			return
		}
		if len(resp) == 0 {
			continue
		}

		text := protobuf.ExtractTextFromResponse(resp)
		if text != "" {
			out <- StreamEvent{Type: "text", Data: text}
		}
	}
}

// Ping checks if the port is reachable.
func (c *WindsurfGrpcClient) Ping() bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", c.Host, c.Port), 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// InitializeCascadePanelState calls InitializeCascadePanelState on the LS.
func (c *WindsurfGrpcClient) InitializeCascadePanelState(apiKey string, version string) error {
	// Build request: field 1 = metadata, field 3 = workspace_trusted (bool = true)
	meta := protobuf.EncodeMetadata(apiKey, version)
	req := make([]byte, 0)
	req = append(req, protobuf.EncodeMessageField(1, meta)...)
	req = append(req, protobuf.EncodeVarintField(3, 1)...)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := c.rawCall(ctx, "InitializeCascadePanelState", req)
	return err
}

func (c *WindsurfGrpcClient) ensureCascadePanelState(apiKey string, version string, accountID string) error {
	key := panelStateKey(accountID)
	signature := apiKey + "|" + version

	c.panelMu.Lock()
	if c.panelInit[key] == signature {
		c.panelMu.Unlock()
		return nil
	}
	c.panelMu.Unlock()

	if err := c.InitializeCascadePanelState(apiKey, version); err != nil {
		return err
	}

	c.panelMu.Lock()
	c.panelInit[key] = signature
	c.panelMu.Unlock()
	return nil
}

func (c *WindsurfGrpcClient) invalidateCascadePanelState(accountID string) {
	c.panelMu.Lock()
	delete(c.panelInit, panelStateKey(accountID))
	c.panelMu.Unlock()
}

func panelStateKey(accountID string) string {
	if accountID != "" {
		return "account:" + accountID
	}
	return "instance"
}

func isPanelStateMissing(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "panel state not found")
}
