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
	ttwebrtc "github.com/artpar/terminal-tunnel/internal/webrtc"
	"github.com/artpar/terminal-tunnel/internal/web"
)

// Options configures the terminal tunnel server
type Options struct {
	Password string
	Shell    string
	Timeout  time.Duration
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
	opts       Options
	peer       *ttwebrtc.Peer
	signaling  *SignalingServer
	pty        *PTY
	bridge     *Bridge
	channel    *ttwebrtc.EncryptedChannel
	salt       []byte
	key        [32]byte
	upnpClose  func() error
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

	return &Server{
		opts: opts,
		salt: salt,
		key:  key,
	}, nil
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

	// Start signaling server
	saltB64 := base64.StdEncoding.EncodeToString(s.salt)
	signaling, err := NewSignalingServer(offer, s.generateSessionID(), saltB64, web.StaticFS)
	if err != nil {
		return fmt.Errorf("failed to create signaling server: %w", err)
	}
	s.signaling = signaling

	if err := signaling.Start(); err != nil {
		return fmt.Errorf("failed to start signaling: %w", err)
	}

	port := uint16(signaling.Port())

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
	fmt.Printf("  Salt: %s\n", base64.StdEncoding.EncodeToString(s.salt))
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
	answer, err := signaling.WaitForAnswer(s.opts.Timeout)
	if err != nil {
		return fmt.Errorf("failed to receive answer: %w", err)
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
	signaling.Close()

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
	if s.peer != nil {
		s.peer.Close()
	}
	if s.upnpClose != nil {
		s.upnpClose()
	}
	return nil
}

// generateSessionID creates a unique session identifier
func (s *Server) generateSessionID() string {
	b := make([]byte, 8)
	crypto.GenerateSalt() // Just for randomness
	return base64.RawURLEncoding.EncodeToString(b)[:8]
}
