// Package signaling provides signaling mechanisms for WebRTC connection establishment
package signaling

import "os"

// SignalingMethod represents the method used for SDP exchange
type SignalingMethod int

const (
	// MethodHTTP uses direct HTTP server (requires UPnP or port forwarding)
	MethodHTTP SignalingMethod = iota
	// MethodRelay uses a WebSocket relay server for SDP exchange
	MethodRelay
	// MethodManual uses QR code / copy-paste for SDP exchange
	MethodManual
	// MethodShortCode uses HTTP API with short codes (default for public relay)
	MethodShortCode
)

func (m SignalingMethod) String() string {
	switch m {
	case MethodHTTP:
		return "HTTP (direct)"
	case MethodRelay:
		return "WebSocket Relay"
	case MethodManual:
		return "Manual (QR/paste)"
	case MethodShortCode:
		return "Short Code"
	default:
		return "Unknown"
	}
}

// Environment variable names for customization
const (
	EnvRelayURL  = "TT_RELAY_URL"
	EnvClientURL = "TT_CLIENT_URL"
)

// Default URLs (used when environment variables are not set)
const (
	defaultRelayURL  = "https://terminal-tunnel-relay.artpar.workers.dev"
	defaultClientURL = "https://artpar.github.io/terminal-tunnel"
)

// GetRelayURL returns the relay URL from environment or default
func GetRelayURL() string {
	if url := os.Getenv(EnvRelayURL); url != "" {
		return url
	}
	return defaultRelayURL
}

// GetClientURL returns the web client URL from environment or default
func GetClientURL() string {
	if url := os.Getenv(EnvClientURL); url != "" {
		return url
	}
	return defaultClientURL
}

// RelayMessage represents a message in the relay protocol
type RelayMessage struct {
	Type      string `json:"type"`                 // register, offer, answer, error
	SessionID string `json:"session_id,omitempty"` // Session identifier
	Role      string `json:"role,omitempty"`       // host, client
	SDP       string `json:"sdp,omitempty"`        // SDP offer or answer
	Salt      string `json:"salt,omitempty"`       // Base64 encoded salt for key derivation
	Error     string `json:"error,omitempty"`      // Error message
}

// Message types
const (
	MsgTypeRegister = "register"
	MsgTypeOffer    = "offer"
	MsgTypeAnswer   = "answer"
	MsgTypeError    = "error"
)

// Roles
const (
	RoleHost   = "host"
	RoleClient = "client"
)

// CompactData represents compressed SDP data for QR/manual exchange
type CompactData struct {
	Version byte   // Protocol version
	Salt    []byte // 16-byte salt
	SDP     []byte // Compressed SDP
}

const (
	// CompactVersion is the current compact data format version
	CompactVersion byte = 0x01
	// SaltSize is the size of the salt in bytes
	SaltSize = 16
)

// ViewerSessionInfo contains info about a viewer session
type ViewerSessionInfo struct {
	ViewerCode string `json:"viewer_code"` // Code with V suffix (e.g., WXYZ5678V)
	ViewerURL  string `json:"viewer_url"`  // Full URL for viewers
}

// ViewerSessionResponse is the response when fetching a viewer session
type ViewerSessionResponse struct {
	SDP      string `json:"sdp"`
	Key      string `json:"key"`       // Base64-encoded encryption key (no password needed)
	ReadOnly bool   `json:"read_only"` // Always true for viewer sessions
	Used     bool   `json:"used"`      // True if a viewer already connected
}
