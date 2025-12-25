// Package relayserver implements a WebSocket relay server for SDP exchange
package relayserver

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/artpar/terminal-tunnel/internal/signaling"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for relay
	},
}

// Session represents a signaling session between host and client
type Session struct {
	ID         string
	HostConn   *websocket.Conn
	ClientConn *websocket.Conn
	Offer      string
	Salt       string
	Created    time.Time
	mu         sync.Mutex
}

// RelayServer is a WebSocket relay server for SDP exchange
type RelayServer struct {
	sessions   map[string]*Session
	mu         sync.RWMutex
	expiration time.Duration
}

// NewRelayServer creates a new relay server
func NewRelayServer() *RelayServer {
	rs := &RelayServer{
		sessions:   make(map[string]*Session),
		expiration: 5 * time.Minute,
	}

	// Start session cleanup goroutine
	go rs.cleanupLoop()

	return rs
}

// cleanupLoop periodically removes expired sessions
func (rs *RelayServer) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rs.mu.Lock()
		now := time.Now()
		for id, session := range rs.sessions {
			if now.Sub(session.Created) > rs.expiration {
				session.mu.Lock()
				if session.HostConn != nil {
					session.HostConn.Close()
				}
				if session.ClientConn != nil {
					session.ClientConn.Close()
				}
				session.mu.Unlock()
				delete(rs.sessions, id)
				log.Printf("Session %s expired", id)
			}
		}
		rs.mu.Unlock()
	}
}

// HandleWebSocket handles WebSocket connections
func (rs *RelayServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Get session ID from query parameter
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		conn.WriteJSON(signaling.RelayMessage{
			Type:  signaling.MsgTypeError,
			Error: "session parameter required",
		})
		conn.Close()
		return
	}

	// Handle messages
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			rs.handleDisconnect(sessionID, conn)
			return
		}

		var msg signaling.RelayMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		rs.handleMessage(conn, sessionID, msg)
	}
}

func (rs *RelayServer) handleMessage(conn *websocket.Conn, sessionID string, msg signaling.RelayMessage) {
	switch msg.Type {
	case signaling.MsgTypeRegister:
		rs.handleRegister(conn, sessionID, msg.Role)

	case signaling.MsgTypeOffer:
		rs.handleOffer(sessionID, msg.SDP, msg.Salt)

	case signaling.MsgTypeAnswer:
		rs.handleAnswer(sessionID, msg.SDP)
	}
}

func (rs *RelayServer) handleRegister(conn *websocket.Conn, sessionID, role string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	session, exists := rs.sessions[sessionID]
	if !exists {
		session = &Session{
			ID:      sessionID,
			Created: time.Now(),
		}
		rs.sessions[sessionID] = session
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if role == signaling.RoleHost {
		session.HostConn = conn
		log.Printf("Host registered for session %s", sessionID)
	} else if role == signaling.RoleClient {
		session.ClientConn = conn
		log.Printf("Client registered for session %s", sessionID)

		// If we have an offer, send it to the client
		if session.Offer != "" {
			conn.WriteJSON(signaling.RelayMessage{
				Type:      signaling.MsgTypeOffer,
				SessionID: sessionID,
				SDP:       session.Offer,
				Salt:      session.Salt,
			})
		}
	}
}

func (rs *RelayServer) handleOffer(sessionID, sdp, salt string) {
	rs.mu.RLock()
	session, exists := rs.sessions[sessionID]
	rs.mu.RUnlock()

	if !exists {
		return
	}

	session.mu.Lock()
	session.Offer = sdp
	session.Salt = salt

	// If client is already connected, forward the offer
	if session.ClientConn != nil {
		session.ClientConn.WriteJSON(signaling.RelayMessage{
			Type:      signaling.MsgTypeOffer,
			SessionID: sessionID,
			SDP:       sdp,
			Salt:      salt,
		})
	}
	session.mu.Unlock()

	log.Printf("Offer stored for session %s", sessionID)
}

func (rs *RelayServer) handleAnswer(sessionID, sdp string) {
	rs.mu.RLock()
	session, exists := rs.sessions[sessionID]
	rs.mu.RUnlock()

	if !exists {
		return
	}

	session.mu.Lock()
	// Forward answer to host
	if session.HostConn != nil {
		session.HostConn.WriteJSON(signaling.RelayMessage{
			Type:      signaling.MsgTypeAnswer,
			SessionID: sessionID,
			SDP:       sdp,
		})
	}
	session.mu.Unlock()

	log.Printf("Answer forwarded for session %s", sessionID)

	// Clean up session after successful exchange
	go func() {
		time.Sleep(5 * time.Second)
		rs.mu.Lock()
		delete(rs.sessions, sessionID)
		rs.mu.Unlock()
		log.Printf("Session %s completed and cleaned up", sessionID)
	}()
}

func (rs *RelayServer) handleDisconnect(sessionID string, conn *websocket.Conn) {
	rs.mu.RLock()
	session, exists := rs.sessions[sessionID]
	rs.mu.RUnlock()

	if !exists {
		conn.Close()
		return
	}

	session.mu.Lock()
	if session.HostConn == conn {
		session.HostConn = nil
		log.Printf("Host disconnected from session %s", sessionID)
	} else if session.ClientConn == conn {
		session.ClientConn = nil
		log.Printf("Client disconnected from session %s", sessionID)
	}
	session.mu.Unlock()

	conn.Close()
}

// Start starts the relay server on the given port
func (rs *RelayServer) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", rs.HandleWebSocket)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Relay server starting on %s", addr)

	return http.ListenAndServe(addr, mux)
}
