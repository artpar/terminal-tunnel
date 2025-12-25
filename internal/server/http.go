package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// SignalingServer handles WebRTC signaling over HTTP
type SignalingServer struct {
	listener   net.Listener
	server     *http.Server
	offer      string
	sessionID  string
	salt       string // base64 encoded salt for key derivation
	answerChan chan string
	staticFS   embed.FS

	mu     sync.Mutex
	closed bool
}

// AnswerPayload is the JSON structure for answer submission
type AnswerPayload struct {
	Answer string `json:"answer"`
}

// NewSignalingServer creates a new signaling server
func NewSignalingServer(offer string, sessionID string, salt string, staticFS embed.FS) (*SignalingServer, error) {
	// Find an available port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, fmt.Errorf("failed to find available port: %w", err)
	}

	s := &SignalingServer{
		listener:   listener,
		offer:      offer,
		sessionID:  sessionID,
		salt:       salt,
		answerChan: make(chan string, 1),
		staticFS:   staticFS,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/offer", s.handleOffer)
	mux.HandleFunc("/answer", s.handleAnswer)
	mux.HandleFunc("/health", s.handleHealth)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s, nil
}

// Start begins serving HTTP requests
func (s *SignalingServer) Start() error {
	go s.server.Serve(s.listener)
	return nil
}

// Port returns the port the server is listening on
func (s *SignalingServer) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// WaitForAnswer waits for the client to submit an answer
func (s *SignalingServer) WaitForAnswer(timeout time.Duration) (string, error) {
	select {
	case answer := <-s.answerChan:
		return answer, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for answer")
	}
}

// Close shuts down the signaling server
func (s *SignalingServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// handleIndex serves the web client
func (s *SignalingServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Try to serve embedded static file
	content, err := s.staticFS.ReadFile("static/index.html")
	if err != nil {
		// Fallback to minimal HTML
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Terminal Tunnel</title></head>
<body>
<h1>Terminal Tunnel</h1>
<p>Session ID: %s</p>
<p>Use the client app to connect.</p>
</body>
</html>`, s.sessionID)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// handleOffer returns the SDP offer
func (s *SignalingServer) handleOffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	json.NewEncoder(w).Encode(map[string]string{
		"offer":     s.offer,
		"sessionId": s.sessionID,
		"salt":      s.salt,
	})
}

// handleAnswer receives the SDP answer from the client
func (s *SignalingServer) handleAnswer(w http.ResponseWriter, r *http.Request) {
	// Handle CORS preflight
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var payload AnswerPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if payload.Answer == "" {
		http.Error(w, "Answer is required", http.StatusBadRequest)
		return
	}

	// Send answer through channel (non-blocking)
	select {
	case s.answerChan <- payload.Answer:
	default:
		// Answer already received
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleHealth is a simple health check endpoint
func (s *SignalingServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// GetLocalIP attempts to get the local IP address
func GetLocalIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}
