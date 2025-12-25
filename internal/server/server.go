package server

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/skip2/go-qrcode"

	"github.com/artpar/terminal-tunnel/internal/crypto"
	"github.com/artpar/terminal-tunnel/internal/signaling"
	ttwebrtc "github.com/artpar/terminal-tunnel/internal/webrtc"
	"github.com/artpar/terminal-tunnel/internal/web"
)

// Options configures the terminal tunnel server
type Options struct {
	Password string
	Shell    string
	Timeout  time.Duration
	RelayURL string // WebSocket relay URL for signaling
	NoRelay  bool   // Disable relay, use manual if UPnP fails
	Manual   bool   // Force manual (QR/copy-paste) signaling mode
}

// DefaultOptions returns sensible defaults
func DefaultOptions() Options {
	return Options{
		Shell:   "",
		Timeout: 5 * time.Minute,
	}
}

// Server orchestrates the terminal tunnel
type Server struct {
	opts         Options
	peer         *ttwebrtc.Peer
	signaling    *SignalingServer
	relayClient  *signaling.RelayClient
	pty          *PTY
	bridge       *Bridge
	channel      *ttwebrtc.EncryptedChannel
	salt         []byte
	key          [32]byte
	sessionID    string
	upnpClose    func() error
}

// NewServer creates a new terminal tunnel server
func NewServer(opts Options) (*Server, error) {
	// Generate salt for key derivation
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive encryption key
	key := crypto.DeriveKey(opts.Password, salt)

	// Generate session ID
	sessionID := generateSessionID()

	return &Server{
		opts:      opts,
		salt:      salt,
		key:       key,
		sessionID: sessionID,
	}, nil
}

// generateSessionID creates a unique session identifier
func generateSessionID() string {
	salt, _ := crypto.GenerateSalt()
	return base64.RawURLEncoding.EncodeToString(salt)[:8]
}

// Start initializes and runs the terminal tunnel
func (s *Server) Start() error {
	// Create WebRTC peer
	peer, err := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	if err != nil {
		return fmt.Errorf("failed to create peer: %w", err)
	}
	s.peer = peer

	// Create data channel
	dc, err := peer.CreateDataChannel("terminal")
	if err != nil {
		return fmt.Errorf("failed to create data channel: %w", err)
	}

	// Create SDP offer
	offer, err := peer.CreateOffer()
	if err != nil {
		return fmt.Errorf("failed to create offer: %w", err)
	}

	// Get public IP from STUN (for display purposes)
	publicIP := peer.GetPublicIP()
	if publicIP != "" {
		fmt.Printf("✓ Public IP discovered via STUN: %s\n", publicIP)
	}

	saltB64 := base64.StdEncoding.EncodeToString(s.salt)

	// Determine signaling method
	sigMethod := s.determineSignalingMethod()
	fmt.Printf("Using signaling method: %s\n", sigMethod)

	var answer string

	switch sigMethod {
	case signaling.MethodHTTP:
		answer, err = s.startHTTPSignaling(offer, saltB64)
	case signaling.MethodRelay:
		answer, err = s.startRelaySignaling(offer, saltB64)
	case signaling.MethodManual:
		answer, err = s.startManualSignaling(offer)
	case signaling.MethodShortCode:
		answer, err = s.startShortCodeSignaling(offer, saltB64)
	}

	if err != nil {
		return err
	}

	fmt.Printf("✓ Received client answer\n")

	// Set remote description
	if err := peer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer); err != nil {
		return fmt.Errorf("failed to set answer: %w", err)
	}

	// Wait for data channel to open
	dcOpen := make(chan bool)
	dc.OnOpen(func() {
		dcOpen <- true
	})

	select {
	case <-dcOpen:
		fmt.Printf("✓ Data channel connected\n")
	case <-time.After(30 * time.Second):
		return fmt.Errorf("data channel connection timeout")
	}

	// Close signaling server - no longer needed
	if s.signaling != nil {
		s.signaling.Close()
	}

	// Start PTY
	pty, err := StartPTY(s.opts.Shell)
	if err != nil {
		return fmt.Errorf("failed to start PTY: %w", err)
	}
	s.pty = pty

	fmt.Printf("✓ Terminal session started\n")
	fmt.Printf("\n")

	// Create encrypted channel
	channel := ttwebrtc.NewEncryptedChannel(dc, &s.key)
	s.channel = channel

	// Create bridge
	bridge := NewBridge(pty, channel.SendData)
	s.bridge = bridge

	// Handle incoming data
	channel.OnData(func(data []byte) {
		bridge.HandleData(data)
	})

	channel.OnResize(func(rows, cols uint16) {
		bridge.HandleResize(rows, cols)
	})

	channel.OnClose(func() {
		fmt.Printf("\n✓ Client disconnected\n")
		s.Stop()
	})

	// Start bridge (PTY -> channel)
	bridge.Start()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination
	<-sigChan
	fmt.Printf("\n\nShutting down...\n")

	return s.Stop()
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	if s.bridge != nil {
		s.bridge.Close()
	}
	if s.channel != nil {
		s.channel.Close()
	}
	if s.pty != nil {
		s.pty.Close()
	}
	if s.signaling != nil {
		s.signaling.Close()
	}
	if s.relayClient != nil {
		s.relayClient.Close()
	}
	if s.peer != nil {
		s.peer.Close()
	}
	if s.upnpClose != nil {
		s.upnpClose()
	}
	return nil
}

// determineSignalingMethod decides which signaling method to use
func (s *Server) determineSignalingMethod() signaling.SignalingMethod {
	// If manual mode is forced, use it
	if s.opts.Manual {
		return signaling.MethodManual
	}

	// If relay URL is set and not disabled, use short code mode (default)
	if s.opts.RelayURL != "" && !s.opts.NoRelay {
		return signaling.MethodShortCode
	}

	// If no relay configured but not disabled, use default public relay
	if !s.opts.NoRelay {
		s.opts.RelayURL = signaling.DefaultRelayURL
		return signaling.MethodShortCode
	}

	// Default to HTTP (with UPnP attempt)
	return signaling.MethodHTTP
}

// startHTTPSignaling uses the HTTP server for signaling (with UPnP)
func (s *Server) startHTTPSignaling(offer, saltB64 string) (string, error) {
	// Start signaling server
	sig, err := NewSignalingServer(offer, s.sessionID, saltB64, web.StaticFS)
	if err != nil {
		return "", fmt.Errorf("failed to create signaling server: %w", err)
	}
	s.signaling = sig

	if err := sig.Start(); err != nil {
		return "", fmt.Errorf("failed to start signaling: %w", err)
	}

	port := uint16(sig.Port())

	// Try UPnP port mapping
	localIP, err := GetLocalIP()
	if err != nil {
		localIP = "localhost"
	}

	externalIP := localIP
	upnpMapped := false

	mapping, err := MapPort(port, "Terminal Tunnel")
	if err == nil {
		externalIP = mapping.ExternalIP
		upnpMapped = true
		s.upnpClose = mapping.Close
		fmt.Printf("✓ UPnP port mapping successful\n")
	} else {
		fmt.Printf("⚠ UPnP not available: %v\n", err)
		// If UPnP failed and relay is available, switch to relay
		if s.opts.RelayURL != "" && !s.opts.NoRelay {
			s.signaling.Close()
			s.signaling = nil
			return s.startRelaySignaling(offer, saltB64)
		}
		// If no relay, fall back to manual
		if !upnpMapped {
			s.signaling.Close()
			s.signaling = nil
			return s.startManualSignaling(offer)
		}
	}

	// Display connection info
	url := fmt.Sprintf("http://%s:%d", externalIP, port)
	fmt.Printf("\n")
	fmt.Printf("═══════════════════════════════════════════════════\n")
	fmt.Printf("  Terminal Tunnel Ready!\n")
	fmt.Printf("═══════════════════════════════════════════════════\n")
	fmt.Printf("\n")
	fmt.Printf("  URL: %s\n", url)
	fmt.Printf("  Password: %s\n", s.opts.Password)
	fmt.Printf("\n")

	if !upnpMapped {
		fmt.Printf("  ⚠ Note: Port %d may need manual forwarding\n", port)
		fmt.Printf("  Local URL: http://%s:%d\n", localIP, port)
		fmt.Printf("\n")
	}

	// Generate QR code
	qr, err := qrcode.New(url, qrcode.Medium)
	if err == nil {
		fmt.Print(qr.ToSmallString(false))
	}

	fmt.Printf("\n")
	fmt.Printf("  Waiting for connection... (Ctrl+C to cancel)\n")
	fmt.Printf("\n")

	// Wait for answer
	answer, err := sig.WaitForAnswer(s.opts.Timeout)
	if err != nil {
		return "", fmt.Errorf("failed to receive answer: %w", err)
	}

	return answer, nil
}

// startRelaySignaling uses a WebSocket relay for signaling
func (s *Server) startRelaySignaling(offer, saltB64 string) (string, error) {
	fmt.Printf("\n")
	fmt.Printf("═══════════════════════════════════════════════════\n")
	fmt.Printf("  Terminal Tunnel - Relay Mode\n")
	fmt.Printf("═══════════════════════════════════════════════════\n")
	fmt.Printf("\n")
	fmt.Printf("  Relay: %s\n", s.opts.RelayURL)
	fmt.Printf("  Session ID: %s\n", s.sessionID)
	fmt.Printf("  Password: %s (share separately!)\n", s.opts.Password)
	fmt.Printf("\n")

	// Create relay client
	relay := signaling.NewRelayClient(s.opts.RelayURL, s.sessionID, saltB64)
	s.relayClient = relay

	// Connect and send offer
	if err := relay.ConnectAsHost(offer); err != nil {
		fmt.Printf("⚠ Relay connection failed: %v\n", err)
		fmt.Printf("Falling back to manual mode...\n")
		return s.startManualSignaling(offer)
	}

	fmt.Printf("✓ Connected to relay\n")
	fmt.Printf("  Waiting for client... (Ctrl+C to cancel)\n")
	fmt.Printf("\n")

	// Wait for answer
	answer, err := relay.WaitForAnswer(s.opts.Timeout)
	if err != nil {
		return "", fmt.Errorf("failed to receive answer from relay: %w", err)
	}

	return answer, nil
}

// startManualSignaling uses QR code and copy-paste for signaling
func (s *Server) startManualSignaling(offer string) (string, error) {
	manual := signaling.NewManualSignaling(offer, s.salt)

	// Print instructions with QR code
	manual.PrintInstructions(s.sessionID, s.opts.Password)

	// Read answer from stdin
	answer, err := signaling.ReadAnswer()
	if err != nil {
		return "", fmt.Errorf("failed to read answer: %w", err)
	}

	return answer, nil
}

// startShortCodeSignaling uses the relay HTTP API with short codes
func (s *Server) startShortCodeSignaling(offer, saltB64 string) (string, error) {
	// Create short code client
	client := signaling.NewShortCodeClient(s.opts.RelayURL, signaling.DefaultClientURL)

	// Create session and get short code
	code, err := client.CreateSession(offer, saltB64)
	if err != nil {
		fmt.Printf("⚠ Failed to create session: %v\n", err)
		fmt.Printf("Falling back to manual mode...\n")
		return s.startManualSignaling(offer)
	}

	clientURL := client.GetClientURL()

	// Display connection info
	fmt.Printf("\n")
	fmt.Printf("═══════════════════════════════════════════════════\n")
	fmt.Printf("  Terminal Tunnel Ready!\n")
	fmt.Printf("═══════════════════════════════════════════════════\n")
	fmt.Printf("\n")
	fmt.Printf("  Code: %s\n", code)
	fmt.Printf("  Password: %s\n", s.opts.Password)
	fmt.Printf("\n")
	fmt.Printf("  Or open: %s\n", clientURL)
	fmt.Printf("\n")

	// Generate small QR code for the URL (much smaller than full SDP!)
	qr, err := qrcode.New(clientURL, qrcode.Low)
	if err == nil {
		fmt.Print(qr.ToSmallString(false))
	}

	fmt.Printf("\n")
	fmt.Printf("  Waiting for connection... (Ctrl+C to cancel)\n")
	fmt.Printf("\n")

	// Wait for answer via long-polling
	answer, err := client.WaitForAnswer(s.opts.Timeout)
	if err != nil {
		return "", fmt.Errorf("failed to receive answer: %w", err)
	}

	return answer, nil
}
