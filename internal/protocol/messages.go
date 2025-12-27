// Package protocol defines the message format for terminal-tunnel communication
package protocol

import (
	"encoding/binary"
	"errors"
)

// MsgType represents the type of terminal message
type MsgType byte

const (
	MsgData   MsgType = 0x01 // Terminal I/O data
	MsgResize MsgType = 0x02 // Window resize event
	MsgPing   MsgType = 0x03 // Keepalive ping
	MsgPong   MsgType = 0x04 // Keepalive pong
	MsgClose  MsgType = 0x05 // Graceful close
)

// Header size: 1 byte type + 2 bytes length
const headerSize = 3

var (
	ErrMessageTooShort = errors.New("message too short")
	ErrInvalidLength   = errors.New("invalid message length")
)

// Message represents a terminal protocol message
type Message struct {
	Type    MsgType
	Payload []byte
}

// ResizePayload contains terminal dimensions
type ResizePayload struct {
	Rows uint16
	Cols uint16
}

// MaxPayloadSize is the maximum allowed message payload size (64KB - 1)
const MaxPayloadSize = 65535

// Encode serializes a message to wire format.
// Format: [1 byte type][2 byte length (big-endian)][payload]
func (m *Message) Encode() []byte {
	length := len(m.Payload)
	if length > MaxPayloadSize {
		// Truncate if too large - this shouldn't happen in normal operation
		length = MaxPayloadSize
		m.Payload = m.Payload[:MaxPayloadSize]
	}
	buf := make([]byte, headerSize+length)
	buf[0] = byte(m.Type)
	binary.BigEndian.PutUint16(buf[1:3], uint16(length)) //nolint:gosec // length is bounds-checked above
	copy(buf[headerSize:], m.Payload)
	return buf
}

// DecodeMessage parses a wire format message.
func DecodeMessage(data []byte) (*Message, error) {
	if len(data) < headerSize {
		return nil, ErrMessageTooShort
	}

	msgType := MsgType(data[0])
	length := binary.BigEndian.Uint16(data[1:3])

	if len(data) < headerSize+int(length) {
		return nil, ErrInvalidLength
	}

	payload := make([]byte, length)
	copy(payload, data[headerSize:headerSize+int(length)])

	return &Message{
		Type:    msgType,
		Payload: payload,
	}, nil
}

// NewDataMessage creates a terminal data message.
func NewDataMessage(data []byte) *Message {
	return &Message{
		Type:    MsgData,
		Payload: data,
	}
}

// NewResizeMessage creates a resize message.
func NewResizeMessage(rows, cols uint16) *Message {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint16(payload[0:2], rows)
	binary.BigEndian.PutUint16(payload[2:4], cols)
	return &Message{
		Type:    MsgResize,
		Payload: payload,
	}
}

// ParseResizePayload extracts dimensions from a resize message payload.
func ParseResizePayload(payload []byte) (*ResizePayload, error) {
	if len(payload) < 4 {
		return nil, ErrMessageTooShort
	}
	return &ResizePayload{
		Rows: binary.BigEndian.Uint16(payload[0:2]),
		Cols: binary.BigEndian.Uint16(payload[2:4]),
	}, nil
}

// NewPingMessage creates a keepalive ping.
func NewPingMessage() *Message {
	return &Message{Type: MsgPing}
}

// NewPongMessage creates a keepalive pong.
func NewPongMessage() *Message {
	return &Message{Type: MsgPong}
}

// NewCloseMessage creates a graceful close message.
func NewCloseMessage() *Message {
	return &Message{Type: MsgClose}
}
