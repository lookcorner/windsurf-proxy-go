package grpc

import (
	"fmt"

	"google.golang.org/grpc/encoding"
)

// rawCodec is a gRPC codec that treats the request/response as opaque bytes.
//
// The Windsurf proxy talks to language_server using hand-built protobuf
// payloads (no .proto files), so both Marshal and Unmarshal must just pass
// the raw bytes through. Because the reported Name() is "proto", the wire
// Content-Type remains "application/grpc+proto" and the server is happy.
//
// This is used together with grpc.ForceCodec(...) on Invoke so it overrides
// the default proto codec only for our raw calls.
type rawCodec struct{}

// Marshal accepts either a []byte or *[]byte and returns the bytes unchanged.
func (rawCodec) Marshal(v any) ([]byte, error) {
	switch b := v.(type) {
	case []byte:
		return b, nil
	case *[]byte:
		if b == nil {
			return nil, nil
		}
		return *b, nil
	default:
		return nil, fmt.Errorf("rawCodec: unsupported marshal type %T", v)
	}
}

// Unmarshal writes the wire bytes directly into the caller's *[]byte.
func (rawCodec) Unmarshal(data []byte, v any) error {
	switch b := v.(type) {
	case *[]byte:
		buf := make([]byte, len(data))
		copy(buf, data)
		*b = buf
		return nil
	default:
		return fmt.Errorf("rawCodec: unsupported unmarshal type %T", v)
	}
}

// Name must match what the server expects in the sub-content-type. Windsurf
// negotiates "application/grpc+proto", so we keep "proto".
func (rawCodec) Name() string { return "proto" }

// ensure interface compliance
var _ encoding.Codec = rawCodec{}
