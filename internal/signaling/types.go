// Package signaling provides signaling mechanisms for WebRTC connection establishment
package signaling

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

// DefaultRelayURL is the public relay server (Cloudflare Worker)
const DefaultRelayURL = "https://terminal-tunnel-relay.artpar.workers.dev"

// DefaultClientURL is the public web client
const DefaultClientURL = "https://artpar.github.io/terminal-tunnel"

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
