package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/skip2/go-qrcode"

	"github.com/artpar/terminal-tunnel/internal/crypto"
	"github.com/artpar/terminal-tunnel/internal/recording"
	"github.com/artpar/terminal-tunnel/internal/signaling"
	ttwebrtc "github.com/artpar/terminal-tunnel/internal/webrtc"
	"github.com/artpar/terminal-tunnel/internal/web"
)

// Options configures the terminal tunnel server
type Options struct {
	Password   string
	Shell      string
	Timeout    time.Duration
	RelayURL   string // WebSocket relay URL for signaling
	NoRelay    bool   // Disable relay, use manual if UPnP fails
	Manual     bool   // Force manual (QR/copy-paste) signaling mode
	NoTURN     bool   // Disable TURN servers (P2P only, may fail with symmetric NAT)
	Public     bool   // Enable public viewer mode (read-only viewers without password)
	Record     bool   // Enable session recording
	RecordFile string // Custom recording file path (optional)
}

// Callbacks for daemon integration
type Callbacks struct {
	OnShortCodeReady   func(code, clientURL string)
	OnViewerCodeReady  func(viewerCode, viewerURL string) // For public viewer mode
	OnClientConnect    func()
	OnClientDisconnect func()
	OnViewerConnect    func() // For public viewer connections
	OnViewerDisconnect func()
	OnPTYReady         func(ptyPath string, shellPID int)
	OnBridgeReady      func(bridge *Bridge) // Called when bridge is ready for local I/O
}

// DefaultOptions returns sensible defaults
func DefaultOptions() Options {
	return Options{
		Shell:   "",
		Timeout: 5 * time.Minute,
	}
}

// Relay heartbeat interval (keeps session alive on relay)
// Set to 4 minutes (session TTL is 5 min) to minimize KV operations
const relayHeartbeatInterval = 4 * time.Minute

// Server orchestrates the terminal tunnel
type Server struct {
	opts            Options
	peer            *ttwebrtc.Peer
	signaling       *SignalingServer
	relayClient     *signaling.RelayClient
	shortCodeClient *signaling.ShortCodeClient
	pty             *PTY
	bridge          *Bridge
	channel         *ttwebrtc.EncryptedChannel
	salt            []byte
	key             [32]byte
	pbkdf2Key       [32]byte // PBKDF2 fallback key for CSP-restricted browsers
	sessionID       string
	upnpClose       func() error
	disconnected    chan bool
	ctx             context.Context
	cancel          context.CancelFunc
	callbacks       Callbacks
	webrtcConfig    ttwebrtc.Config

	// Public viewer support (dual-peer architecture)
	viewerPeer    *ttwebrtc.Peer
	viewerChannel *ttwebrtc.EncryptedChannel
	viewerKey     [32]byte // Random key for viewer encryption (stored in relay)
	viewerCode    string   // Viewer session code (ends with V)

	// Recording support
	recorder *recording.Recorder

	// Relay heartbeat
	heartbeatStop chan struct{}
}

// NewServer creates a new terminal tunnel server
func NewServer(opts Options) (*Server, error) {
	// Generate salt for key derivation
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive encryption keys (Argon2 primary, PBKDF2 fallback for CSP-restricted browsers)
	key := crypto.DeriveKey(opts.Password, salt)
	pbkdf2Key := crypto.DeriveKeyPBKDF2(opts.Password, salt)

	// Generate session ID
	sessionID := generateSessionID()

	// Configure WebRTC with TURN support
	var webrtcConfig ttwebrtc.Config
	if opts.NoTURN {
		webrtcConfig = ttwebrtc.ConfigWithoutTURN()
	} else {
		webrtcConfig = ttwebrtc.DefaultConfig()
	}

	server := &Server{
		opts:         opts,
		salt:         salt,
		key:          key,
		pbkdf2Key:    pbkdf2Key,
		sessionID:    sessionID,
		webrtcConfig: webrtcConfig,
	}

	// Generate random viewer key if public mode is enabled
	if opts.Public {
		viewerKeyBytes, err := crypto.GenerateRandomKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate viewer key: %w", err)
		}
		copy(server.viewerKey[:], viewerKeyBytes)
	}

	return server, nil
}

// New is an alias for NewServer (for daemon use)
func New(opts Options) (*Server, error) {
	return NewServer(opts)
}

// SetCallbacks sets the callbacks for daemon integration
func (s *Server) SetCallbacks(cb Callbacks) {
	s.callbacks = cb
}

// SetPTY sets an existing PTY for session recovery (reattachment after daemon restart)
func (s *Server) SetPTY(pty *PTY) {
	s.pty = pty
}

// GetPTY returns the PTY (may be nil if not started yet)
func (s *Server) GetPTY() *PTY {
	return s.pty
}

// GetBridge returns the Bridge (may be nil if not connected)
func (s *Server) GetBridge() *Bridge {
	return s.bridge
}

// generateSessionID creates a unique session identifier
func generateSessionID() string {
	salt, _ := crypto.GenerateSalt()
	return base64.RawURLEncoding.EncodeToString(salt)[:8]
}

// Start initializes and runs the terminal tunnel (standalone mode)
func (s *Server) Start(ctx ...context.Context) error {
	s.disconnected = make(chan bool, 1)
	saltB64 := base64.StdEncoding.EncodeToString(s.salt)

	// Use provided context or create our own
	if len(ctx) > 0 && ctx[0] != nil {
		s.ctx, s.cancel = context.WithCancel(ctx[0])
	} else {
		// Create cancellable context for graceful shutdown
		s.ctx, s.cancel = context.WithCancel(context.Background())

		// Handle signals only in standalone mode
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		// Cancel context on signal (allows blocking operations to exit)
		go func() {
			<-sigChan
			fmt.Printf("\n\nShutting down...\n")
			s.cancel()
		}()
	}

	// Determine signaling method once
	sigMethod := s.determineSignalingMethod()
	fmt.Printf("Using signaling method: %s\n", sigMethod)

	// Display TURN configuration
	if s.webrtcConfig.UseTURN {
		fmt.Printf("✓ TURN relay enabled for symmetric NAT traversal\n")
	} else {
		fmt.Printf("⚠ TURN disabled (may fail with symmetric NAT)\n")
	}

	isFirstConnection := true

	// Connection loop - allows reconnection
	for {
		// Create WebRTC peer with configured ICE servers (STUN + optional TURN)
		peer, err := ttwebrtc.NewPeer(s.webrtcConfig)
		if err != nil {
			return fmt.Errorf("failed to create peer: %w", err)
		}
		s.peer = peer

		// Monitor connection state for debugging and early disconnect detection
		peer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			fmt.Printf("  [WebRTC] Connection state: %s\n", state.String())
			switch state {
			case webrtc.PeerConnectionStateConnected:
				// Connection established
			case webrtc.PeerConnectionStateDisconnected:
				// Note: "disconnected" can recover - don't trigger reconnect yet
				fmt.Printf("\n⚠ WebRTC connection disconnected (may recover)\n")
			case webrtc.PeerConnectionStateFailed:
				fmt.Printf("\n✗ WebRTC connection failed\n")
				select {
				case s.disconnected <- true:
				default:
				}
			case webrtc.PeerConnectionStateClosed:
				// Connection closed intentionally
			}
		})

		// Monitor ICE connection state for debugging
		peer.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
			fmt.Printf("  [ICE] Connection state: %s\n", state.String())
			switch state {
			case webrtc.ICEConnectionStateDisconnected:
				fmt.Printf("\n⚠ ICE disconnected (checking connectivity...)\n")
			case webrtc.ICEConnectionStateFailed:
				fmt.Printf("\n✗ ICE failed - NAT/firewall may be blocking UDP\n")
			case webrtc.ICEConnectionStateClosed:
				fmt.Printf("\n⚠ ICE connection closed\n")
			}
		})

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

		// Get public IP from STUN (for display purposes) - only on first connection
		if isFirstConnection {
			publicIP := peer.GetPublicIP()
			if publicIP != "" {
				fmt.Printf("✓ Public IP discovered via STUN: %s\n", publicIP)
			}
		}

		var answer string

		if isFirstConnection {
			// First connection - create new session
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
		} else {
			// Reconnection - update existing session
			if sigMethod == signaling.MethodShortCode && s.shortCodeClient != nil {
				fmt.Printf("\n  Waiting for reconnection... (same code: %s)\n\n", s.shortCodeClient.GetCode())
				err = s.shortCodeClient.UpdateSession(offer, saltB64)
				if err != nil {
					fmt.Printf("⚠ Failed to update session: %v\n", err)
					return err
				}
				// Use context for cancellation support
				var reconnCtx context.Context
				var reconnCancel context.CancelFunc
				if s.opts.Timeout > 0 {
					reconnCtx, reconnCancel = context.WithTimeout(s.ctx, s.opts.Timeout)
				} else {
					reconnCtx, reconnCancel = context.WithCancel(s.ctx)
				}
				answer, err = s.shortCodeClient.WaitForAnswerWithContext(reconnCtx)
				reconnCancel()
				if err != nil && s.ctx.Err() != nil {
					return s.Stop()
				}
			} else {
				// For other methods, fall back to creating new session
				switch sigMethod {
				case signaling.MethodHTTP:
					answer, err = s.startHTTPSignaling(offer, saltB64)
				case signaling.MethodRelay:
					answer, err = s.startRelaySignaling(offer, saltB64)
				case signaling.MethodManual:
					answer, err = s.startManualSignaling(offer)
				}
			}
		}

		if err != nil {
			if s.ctx.Err() != nil {
				return s.Stop()
			}
			return err
		}

		fmt.Printf("✓ Received client answer\n")

		// Set remote description
		if err := peer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer); err != nil {
			return fmt.Errorf("failed to set answer: %w", err)
		}

		// Wait for data channel to open
		dcOpen := make(chan bool, 1)
		dc.OnOpen(func() {
			dcOpen <- true
		})

		select {
		case <-dcOpen:
			fmt.Printf("✓ Data channel connected\n")
		case <-time.After(30 * time.Second):
			peer.Close()
			fmt.Printf("⚠ Connection timeout, waiting for new client...\n")
			continue
		case <-s.ctx.Done():
			return s.Stop()
		}

		// Close signaling server - no longer needed
		if s.signaling != nil {
			s.signaling.Close()
			s.signaling = nil
		}

		// Start PTY only on first connection
		if s.pty == nil {
			pty, err := StartPTY(s.opts.Shell)
			if err != nil {
				return fmt.Errorf("failed to start PTY: %w", err)
			}
			s.pty = pty

			// Invoke PTY ready callback
			if s.callbacks.OnPTYReady != nil {
				s.callbacks.OnPTYReady(pty.Name(), pty.PID())
			}

			// Start recording if enabled
			if s.opts.Record && s.recorder == nil {
				recordPath := s.opts.RecordFile
				if recordPath == "" {
					// Generate default recording path using short code
					code := s.sessionID
					if s.shortCodeClient != nil {
						code = s.shortCodeClient.GetCode()
					}
					recordPath = recording.GenerateRecordingPath(code)
				}
				rec, err := recording.NewRecorder(recordPath, 80, 24, "Terminal Tunnel Session")
				if err != nil {
					fmt.Printf("⚠ Failed to start recording: %v\n", err)
				} else {
					s.recorder = rec
					fmt.Printf("✓ Recording to: %s\n", recordPath)
				}
			}
		}

		fmt.Printf("✓ Terminal session active\n")

		// Invoke client connect callback
		if s.callbacks.OnClientConnect != nil {
			s.callbacks.OnClientConnect()
		}
		fmt.Printf("\n")

		// Create encrypted channel with PBKDF2 fallback for CSP-restricted browsers
		channel := ttwebrtc.NewEncryptedChannel(dc, &s.key)
		channel.SetAltKey(&s.pbkdf2Key)
		s.channel = channel

		// Create bridge
		bridge := NewBridge(s.pty, channel.SendData)
		s.bridge = bridge

		// Attach recorder to bridge if recording is enabled
		if s.recorder != nil {
			bridge.SetRecorder(s.recorder.WriteOutput)
		}

		// Invoke bridge ready callback for interactive mode
		if s.callbacks.OnBridgeReady != nil {
			s.callbacks.OnBridgeReady(bridge)
		}

		// Handle incoming data
		channel.OnData(func(data []byte) {
			bridge.HandleData(data)
		})

		channel.OnResize(func(rows, cols uint16) {
			bridge.HandleResize(rows, cols)
		})

		channel.OnClose(func() {
			fmt.Printf("\n✓ Client disconnected (data channel closed)\n")
			if s.peer != nil {
				fmt.Printf("  [Debug] Peer connection state: %s\n", s.peer.ConnectionState().String())
			}
			fmt.Printf("  [Debug] Channel useAltKey: %v\n", channel.UseAltKey())
			// Invoke disconnect callback
			if s.callbacks.OnClientDisconnect != nil {
				s.callbacks.OnClientDisconnect()
			}
			select {
			case s.disconnected <- true:
			default:
				fmt.Printf("  [Debug] disconnected channel was full/blocked\n")
			}
		})

		// Start bridge (PTY -> channel)
		bridge.Start()

		// Start keepalive monitoring (server sends pings, expects pongs)
		keepaliveTimeout := channel.StartKeepalive()

		// Start relay heartbeat on first connection (keeps session alive on relay)
		if isFirstConnection {
			s.startRelayHeartbeat()
		}

		isFirstConnection = false

		// Wait for disconnection, keepalive timeout, or termination
		select {
		case <-s.disconnected:
			// Client disconnected, clean up and wait for reconnection
			s.cleanupConnection()
			// Delay before accepting reconnection to avoid race condition
			// where client reconnects with stale offer (must be longer than client's reconnect delay)
			time.Sleep(3 * time.Second)
			continue
		case <-keepaliveTimeout:
			// Keepalive timed out - no pong received within timeout
			fmt.Printf("\n⚠ Connection timed out (no response from client)\n")
			s.cleanupConnection()
			time.Sleep(3 * time.Second)
			continue
		case <-s.ctx.Done():
			return s.Stop()
		}
	}
}

// cleanupConnection cleans up the current connection for reconnection
// PTY is kept running to allow client reconnection to the same session
func (s *Server) cleanupConnection() {
	fmt.Printf("  [Debug] cleanupConnection starting\n")
	if s.bridge != nil {
		s.bridge.ClearViewerSends() // Clear viewer sends before closing
		s.bridge.CloseWithoutPTY()  // Keep PTY running for reconnection
		s.bridge = nil
	}
	if s.channel != nil {
		s.channel.StopKeepalive() // Stop keepalive before closing
		s.channel.Close()
		s.channel = nil
	}
	if s.viewerChannel != nil {
		s.viewerChannel.Close()
		s.viewerChannel = nil
	}
	if s.peer != nil {
		s.peer.Close()
		s.peer = nil
	}
	if s.viewerPeer != nil {
		s.viewerPeer.Close()
		s.viewerPeer = nil
	}
	fmt.Printf("  [Debug] cleanupConnection complete\n")
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	s.stopRelayHeartbeat()
	if s.bridge != nil {
		s.bridge.Close()
	}
	if s.channel != nil {
		s.channel.Close()
	}
	if s.viewerChannel != nil {
		s.viewerChannel.Close()
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
	if s.viewerPeer != nil {
		_ = s.viewerPeer.Close()
	}
	if s.upnpClose != nil {
		s.upnpClose()
	}
	// Close recorder and print summary
	if s.recorder != nil {
		path := s.recorder.Path()
		duration := s.recorder.Duration()
		_ = s.recorder.Close()
		fmt.Printf("✓ Recording saved: %s (duration: %v)\n", path, duration.Round(time.Second))
	}
	return nil
}

// startRelayHeartbeat starts a goroutine to periodically send heartbeats to keep the relay session alive
func (s *Server) startRelayHeartbeat() {
	if s.shortCodeClient == nil {
		return
	}

	s.heartbeatStop = make(chan struct{})

	go func() {
		ticker := time.NewTicker(relayHeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-s.heartbeatStop:
				return
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				if err := s.shortCodeClient.SendHeartbeat(); err != nil {
					// Log but don't fail - session might still work
					fmt.Printf("⚠ Relay heartbeat failed: %v\n", err)
				}
			}
		}
	}()
}

// stopRelayHeartbeat stops the relay heartbeat goroutine
func (s *Server) stopRelayHeartbeat() {
	if s.heartbeatStop != nil {
		select {
		case <-s.heartbeatStop:
			// Already closed
		default:
			close(s.heartbeatStop)
		}
		s.heartbeatStop = nil
	}
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
		s.opts.RelayURL = signaling.GetRelayURL()
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

	sigPort := sig.Port()
	if sigPort < 0 || sigPort > 65535 {
		return "", fmt.Errorf("invalid port number: %d", sigPort)
	}
	port := uint16(sigPort)

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
			_ = s.signaling.Close()
			s.signaling = nil
			return s.startRelaySignaling(offer, saltB64)
		}
		// If no relay, fall back to manual
		if !upnpMapped {
			_ = s.signaling.Close()
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
	// Create short code client and save for reconnection
	client := signaling.NewShortCodeClient(s.opts.RelayURL, signaling.GetClientURL())
	s.shortCodeClient = client

	var code string
	var viewerCode string
	var err error

	// If public mode, create viewer peer and session
	if s.opts.Public {
		// Create viewer WebRTC peer
		viewerPeer, err := ttwebrtc.NewPeer(s.webrtcConfig)
		if err != nil {
			return "", fmt.Errorf("failed to create viewer peer: %w", err)
		}
		s.viewerPeer = viewerPeer

		// Create viewer data channel
		viewerDC, err := viewerPeer.CreateDataChannel("terminal")
		if err != nil {
			_ = viewerPeer.Close()
			return "", fmt.Errorf("failed to create viewer data channel: %w", err)
		}

		// Create viewer SDP offer
		viewerOffer, err := viewerPeer.CreateOffer()
		if err != nil {
			_ = viewerPeer.Close()
			return "", fmt.Errorf("failed to create viewer offer: %w", err)
		}

		// Encode viewer key
		viewerKeyB64 := base64.StdEncoding.EncodeToString(s.viewerKey[:])

		// Create session with viewer
		code, viewerCode, err = client.CreateSessionWithViewer(offer, saltB64, viewerOffer, viewerKeyB64)
		if err != nil {
			_ = viewerPeer.Close()
			fmt.Printf("⚠ Failed to create session with viewer: %v\n", err)
			fmt.Printf("Falling back to manual mode...\n")
			return s.startManualSignaling(offer)
		}
		s.viewerCode = viewerCode

		// Set up viewer data channel handler (output only, no input)
		viewerDC.OnOpen(func() {
			fmt.Printf("✓ Viewer connected\n")
			if s.callbacks.OnViewerConnect != nil {
				s.callbacks.OnViewerConnect()
			}

			// Create encrypted channel for viewer with viewer key
			viewerChannel := ttwebrtc.NewEncryptedChannel(viewerDC, &s.viewerKey)
			s.viewerChannel = viewerChannel

			// Add viewer to bridge output (if bridge exists)
			if s.bridge != nil {
				s.bridge.AddViewerSend(viewerChannel.SendData)
			}

			// Handle viewer disconnect (no input handling for viewers)
			viewerChannel.OnClose(func() {
				fmt.Printf("✓ Viewer disconnected\n")
				if s.callbacks.OnViewerDisconnect != nil {
					s.callbacks.OnViewerDisconnect()
				}
			})
		})

		// Start waiting for viewer answer in background
		go s.waitForViewerConnection()
	} else {
		// Normal session without viewer
		code, err = client.CreateSession(offer, saltB64)
		if err != nil {
			fmt.Printf("⚠ Failed to create session: %v\n", err)
			fmt.Printf("Falling back to manual mode...\n")
			return s.startManualSignaling(offer)
		}
	}

	clientURL := client.GetClientURL()

	// Display connection info (skip if CLI is handling display via callback)
	if s.callbacks.OnShortCodeReady == nil {
		fmt.Printf("\n")
		fmt.Printf("═══════════════════════════════════════════════════\n")
		fmt.Printf("  Terminal Tunnel Ready!\n")
		fmt.Printf("═══════════════════════════════════════════════════\n")
		fmt.Printf("\n")
		fmt.Printf("  Code: %s\n", code)
		fmt.Printf("  Password: %s\n", s.opts.Password)
		fmt.Printf("\n")
		fmt.Printf("  Or open: %s\n", clientURL)
	}

	// Display viewer info if public mode
	if s.opts.Public && viewerCode != "" {
		viewerURL := client.GetViewerURL()
		if s.callbacks.OnShortCodeReady == nil {
			fmt.Printf("\n")
			fmt.Printf("  Viewer Code: %s (read-only, no password)\n", viewerCode)
			fmt.Printf("  Viewer URL:  %s\n", viewerURL)
		}

		// Invoke callback for viewer code ready
		if s.callbacks.OnViewerCodeReady != nil {
			s.callbacks.OnViewerCodeReady(viewerCode, viewerURL)
		}
	}

	// Invoke callback for short code ready
	if s.callbacks.OnShortCodeReady != nil {
		s.callbacks.OnShortCodeReady(code, clientURL)
	}

	// Display QR code and waiting message (skip if CLI is handling display)
	if s.callbacks.OnShortCodeReady == nil {
		fmt.Printf("\n")
		qr, err := qrcode.New(clientURL, qrcode.Low)
		if err == nil {
			fmt.Print(qr.ToSmallString(false))
		}
		fmt.Printf("\n")
		fmt.Printf("  Waiting for connection... (Ctrl+C to cancel)\n")
		fmt.Printf("\n")
	}

	// Wait for answer via long-polling with context for cancellation
	var waitCtx context.Context
	var cancelWait context.CancelFunc
	if s.opts.Timeout > 0 {
		waitCtx, cancelWait = context.WithTimeout(s.ctx, s.opts.Timeout)
	} else {
		// No timeout - wait indefinitely (for daemon mode)
		waitCtx, cancelWait = context.WithCancel(s.ctx)
	}
	defer cancelWait()
	answer, err := client.WaitForAnswerWithContext(waitCtx)
	if err != nil {
		if s.ctx.Err() != nil {
			return "", s.ctx.Err()
		}
		return "", fmt.Errorf("failed to receive answer: %w", err)
	}

	return answer, nil
}

// waitForViewerConnection waits for a viewer to connect in the background
func (s *Server) waitForViewerConnection() {
	if s.shortCodeClient == nil || s.viewerPeer == nil {
		return
	}

	// Wait for viewer answer
	answer, err := s.shortCodeClient.WaitForViewerAnswerWithContext(s.ctx)
	if err != nil {
		if s.ctx.Err() == nil {
			fmt.Printf("⚠ Viewer connection failed: %v\n", err)
		}
		return
	}

	// Set remote description
	if err := s.viewerPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer); err != nil {
		fmt.Printf("⚠ Failed to set viewer answer: %v\n", err)
		return
	}

	fmt.Printf("✓ Viewer answer received\n")
}
