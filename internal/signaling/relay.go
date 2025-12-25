package signaling

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// RelayClient connects to a WebSocket relay server for SDP exchange
type RelayClient struct {
	relayURL  string
	sessionID string
	salt      string
	conn      *websocket.Conn
	mu        sync.Mutex
	closed    bool
}

// NewRelayClient creates a new relay client
func NewRelayClient(relayURL, sessionID, salt string) *RelayClient {
	return &RelayClient{
		relayURL:  relayURL,
		sessionID: sessionID,
		salt:      salt,
	}
}

// ConnectAsHost connects to the relay as a host and sends the offer
func (r *RelayClient) ConnectAsHost(offer string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Parse and validate URL
	u, err := url.Parse(r.relayURL)
	if err != nil {
		return fmt.Errorf("invalid relay URL: %w", err)
	}

	// Add session ID to query params
	q := u.Query()
	q.Set("session", r.sessionID)
	u.RawQuery = q.Encode()

	// Connect to WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to relay: %w", err)
	}
	r.conn = conn

	// Register as host
	regMsg := RelayMessage{
		Type:      MsgTypeRegister,
		SessionID: r.sessionID,
		Role:      RoleHost,
	}
	if err := r.sendMessage(regMsg); err != nil {
		r.conn.Close()
		return fmt.Errorf("failed to register: %w", err)
	}

	// Send offer
	offerMsg := RelayMessage{
		Type:      MsgTypeOffer,
		SessionID: r.sessionID,
		SDP:       offer,
		Salt:      r.salt,
	}
	if err := r.sendMessage(offerMsg); err != nil {
		r.conn.Close()
		return fmt.Errorf("failed to send offer: %w", err)
	}

	return nil
}

// WaitForAnswer waits for an answer from a client
func (r *RelayClient) WaitForAnswer(timeout time.Duration) (string, error) {
	if r.conn == nil {
		return "", fmt.Errorf("not connected")
	}

	r.conn.SetReadDeadline(time.Now().Add(timeout))

	for {
		_, data, err := r.conn.ReadMessage()
		if err != nil {
			return "", fmt.Errorf("failed to read message: %w", err)
		}

		var msg RelayMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // Skip malformed messages
		}

		switch msg.Type {
		case MsgTypeAnswer:
			return msg.SDP, nil
		case MsgTypeError:
			return "", fmt.Errorf("relay error: %s", msg.Error)
		}
	}
}

// ConnectAsClient connects to the relay as a client and retrieves the offer
func (r *RelayClient) ConnectAsClient(sessionID string) (offer, salt string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessionID = sessionID

	// Parse and validate URL
	u, err := url.Parse(r.relayURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid relay URL: %w", err)
	}

	// Add session ID to query params
	q := u.Query()
	q.Set("session", sessionID)
	u.RawQuery = q.Encode()

	// Connect to WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to connect to relay: %w", err)
	}
	r.conn = conn

	// Register as client
	regMsg := RelayMessage{
		Type:      MsgTypeRegister,
		SessionID: sessionID,
		Role:      RoleClient,
	}
	if err := r.sendMessage(regMsg); err != nil {
		r.conn.Close()
		return "", "", fmt.Errorf("failed to register: %w", err)
	}

	// Wait for offer
	r.conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	for {
		_, data, err := r.conn.ReadMessage()
		if err != nil {
			r.conn.Close()
			return "", "", fmt.Errorf("failed to read offer: %w", err)
		}

		var msg RelayMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case MsgTypeOffer:
			return msg.SDP, msg.Salt, nil
		case MsgTypeError:
			r.conn.Close()
			return "", "", fmt.Errorf("relay error: %s", msg.Error)
		}
	}
}

// SendAnswer sends an answer to the host
func (r *RelayClient) SendAnswer(answer string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := RelayMessage{
		Type:      MsgTypeAnswer,
		SessionID: r.sessionID,
		SDP:       answer,
	}
	return r.sendMessage(msg)
}

// Close closes the connection
func (r *RelayClient) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

func (r *RelayClient) sendMessage(msg RelayMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return r.conn.WriteMessage(websocket.TextMessage, data)
}
