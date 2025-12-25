package webrtc

import (
	"io"
	"sync"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
	"github.com/artpar/terminal-tunnel/internal/protocol"
)

// EncryptedChannel wraps a WebRTC DataChannel with encryption and protocol handling
type EncryptedChannel struct {
	dc  *webrtc.DataChannel
	key *[32]byte

	onData   func([]byte)
	onResize func(rows, cols uint16)
	onClose  func()

	mu     sync.Mutex
	closed bool
}

// NewEncryptedChannel creates an encrypted wrapper for a DataChannel
func NewEncryptedChannel(dc *webrtc.DataChannel, key *[32]byte) *EncryptedChannel {
	ec := &EncryptedChannel{
		dc:  dc,
		key: key,
	}

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		ec.handleMessage(msg.Data)
	})

	dc.OnClose(func() {
		ec.mu.Lock()
		ec.closed = true
		handler := ec.onClose
		ec.mu.Unlock()
		if handler != nil {
			handler()
		}
	})

	return ec
}

// handleMessage decrypts and processes incoming messages
func (ec *EncryptedChannel) handleMessage(data []byte) {
	// Decrypt the message
	plaintext, err := crypto.Decrypt(data, ec.key)
	if err != nil {
		// Decryption failed - likely wrong password or corrupted data
		return
	}

	// Parse the protocol message
	msg, err := protocol.DecodeMessage(plaintext)
	if err != nil {
		return
	}

	switch msg.Type {
	case protocol.MsgData:
		if ec.onData != nil {
			ec.onData(msg.Payload)
		}
	case protocol.MsgResize:
		if ec.onResize != nil {
			resize, err := protocol.ParseResizePayload(msg.Payload)
			if err == nil {
				ec.onResize(resize.Rows, resize.Cols)
			}
		}
	case protocol.MsgPing:
		// Respond with pong
		ec.sendMessage(protocol.NewPongMessage())
	case protocol.MsgClose:
		ec.Close()
	}
}

// sendMessage encrypts and sends a protocol message
func (ec *EncryptedChannel) sendMessage(msg *protocol.Message) error {
	ec.mu.Lock()
	if ec.closed {
		ec.mu.Unlock()
		return io.ErrClosedPipe
	}
	ec.mu.Unlock()

	encoded := msg.Encode()
	encrypted, err := crypto.Encrypt(encoded, ec.key)
	if err != nil {
		return err
	}

	return ec.dc.Send(encrypted)
}

// SendData sends terminal data
func (ec *EncryptedChannel) SendData(data []byte) error {
	return ec.sendMessage(protocol.NewDataMessage(data))
}

// SendResize sends a resize event
func (ec *EncryptedChannel) SendResize(rows, cols uint16) error {
	return ec.sendMessage(protocol.NewResizeMessage(rows, cols))
}

// SendClose sends a graceful close message
func (ec *EncryptedChannel) SendClose() error {
	return ec.sendMessage(protocol.NewCloseMessage())
}

// OnData sets the handler for terminal data
func (ec *EncryptedChannel) OnData(handler func([]byte)) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.onData = handler
}

// OnResize sets the handler for resize events
func (ec *EncryptedChannel) OnResize(handler func(rows, cols uint16)) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.onResize = handler
}

// OnClose sets the handler for close events
func (ec *EncryptedChannel) OnClose(handler func()) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.onClose = handler
}

// Close closes the data channel
func (ec *EncryptedChannel) Close() error {
	ec.mu.Lock()
	if ec.closed {
		ec.mu.Unlock()
		return nil
	}
	ec.closed = true
	ec.mu.Unlock()

	// Try to send close message before closing
	ec.sendMessage(protocol.NewCloseMessage())
	return ec.dc.Close()
}

// Ready returns true if the data channel is open
func (ec *EncryptedChannel) Ready() bool {
	return ec.dc.ReadyState() == webrtc.DataChannelStateOpen
}

// Label returns the data channel label
func (ec *EncryptedChannel) Label() string {
	return ec.dc.Label()
}
