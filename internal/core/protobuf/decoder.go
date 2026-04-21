package protobuf

import (
	"encoding/binary"
)

// ProtoField represents a parsed protobuf field.
type ProtoField struct {
	FieldNumber int
	WireType    int
	Value       interface{} // []byte for wire_type 2, uint64 for wire_type 0
}

// DecodeVarint decodes a varint from buffer at offset.
// Returns (value, bytes_consumed).
func DecodeVarint(buf []byte, offset int) (uint64, int) {
	result := uint64(0)
	shift := 0
	consumed := 0
	for offset+consumed < len(buf) {
		b := buf[offset+consumed]
		consumed++
		result |= uint64(b&0x7F) << shift
		if (b & 0x80) == 0 {
			break
		}
		shift += 7
	}
	return result, consumed
}

// ParseFields parses all protobuf fields from a buffer.
func ParseFields(buf []byte) []ProtoField {
	fields := make([]ProtoField, 0)
	offset := 0
	for offset < len(buf) {
		tag, tagBytes := DecodeVarint(buf, offset)
		fieldNumber := int(tag >> 3)
		wireType := int(tag & 0x7)
		offset += tagBytes

		switch wireType {
		case 0: // varint
			value, valBytes := DecodeVarint(buf, offset)
			offset += valBytes
			fields = append(fields, ProtoField{
				FieldNumber: fieldNumber,
				WireType:    wireType,
				Value:       value,
			})
		case 2: // length-delimited
			length, lenBytes := DecodeVarint(buf, offset)
			offset += lenBytes
			if offset+int(length) > len(buf) {
				break
			}
			data := buf[offset : offset+int(length)]
			offset += int(length)
			fields = append(fields, ProtoField{
				FieldNumber: fieldNumber,
				WireType:    wireType,
				Value:       data,
			})
		case 1: // 64-bit
			if offset+8 > len(buf) {
				break
			}
			offset += 8
		case 5: // 32-bit
			if offset+4 > len(buf) {
				break
			}
			offset += 4
		default:
			break // unknown wire type
		}
	}
	return fields
}

// GetStringField extracts a string value from parsed fields.
func GetStringField(fields []ProtoField, fieldNumber int) string {
	for _, f := range fields {
		if f.FieldNumber == fieldNumber && f.WireType == 2 {
			if data, ok := f.Value.([]byte); ok {
				return string(data)
			}
		}
	}
	return ""
}

// GetMessageField extracts a raw embedded message from parsed fields.
func GetMessageField(fields []ProtoField, fieldNumber int) []byte {
	for _, f := range fields {
		if f.FieldNumber == fieldNumber && f.WireType == 2 {
			if data, ok := f.Value.([]byte); ok {
				return data
			}
		}
	}
	return nil
}

// GetVarintField extracts a varint value from parsed fields.
func GetVarintField(fields []ProtoField, fieldNumber int) uint64 {
	for _, f := range fields {
		if f.FieldNumber == fieldNumber && f.WireType == 0 {
			if value, ok := f.Value.(uint64); ok {
				return value
			}
		}
	}
	return 0
}

// HasField checks if a field exists.
func HasField(fields []ProtoField, fieldNumber int) bool {
	for _, f := range fields {
		if f.FieldNumber == fieldNumber {
			return true
		}
	}
	return false
}

// GRPCFrame wraps a protobuf payload in a gRPC frame.
// Format: 1-byte flag (compressed) + 4-byte length (big endian) + payload
func GRPCFrame(payload []byte) []byte {
	header := make([]byte, 5)
	header[0] = 0 // not compressed
	binary.BigEndian.PutUint32(header[1:5], uint32(len(payload)))
	return append(header, payload...)
}

// GRPCUnframe extracts protobuf messages from gRPC-framed data.
// Returns list of payloads.
func GRPCUnframe(data []byte) [][]byte {
	messages := make([][]byte, 0)
	offset := 0
	for offset+5 <= len(data) {
		compressed := data[offset]
		msgLen := binary.BigEndian.Uint32(data[offset+1 : offset+5])
		offset += 5
		if compressed != 0 {
			offset += int(msgLen)
			continue
		}
		if offset+int(msgLen) > len(data) {
			break
		}
		messages = append(messages, data[offset:offset+int(msgLen)])
		offset += int(msgLen)
	}
	return messages
}