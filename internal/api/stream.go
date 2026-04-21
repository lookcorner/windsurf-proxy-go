// Package api provides SSE streaming helpers.
package api

import (
	"encoding/json"
	"fmt"
	"io"
)

// SSEWriter writes Server-Sent Events to an io.Writer.
type SSEWriter struct {
	w       io.Writer
	flusher interface{ Flush() }
}

// NewSSEWriter creates a new SSE writer.
func NewSSEWriter(w io.Writer) *SSEWriter {
	sse := &SSEWriter{w: w}
	if flusher, ok := w.(interface{ Flush() }); ok {
		sse.flusher = flusher
	}
	return sse
}

// WriteEvent writes a single SSE event.
func (s *SSEWriter) WriteEvent(data string) error {
	_, err := fmt.Fprintf(s.w, "data: %s\n\n", data)
	if err == nil && s.flusher != nil {
		s.flusher.Flush()
	}
	return err
}

// WriteJSON writes a JSON SSE event.
func (s *SSEWriter) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.WriteEvent(string(data))
}

// WriteDone writes the SSE done marker.
func (s *SSEWriter) WriteDone() error {
	_, err := fmt.Fprint(s.w, "data: [DONE]\n\n")
	if err == nil && s.flusher != nil {
		s.flusher.Flush()
	}
	return err
}

// WriteError writes an error SSE event.
func (s *SSEWriter) WriteError(message string, errType string) error {
	errResp := ErrorResponse{
		Error: ErrorDetail{
			Message: message,
			Type:    errType,
		},
	}
	return s.WriteJSON(errResp)
}

// StreamChatCompletion streams a chat completion response.
func StreamChatCompletion(w io.Writer, model string, events <-chan string) error {
	sse := NewSSEWriter(w)

	// First chunk: role
	first := NewStreamChunk(model, DeltaContent{Role: "assistant"}, "")
	if err := sse.WriteJSON(first); err != nil {
		return err
	}

	// Content chunks
	for content := range events {
		chunk := NewStreamChunk(model, DeltaContent{Content: content}, "")
		if err := sse.WriteJSON(chunk); err != nil {
			return err
		}
	}

	// Final chunk
	done := NewStreamChunk(model, DeltaContent{}, "stop")
	if err := sse.WriteJSON(done); err != nil {
		return err
	}

	return sse.WriteDone()
}
