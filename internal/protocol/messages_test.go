package protocol

import (
	"bytes"
	"testing"
)

func TestMessageEncodeDecode(t *testing.T) {
	tests := []struct {
		name    string
		msg     *Message
		wantLen int
	}{
		{
			name:    "data message",
			msg:     NewDataMessage([]byte("hello world")),
			wantLen: 3 + 11, // header + payload
		},
		{
			name:    "empty data",
			msg:     NewDataMessage([]byte{}),
			wantLen: 3,
		},
		{
			name:    "ping",
			msg:     NewPingMessage(),
			wantLen: 3,
		},
		{
			name:    "pong",
			msg:     NewPongMessage(),
			wantLen: 3,
		},
		{
			name:    "close",
			msg:     NewCloseMessage(),
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := tt.msg.Encode()

			if len(encoded) != tt.wantLen {
				t.Errorf("encoded length = %d, want %d", len(encoded), tt.wantLen)
			}

			decoded, err := DecodeMessage(encoded)
			if err != nil {
				t.Fatalf("DecodeMessage failed: %v", err)
			}

			if decoded.Type != tt.msg.Type {
				t.Errorf("type = %v, want %v", decoded.Type, tt.msg.Type)
			}

			if !bytes.Equal(decoded.Payload, tt.msg.Payload) {
				t.Errorf("payload = %v, want %v", decoded.Payload, tt.msg.Payload)
			}
		})
	}
}

func TestResizeMessage(t *testing.T) {
	rows := uint16(24)
	cols := uint16(80)

	msg := NewResizeMessage(rows, cols)

	if msg.Type != MsgResize {
		t.Errorf("type = %v, want MsgResize", msg.Type)
	}

	if len(msg.Payload) != 4 {
		t.Errorf("resize payload length = %d, want 4", len(msg.Payload))
	}

	// Encode and decode
	encoded := msg.Encode()
	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage failed: %v", err)
	}

	resize, err := ParseResizePayload(decoded.Payload)
	if err != nil {
		t.Fatalf("ParseResizePayload failed: %v", err)
	}

	if resize.Rows != rows {
		t.Errorf("rows = %d, want %d", resize.Rows, rows)
	}

	if resize.Cols != cols {
		t.Errorf("cols = %d, want %d", resize.Cols, cols)
	}
}

func TestDecodeMessageTooShort(t *testing.T) {
	_, err := DecodeMessage([]byte{0x01, 0x00})
	if err != ErrMessageTooShort {
		t.Errorf("expected ErrMessageTooShort, got %v", err)
	}
}

func TestDecodeMessageInvalidLength(t *testing.T) {
	// Header says 100 bytes but only 1 byte of payload
	data := []byte{0x01, 0x00, 0x64, 0xFF}
	_, err := DecodeMessage(data)
	if err != ErrInvalidLength {
		t.Errorf("expected ErrInvalidLength, got %v", err)
	}
}

func TestParseResizePayloadTooShort(t *testing.T) {
	_, err := ParseResizePayload([]byte{0x00, 0x18})
	if err != ErrMessageTooShort {
		t.Errorf("expected ErrMessageTooShort, got %v", err)
	}
}

func TestLargeDataMessage(t *testing.T) {
	// Test with large payload (64KB)
	data := make([]byte, 65535)
	for i := range data {
		data[i] = byte(i % 256)
	}

	msg := NewDataMessage(data)
	encoded := msg.Encode()

	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage failed: %v", err)
	}

	if !bytes.Equal(decoded.Payload, data) {
		t.Error("large payload mismatch")
	}
}

func TestMessageTypes(t *testing.T) {
	tests := []struct {
		msg      *Message
		wantType MsgType
	}{
		{NewDataMessage([]byte("x")), MsgData},
		{NewResizeMessage(24, 80), MsgResize},
		{NewPingMessage(), MsgPing},
		{NewPongMessage(), MsgPong},
		{NewCloseMessage(), MsgClose},
	}

	for _, tt := range tests {
		if tt.msg.Type != tt.wantType {
			t.Errorf("got type %v, want %v", tt.msg.Type, tt.wantType)
		}
	}
}
