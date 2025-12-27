// Package relayserver implements a WebSocket relay server for SDP exchange
package relayserver

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/artpar/terminal-tunnel/internal/signaling"
)

// Short code alphabet (no ambiguous chars: 0/O, 1/I/l)
const codeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// Security: 8 chars = 31^8 = 852 billion possibilities (vs 31^6 = 887 million)
const codeLength = 8

// Rate limiting constants
const (
	rateLimitWindow   = 1 * time.Minute
	maxRequestsPerIP  = 30 // Max requests per IP per minute
	rateLimitCleanup  = 5 * time.Minute
)

// RateLimiter tracks request rates per IP
type RateLimiter struct {
	requests map[string][]time.Time
	mu       sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
	}
	go rl.cleanupLoop()
	return rl
}

// Allow checks if a request from the given IP should be allowed
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rateLimitWindow)

	// Filter to only recent requests
	recent := make([]time.Time, 0)
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= maxRequestsPerIP {
		rl.requests[ip] = recent
		return false
	}

	rl.requests[ip] = append(recent, now)
	return true
}

// cleanupLoop periodically removes old entries
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rateLimitCleanup)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-rateLimitWindow)
		for ip, times := range rl.requests {
			recent := make([]time.Time, 0)
			for _, t := range times {
				if t.After(cutoff) {
					recent = append(recent, t)
				}
			}
			if len(recent) == 0 {
				delete(rl.requests, ip)
			} else {
				rl.requests[ip] = recent
			}
		}
		rl.mu.Unlock()
	}
}

// Allowed CORS origins (security: prevent arbitrary websites from accessing API)
var allowedOrigins = map[string]bool{
	"https://artpar.github.io":   true,
	"http://localhost":           true,
	"http://localhost:8080":      true,
	"http://127.0.0.1":           true,
	"http://127.0.0.1:8080":      true,
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Allow non-browser clients
		}
		return allowedOrigins[origin]
	},
}

// Session represents a signaling session between host and client
type Session struct {
	ID           string
	ShortCode    string
	HostConn     *websocket.Conn
	ClientConn   *websocket.Conn
	Offer        string
	Answer       string
	Salt         string
	Created      time.Time
	LastActivity time.Time // Last activity time for expiry calculation
	AnswerChan   chan string // Channel to notify host of answer
	mu           sync.Mutex
}

// SessionRequest is the request body for creating a session
type SessionRequest struct {
	SDP  string `json:"sdp"`
	Salt string `json:"salt"`
}

// SessionResponse is the response for session creation
type SessionResponse struct {
	Code      string `json:"code"`
	ExpiresIn int    `json:"expires_in"`
	URL       string `json:"url,omitempty"`
}

// SessionInfo is returned when fetching a session
type SessionInfo struct {
	SDP  string `json:"sdp"`
	Salt string `json:"salt"`
}

// AnswerRequest is the request body for submitting an answer
type AnswerRequest struct {
	SDP string `json:"sdp"`
}

// generateShortCode creates a random short code
func generateShortCode() string {
	code := make([]byte, codeLength)
	alphabetLen := big.NewInt(int64(len(codeAlphabet)))
	for i := range code {
		n, _ := rand.Int(rand.Reader, alphabetLen)
		code[i] = codeAlphabet[n.Int64()]
	}
	return string(code)
}

// RelayServer is a WebSocket relay server for SDP exchange
type RelayServer struct {
	sessions    map[string]*Session
	shortCodes  map[string]*Session // maps short code to session
	mu          sync.RWMutex
	expiration  time.Duration
	publicURL   string // Public URL for generating client links
	rateLimiter *RateLimiter
}

// NewRelayServer creates a new relay server
func NewRelayServer() *RelayServer {
	rs := &RelayServer{
		sessions:    make(map[string]*Session),
		shortCodes:  make(map[string]*Session),
		expiration:  5 * time.Minute,
		rateLimiter: NewRateLimiter(),
	}

	// Start session cleanup goroutine
	go rs.cleanupLoop()

	return rs
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// setCORSHeaders sets CORS headers based on origin whitelist
func setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" && allowedOrigins[origin] {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	} else if origin == "" {
		// For non-browser clients, allow all
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// SetPublicURL sets the public URL for generating client links
func (rs *RelayServer) SetPublicURL(url string) {
	rs.publicURL = strings.TrimSuffix(url, "/")
}

// cleanupLoop periodically removes expired sessions
// Sessions expire based on LastActivity, not creation time
func (rs *RelayServer) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rs.mu.Lock()
		now := time.Now()
		for id, session := range rs.sessions {
			session.mu.Lock()
			timeSinceActivity := now.Sub(session.LastActivity)
			session.mu.Unlock()

			if timeSinceActivity > rs.expiration {
				session.mu.Lock()
				if session.HostConn != nil {
					session.HostConn.Close()
				}
				if session.ClientConn != nil {
					session.ClientConn.Close()
				}
				if session.AnswerChan != nil {
					close(session.AnswerChan)
				}
				session.mu.Unlock()
				delete(rs.sessions, id)
				if session.ShortCode != "" {
					delete(rs.shortCodes, session.ShortCode)
				}
				log.Printf("Session %s expired (inactive for %v)", id, timeSinceActivity.Round(time.Second))
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

// HandleCreateSession handles POST /session - creates a new session with short code
func (rs *RelayServer) HandleCreateSession(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Rate limiting
	clientIP := getClientIP(r)
	if !rs.rateLimiter.Allow(clientIP) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		log.Printf("Rate limit exceeded for IP: %s", clientIP)
		return
	}

	var req SessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.SDP == "" {
		http.Error(w, "SDP required", http.StatusBadRequest)
		return
	}

	// Generate unique short code
	rs.mu.Lock()
	var code string
	for {
		code = generateShortCode()
		if _, exists := rs.shortCodes[code]; !exists {
			break
		}
	}

	now := time.Now()
	session := &Session{
		ID:           code,
		ShortCode:    code,
		Offer:        req.SDP,
		Salt:         req.Salt,
		Created:      now,
		LastActivity: now,
		AnswerChan:   make(chan string, 1),
	}
	rs.sessions[code] = session
	rs.shortCodes[code] = session
	rs.mu.Unlock()

	log.Printf("Session created with code %s from IP %s", code, clientIP)

	// Build response
	resp := SessionResponse{
		Code:      code,
		ExpiresIn: int(rs.expiration.Seconds()),
	}
	if rs.publicURL != "" {
		resp.URL = fmt.Sprintf("%s/?c=%s", rs.publicURL, code)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleGetSession handles GET /session/{code} - retrieves session SDP
func (rs *RelayServer) HandleGetSession(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Rate limiting
	clientIP := getClientIP(r)
	if !rs.rateLimiter.Allow(clientIP) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Extract code from path: /session/ABC123
	path := strings.TrimPrefix(r.URL.Path, "/session/")
	code := strings.ToUpper(strings.TrimSuffix(path, "/answer"))

	rs.mu.RLock()
	session, exists := rs.shortCodes[code]
	rs.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	session.mu.Lock()
	// Update last activity on access
	session.LastActivity = time.Now()
	resp := SessionInfo{
		SDP:  session.Offer,
		Salt: session.Salt,
	}
	session.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleSessionHeartbeat handles PATCH /session/{code} - keeps session alive
func (rs *RelayServer) HandleSessionHeartbeat(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract code from path: /session/ABC123
	path := strings.TrimPrefix(r.URL.Path, "/session/")
	code := strings.ToUpper(path)

	rs.mu.RLock()
	session, exists := rs.shortCodes[code]
	rs.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	session.mu.Lock()
	session.LastActivity = time.Now()
	session.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleUpdateSession handles PUT /session/{code} - updates session SDP for reconnection
func (rs *RelayServer) HandleUpdateSession(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Rate limiting
	clientIP := getClientIP(r)
	if !rs.rateLimiter.Allow(clientIP) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Extract code from path: /session/ABC123
	path := strings.TrimPrefix(r.URL.Path, "/session/")
	code := strings.ToUpper(path)

	var req SessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.SDP == "" {
		http.Error(w, "SDP required", http.StatusBadRequest)
		return
	}

	rs.mu.RLock()
	session, exists := rs.shortCodes[code]
	rs.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	session.mu.Lock()
	session.Offer = req.SDP
	if req.Salt != "" {
		session.Salt = req.Salt
	}
	session.Answer = "" // Clear old answer for new connection
	session.LastActivity = time.Now()

	// Create new answer channel for the new connection
	if session.AnswerChan != nil {
		select {
		case <-session.AnswerChan: // Drain if anything there
		default:
		}
	} else {
		session.AnswerChan = make(chan string, 1)
	}
	session.mu.Unlock()

	log.Printf("Session %s updated for reconnection from IP %s", code, clientIP)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleSubmitAnswer handles POST /session/{code}/answer - submits answer SDP
func (rs *RelayServer) HandleSubmitAnswer(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Rate limiting
	clientIP := getClientIP(r)
	if !rs.rateLimiter.Allow(clientIP) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Extract code from path: /session/ABC123/answer
	path := strings.TrimPrefix(r.URL.Path, "/session/")
	code := strings.ToUpper(strings.TrimSuffix(path, "/answer"))

	var req AnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	rs.mu.RLock()
	session, exists := rs.shortCodes[code]
	rs.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	session.mu.Lock()
	session.Answer = req.SDP

	// Notify via WebSocket if host is connected
	if session.HostConn != nil {
		session.HostConn.WriteJSON(signaling.RelayMessage{
			Type:      signaling.MsgTypeAnswer,
			SessionID: session.ID,
			SDP:       req.SDP,
		})
	}

	// Also send to answer channel for polling
	select {
	case session.AnswerChan <- req.SDP:
	default:
	}
	session.mu.Unlock()

	log.Printf("Answer submitted for session %s", code)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandlePollAnswer handles GET /session/{code}/answer - polls for answer (long-polling)
func (rs *RelayServer) HandlePollAnswer(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Rate limiting (but more lenient for polling)
	clientIP := getClientIP(r)
	if !rs.rateLimiter.Allow(clientIP) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Extract code from path
	path := strings.TrimPrefix(r.URL.Path, "/session/")
	code := strings.ToUpper(strings.TrimSuffix(path, "/answer"))

	rs.mu.RLock()
	session, exists := rs.shortCodes[code]
	rs.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Check if answer already exists
	session.mu.Lock()
	if session.Answer != "" {
		answer := session.Answer
		session.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"sdp": answer})
		return
	}
	answerChan := session.AnswerChan
	session.mu.Unlock()

	// Long-poll: wait up to 30 seconds for answer
	select {
	case answer := <-answerChan:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"sdp": answer})
	case <-time.After(30 * time.Second):
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "waiting"})
	case <-r.Context().Done():
		return
	}
}

// sessionHandler routes /session/* requests
func (rs *RelayServer) sessionHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)

	// Handle preflight for all session endpoints
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, OPTIONS")
		w.WriteHeader(http.StatusOK)
		return
	}

	path := r.URL.Path

	// POST /session - create new session
	if path == "/session" || path == "/session/" {
		rs.HandleCreateSession(w, r)
		return
	}

	// /session/{code}/answer
	if strings.HasSuffix(path, "/answer") {
		if r.Method == http.MethodPost {
			rs.HandleSubmitAnswer(w, r)
		} else {
			rs.HandlePollAnswer(w, r)
		}
		return
	}

	// PUT /session/{code} - update session for reconnection
	if r.Method == http.MethodPut {
		rs.HandleUpdateSession(w, r)
		return
	}

	// PATCH /session/{code} - heartbeat to keep session alive
	if r.Method == http.MethodPatch {
		rs.HandleSessionHeartbeat(w, r)
		return
	}

	// GET /session/{code}
	rs.HandleGetSession(w, r)
}

// Start starts the relay server on the given port
func (rs *RelayServer) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", rs.HandleWebSocket)
	mux.HandleFunc("/session", rs.sessionHandler)
	mux.HandleFunc("/session/", rs.sessionHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Relay server starting on %s", addr)
	log.Printf("Endpoints:")
	log.Printf("  POST /session - Create session, get short code")
	log.Printf("  GET  /session/{code} - Get session SDP")
	log.Printf("  POST /session/{code}/answer - Submit answer")
	log.Printf("  GET  /session/{code}/answer - Poll for answer")
	log.Printf("  WS   /ws?session={code} - WebSocket connection")

	return http.ListenAndServe(addr, mux)
}
