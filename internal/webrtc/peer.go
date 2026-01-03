// Package webrtc provides WebRTC peer connection management
package webrtc

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// TURN environment variables
const (
	EnvTURNURL      = "TURN_URL"
	EnvTURNUsername = "TURN_USERNAME"
	EnvTURNPassword = "TURN_PASSWORD"
)

// Default STUN servers for ICE candidate gathering
var defaultSTUNServers = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun1.l.google.com:19302",
	"stun:stun2.l.google.com:19302",
}

// Note: No default TURN servers - public TURN services require signup
// Users can configure TURN via environment variables:
//   TURN_URL=turn:your-server.com:3478
//   TURN_USERNAME=your-username
//   TURN_PASSWORD=your-password
// Or use --no-turn flag for STUN-only (works for most NAT types)

// TURNConfig holds TURN server credentials
type TURNConfig struct {
	URLs       []string
	Username   string
	Credential string
}

// Config holds peer connection configuration
type Config struct {
	ICEServers  []webrtc.ICEServer
	TURNServers []TURNConfig // Additional TURN servers
	UseTURN     bool         // Enable TURN for symmetric NAT
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		ICEServers:  nil, // Built from STUN/TURN in NewPeer
		TURNServers: nil, // Use defaults
		UseTURN:     true, // Enable TURN by default for NAT traversal
	}
}

// ConfigWithTURN returns configuration with custom TURN servers
func ConfigWithTURN(turnServers []TURNConfig) Config {
	return Config{
		TURNServers: turnServers,
		UseTURN:     true,
	}
}

// ConfigWithoutTURN returns configuration without TURN (P2P only)
func ConfigWithoutTURN() Config {
	return Config{
		UseTURN: false,
	}
}

// GetTURNFromEnv returns TURN config from environment variables if set
func GetTURNFromEnv() *TURNConfig {
	turnURL := os.Getenv(EnvTURNURL)
	if turnURL == "" {
		return nil
	}

	username := os.Getenv(EnvTURNUsername)
	password := os.Getenv(EnvTURNPassword)

	return &TURNConfig{
		URLs:       []string{turnURL},
		Username:   username,
		Credential: password,
	}
}

// buildICEServers creates WebRTC ICE servers from config
func buildICEServers(config Config) []webrtc.ICEServer {
	servers := []webrtc.ICEServer{}

	// Add STUN servers
	servers = append(servers, webrtc.ICEServer{
		URLs: defaultSTUNServers,
	})

	// Add TURN servers for NAT traversal (if configured)
	if config.UseTURN {
		var turnConfigs []TURNConfig

		// Check environment variables first
		if envTURN := GetTURNFromEnv(); envTURN != nil {
			turnConfigs = []TURNConfig{*envTURN}
		} else if len(config.TURNServers) > 0 {
			// Use custom TURN servers from config
			turnConfigs = config.TURNServers
		}
		// Note: No default TURN - requires explicit configuration

		for _, turn := range turnConfigs {
			servers = append(servers, webrtc.ICEServer{
				URLs:           turn.URLs,
				Username:       turn.Username,
				Credential:     turn.Credential,
				CredentialType: webrtc.ICECredentialTypePassword,
			})
		}
	}

	return servers
}

// Peer wraps a WebRTC peer connection with helpers for terminal tunneling
type Peer struct {
	pc          *webrtc.PeerConnection
	dataChannel *webrtc.DataChannel
	config      Config

	// Callbacks
	onDataChannel func(*webrtc.DataChannel)

	mu sync.Mutex
}

// NewPeer creates a new WebRTC peer connection
func NewPeer(config Config) (*Peer, error) {
	// Build ICE servers from config (STUN + optional TURN)
	iceServers := config.ICEServers
	if len(iceServers) == 0 {
		iceServers = buildICEServers(config)
	}

	peerConfig := webrtc.Configuration{
		ICEServers: iceServers,
	}

	pc, err := webrtc.NewPeerConnection(peerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	peer := &Peer{
		pc:     pc,
		config: config,
	}

	// Set up connection state logging
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		// Connection state changes can be logged/handled here
	})

	return peer, nil
}

// CreateDataChannel creates a data channel for terminal I/O (host side)
func (p *Peer) CreateDataChannel(label string) (*webrtc.DataChannel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ordered := true
	dc, err := p.pc.CreateDataChannel(label, &webrtc.DataChannelInit{
		Ordered: &ordered,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create data channel: %w", err)
	}

	p.dataChannel = dc
	return dc, nil
}

// OnDataChannel sets the callback for when a data channel is received (client side)
func (p *Peer) OnDataChannel(handler func(*webrtc.DataChannel)) {
	p.onDataChannel = handler
	p.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		p.mu.Lock()
		p.dataChannel = dc
		p.mu.Unlock()
		if handler != nil {
			handler(dc)
		}
	})
}

// CreateOffer creates an SDP offer and waits for ICE gathering to complete
func (p *Peer) CreateOffer() (string, error) {
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create offer: %w", err)
	}

	if err := p.pc.SetLocalDescription(offer); err != nil {
		return "", fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	if err := p.waitForICEGathering(); err != nil {
		return "", err
	}

	return p.pc.LocalDescription().SDP, nil
}

// CreateAnswer creates an SDP answer after receiving an offer
func (p *Peer) CreateAnswer() (string, error) {
	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create answer: %w", err)
	}

	if err := p.pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	if err := p.waitForICEGathering(); err != nil {
		return "", err
	}

	return p.pc.LocalDescription().SDP, nil
}

// SetRemoteDescription sets the remote SDP (offer or answer)
func (p *Peer) SetRemoteDescription(sdpType webrtc.SDPType, sdp string) error {
	desc := webrtc.SessionDescription{
		Type: sdpType,
		SDP:  sdp,
	}
	if err := p.pc.SetRemoteDescription(desc); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}
	return nil
}

// waitForICEGathering waits for ICE candidate gathering to complete
func (p *Peer) waitForICEGathering() error {
	gatherComplete := webrtc.GatheringCompletePromise(p.pc)

	select {
	case <-gatherComplete:
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("ICE gathering timed out")
	}
}

// GetPublicIP attempts to get the public IP from gathered ICE candidates
// by parsing server-reflexive (srflx) candidates from the SDP
func (p *Peer) GetPublicIP() string {
	if p.pc.LocalDescription() == nil {
		return ""
	}

	sdp := p.pc.LocalDescription().SDP

	// Parse SDP lines looking for srflx candidates
	// Example: a=candidate:... typ srflx ... raddr 192.168.1.100 rport 12345
	lines := strings.Split(sdp, "\n")
	for _, line := range lines {
		if !strings.Contains(line, "candidate") {
			continue
		}
		if !strings.Contains(line, "srflx") {
			continue
		}

		// Parse the candidate line to extract IP
		// Format: a=candidate:<foundation> <component> <protocol> <priority> <ip> <port> typ srflx ...
		parts := strings.Fields(line)
		for i, part := range parts {
			if part == "srflx" && i >= 2 {
				// The IP should be a few fields before "typ srflx"
				// Standard format puts IP at position 4 (0-indexed)
				for j := 4; j < len(parts) && j < i; j++ {
					if ip := net.ParseIP(parts[j]); ip != nil {
						if isPublicIP(ip) {
							return ip.String()
						}
					}
				}
			}
		}
	}

	return ""
}

// isPublicIP checks if an IP address is publicly routable
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// Check if it's a private/local address
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}

	// IPv4 specific checks
	if ip4 := ip.To4(); ip4 != nil {
		// Check for CGNAT range (100.64.0.0/10)
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return false
		}
	}

	return true
}

// OnConnectionStateChange sets a callback for connection state changes
func (p *Peer) OnConnectionStateChange(handler func(webrtc.PeerConnectionState)) {
	p.pc.OnConnectionStateChange(handler)
}

// OnICEConnectionStateChange sets a callback for ICE connection state changes
func (p *Peer) OnICEConnectionStateChange(handler func(webrtc.ICEConnectionState)) {
	p.pc.OnICEConnectionStateChange(handler)
}

// Close closes the peer connection
func (p *Peer) Close() error {
	return p.pc.Close()
}

// ConnectionState returns the current connection state
func (p *Peer) ConnectionState() webrtc.PeerConnectionState {
	return p.pc.ConnectionState()
}

// DataChannel returns the current data channel
func (p *Peer) DataChannel() *webrtc.DataChannel {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dataChannel
}
