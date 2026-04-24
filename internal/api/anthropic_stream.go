package api

import (
	"encoding/json"
	"fmt"
	"io"

	"windsurf-proxy-go/internal/redact"
)

// AnthropicSSEWriter writes Anthropic-style named SSE events.
//
// The wire format is:
//
//	event: <name>
//	data: <json>
//	\n
//
// Order of events in a successful response:
//
//	message_start
//	content_block_start + content_block_delta* + content_block_stop  (per block)
//	message_delta
//	message_stop
type AnthropicSSEWriter struct {
	w       io.Writer
	flusher interface{ Flush() }
}

// NewAnthropicSSEWriter creates a writer bound to the given response writer.
func NewAnthropicSSEWriter(w io.Writer) *AnthropicSSEWriter {
	s := &AnthropicSSEWriter{w: w}
	if f, ok := w.(interface{ Flush() }); ok {
		s.flusher = f
	}
	return s
}

func (s *AnthropicSSEWriter) write(event string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

// WriteMessageStart emits event:message_start.
func (s *AnthropicSSEWriter) WriteMessageStart(id, model string, inputTokens int) error {
	return s.write("message_start", AnthropicStreamMessageStart{
		Type: "message_start",
		Message: AnthropicStreamStartMessage{
			ID:           id,
			Type:         "message",
			Role:         "assistant",
			Model:        model,
			Content:      []AnthropicOutBlock{},
			StopReason:   nil,
			StopSequence: nil,
			Usage: AnthropicUsage{
				InputTokens:  inputTokens,
				OutputTokens: 0,
			},
		},
	})
}

// WriteBlockStart emits event:content_block_start for either a text or
// tool_use block.
//
// Anthropic's wire protocol requires the placeholder fields to be present
// even when empty: a text block must serialize as
// {"type":"text","text":""} and a tool_use block must include
// {"input":{}}. The shared AnthropicOutBlock struct uses `omitempty` so
// that non-stream responses don't carry redundant zero values, but here
// we build the content_block payload manually so strict clients (Cline,
// Claude Code, anthropic-sdk) don't see `undefined` for the missing keys
// — that bug surfaces as a literal "undefined" prefix on every streamed
// chunk.
func (s *AnthropicSSEWriter) WriteBlockStart(index int, block AnthropicOutBlock) error {
	cb := map[string]interface{}{"type": block.Type}
	switch block.Type {
	case "tool_use":
		cb["id"] = block.ID
		cb["name"] = block.Name
		if block.Input != nil {
			cb["input"] = block.Input
		} else {
			cb["input"] = map[string]interface{}{}
		}
	case "thinking":
		cb["thinking"] = block.Thinking
	default:
		cb["text"] = block.Text
	}
	return s.write("content_block_start", map[string]interface{}{
		"type":          "content_block_start",
		"index":         index,
		"content_block": cb,
	})
}

// WriteTextDelta emits event:content_block_delta with a text_delta.
func (s *AnthropicSSEWriter) WriteTextDelta(index int, text string) error {
	return s.write("content_block_delta", AnthropicStreamBlockDelta{
		Type:  "content_block_delta",
		Index: index,
		Delta: AnthropicStreamContentDelta{
			Type: "text_delta",
			Text: redact.SanitizeText(text),
		},
	})
}

// WriteInputJSONDelta emits event:content_block_delta with an
// input_json_delta carrying the serialized tool arguments.
func (s *AnthropicSSEWriter) WriteInputJSONDelta(index int, partial string) error {
	return s.write("content_block_delta", AnthropicStreamBlockDelta{
		Type:  "content_block_delta",
		Index: index,
		Delta: AnthropicStreamContentDelta{
			Type:        "input_json_delta",
			PartialJSON: redact.SanitizeText(partial),
		},
	})
}

// WriteBlockStop emits event:content_block_stop.
func (s *AnthropicSSEWriter) WriteBlockStop(index int) error {
	return s.write("content_block_stop", AnthropicStreamBlockStop{
		Type:  "content_block_stop",
		Index: index,
	})
}

// WriteMessageDelta emits event:message_delta with the final stop_reason
// and a cumulative output_tokens count.
func (s *AnthropicSSEWriter) WriteMessageDelta(stopReason string, outputTokens int) error {
	return s.write("message_delta", AnthropicStreamMessageDelta{
		Type: "message_delta",
		Delta: AnthropicStreamMessageDeltaInfo{
			StopReason:   stopReason,
			StopSequence: nil,
		},
		Usage: AnthropicUsage{
			OutputTokens: outputTokens,
		},
	})
}

// WriteMessageStop emits event:message_stop.
func (s *AnthropicSSEWriter) WriteMessageStop() error {
	return s.write("message_stop", AnthropicStreamMessageStop{
		Type: "message_stop",
	})
}

// WriteError emits event:error. Clients that understand the Anthropic stream
// protocol know to treat this as a terminal condition.
func (s *AnthropicSSEWriter) WriteError(errType, message string) error {
	return s.write("error", AnthropicErrorResponse{
		Type: "error",
		Error: AnthropicErrorDetail{
			Type:    errType,
			Message: redact.SanitizeText(message),
		},
	})
}

// WritePing emits event:ping. Useful as a keepalive between long tool
// invocations if desired.
func (s *AnthropicSSEWriter) WritePing() error {
	return s.write("ping", map[string]string{"type": "ping"})
}
