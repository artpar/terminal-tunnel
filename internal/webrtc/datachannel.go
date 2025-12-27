package webrtc

import (
	"io"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
	"github.com/artpar/terminal-tunnel/internal/protocol"
)

const (
	// PingInterval is how often the server sends pings to the client
	PingInterval = 10 * time.Second
	// PongTimeout is how long to wait for a pong before considering connection dead
	PongTimeout = 30 * time.Second
)

// EncryptedChannel wraps a WebRTC DataChannel with encryption and protocol handling
type EncryptedChannel struct {
	dc     *webrtc.DataChannel
	key    *[32]byte
	altKey *[32]byte // Alternate key (PBKDF2 fallback for CSP-restricted browsers)

	onData   func([]byte)
	onResize func(rows, cols uint16)
	onClose  func()

	mu        sync.Mutex
	closed    bool
	useAltKey bool // True if client is using altKey (PBKDF2)

	// Keepalive tracking
	lastPongTime  time.Time
	pingTicker    *time.Ticker
	pongCheckDone chan struct{}
}

// NewEncryptedChannel creates an encrypted wrapper for a DataChannel
func NewEncryptedChannel(dc *webrtc.DataChannel, key *[32]byte) *EncryptedChannel {
	ec := &EncryptedChannel{
		dc:           dc,
		key:          key,
		lastPongTime: time.Now(), // Initialize to now, assume connection is fresh
	}

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		ec.handleMessage(msg.Data)
	})

	dc.OnClose(func() {
		ec.mu.Lock()
		ec.closed = true
		handler := ec.onClose
		ec.mu.Unlock()
		ec.StopKeepalive() // Stop keepalive when channel closes
		if handler != nil {
			handler()
		}
	})

	return ec
}

// SetAltKey sets an alternate key for fallback decryption (PBKDF2 for CSP-restricted browsers)
func (ec *EncryptedChannel) SetAltKey(altKey *[32]byte) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.altKey = altKey
}

// handleMessage decrypts and processes incoming messages
func (ec *EncryptedChannel) handleMessage(data []byte) {
	// Try primary key first (Argon2)
	plaintext, err := crypto.Decrypt(data, ec.key)
	usedAltKey := false
	if err != nil {
		// Try alternate key (PBKDF2 fallback)
		ec.mu.Lock()
		altKey := ec.altKey
		ec.mu.Unlock()
		if altKey != nil {
			plaintext, err = crypto.Decrypt(data, altKey)
			if err == nil {
				usedAltKey = true
				// Client is using PBKDF2, remember this for responses
				ec.mu.Lock()
				ec.useAltKey = true
				ec.mu.Unlock()
			}
		}
		if err != nil {
			// Both keys failed - likely wrong password or corrupted data
			return
		}
	}
	_ = usedAltKey // Used for logging if needed

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
		// Respond with pong (ignore error - best effort response)
		_ = ec.sendMessage(protocol.NewPongMessage())
	case protocol.MsgPong:
		// Update last pong time for keepalive tracking
		ec.mu.Lock()
		ec.lastPongTime = time.Now()
		ec.mu.Unlock()
	case protocol.MsgClose:
		_ = ec.Close() // Ignore error on remote-initiated close
	}
}

// sendMessage encrypts and sends a protocol message
func (ec *EncryptedChannel) sendMessage(msg *protocol.Message) error {
	ec.mu.Lock()
	if ec.closed {
		ec.mu.Unlock()
		return io.ErrClosedPipe
	}
	useAlt := ec.useAltKey
	altKey := ec.altKey
	ec.mu.Unlock()

	encoded := msg.Encode()

	// Use the same key the client is using
	key := ec.key
	if useAlt && altKey != nil {
		key = altKey
	}

	encrypted, err := crypto.Encrypt(encoded, key)
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

	// Try to send close message before closing (ignore error - best effort)
	_ = ec.sendMessage(protocol.NewCloseMessage())
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

// StartKeepalive begins sending pings and monitoring for pong timeouts
// Returns a channel that will receive true if the connection times out
func (ec *EncryptedChannel) StartKeepalive() <-chan struct{} {
	ec.mu.Lock()
	if ec.pingTicker != nil {
		ec.mu.Unlock()
		return ec.pongCheckDone
	}

	ticker := time.NewTicker(PingInterval)
	ec.pingTicker = ticker
	pongCheckDone := make(chan struct{})
	ec.pongCheckDone = pongCheckDone
	ec.lastPongTime = time.Now()
	ec.mu.Unlock()

	timeoutChan := make(chan struct{})

	go func() {
		defer close(timeoutChan)
		for {
			select {
			case <-pongCheckDone: // Use local variable to avoid race with StopKeepalive
				return
			case <-ticker.C: // Use local variable to avoid race with StopKeepalive
				// Send ping
				ec.mu.Lock()
				closed := ec.closed
				ec.mu.Unlock()

				if closed {
					return
				}

				if err := ec.sendMessage(protocol.NewPingMessage()); err != nil {
					// Send failed, connection likely dead
					return
				}

				// Check if we've received a pong recently
				ec.mu.Lock()
				lastPong := ec.lastPongTime
				ec.mu.Unlock()

				if time.Since(lastPong) > PongTimeout {
					// Connection timed out - no pong received
					return
				}
			}
		}
	}()

	return timeoutChan
}

// StopKeepalive stops the ping/pong keepalive mechanism
func (ec *EncryptedChannel) StopKeepalive() {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	if ec.pingTicker != nil {
		ec.pingTicker.Stop()
		ec.pingTicker = nil
	}
	if ec.pongCheckDone != nil {
		select {
		case <-ec.pongCheckDone:
			// Already closed
		default:
			close(ec.pongCheckDone)
		}
		ec.pongCheckDone = nil
	}
}

// SendPing sends a ping message (used by client-side keepalive)
func (ec *EncryptedChannel) SendPing() error {
	return ec.sendMessage(protocol.NewPingMessage())
}
