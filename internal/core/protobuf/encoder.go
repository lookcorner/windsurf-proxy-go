// Package protobuf provides manual protobuf encoding/decoding for Windsurf gRPC protocol.
// No .proto files needed — we construct messages by hand based on
// reverse-engineered field numbers from Windsurf's extension.js.
//
// Reference: https://github.com/rsvedant/opencode-windsurf-auth/blob/master/src/plugin/grpc-client.ts
package protobuf

// EncodeVarint encodes an integer as a protobuf varint.
func EncodeVarint(value uint64) []byte {
	pieces := make([]byte, 0, 10)
	for value > 0x7F {
		pieces = append(pieces, byte((value&0x7F)|0x80))
		value >>= 7
	}
	pieces = append(pieces, byte(value&0x7F))
	return pieces
}

// EncodeVarintField encodes a varint field (wire type 0).
func EncodeVarintField(fieldNumber int, value uint64) []byte {
	tag := uint64((fieldNumber << 3) | 0)
	return append(EncodeVarint(tag), EncodeVarint(value)...)
}

// EncodeBoolField encodes a boolean as a protobuf varint field.
func EncodeBoolField(fieldNumber int, value bool) []byte {
	if value {
		return EncodeVarintField(fieldNumber, 1)
	}
	return EncodeVarintField(fieldNumber, 0)
}

// EncodeBytesField encodes a length-delimited field (wire type 2).
func EncodeBytesField(fieldNumber int, data []byte) []byte {
	tag := uint64((fieldNumber << 3) | 2)
	result := EncodeVarint(tag)
	result = append(result, EncodeVarint(uint64(len(data)))...)
	return append(result, data...)
}

// EncodeStringField encodes a string field (wire type 2, UTF-8).
func EncodeStringField(fieldNumber int, value string) []byte {
	return EncodeBytesField(fieldNumber, []byte(value))
}

// EncodeMessageField encodes an embedded message field (wire type 2).
func EncodeMessageField(fieldNumber int, data []byte) []byte {
	return EncodeBytesField(fieldNumber, data)
}

// EncodeTimestamp encodes current time as google.protobuf.Timestamp.
func EncodeTimestamp(nowMs int64) []byte {
	seconds := nowMs / 1000
	nanos := (nowMs % 1000) * 1_000_000
	buf := EncodeVarintField(1, uint64(seconds))
	if nanos > 0 {
		buf = append(buf, EncodeVarintField(2, uint64(nanos))...)
	}
	return buf
}
