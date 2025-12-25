package protocol

import (
	"encoding/base64"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

var (
	encoder *zstd.Encoder
	decoder *zstd.Decoder
)

func init() {
	var err error
	encoder, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		panic(fmt.Sprintf("failed to create zstd encoder: %v", err))
	}
	decoder, err = zstd.NewReader(nil)
	if err != nil {
		panic(fmt.Sprintf("failed to create zstd decoder: %v", err))
	}
}

// SessionData contains all data needed for client connection
type SessionData struct {
	SDP      string // Compressed SDP offer
	Salt     []byte // 16-byte salt for key derivation
	HostPort uint16 // Port for answer submission
}

// CompressSDP compresses an SDP string using zstd
func CompressSDP(sdp string) ([]byte, error) {
	return encoder.EncodeAll([]byte(sdp), nil), nil
}

// DecompressSDP decompresses a zstd-compressed SDP
func DecompressSDP(data []byte) (string, error) {
	decompressed, err := decoder.DecodeAll(data, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decompress SDP: %w", err)
	}
	return string(decompressed), nil
}

// EncodeSessionData encodes session data to a URL-safe base64 string
// Format: [2 bytes port][16 bytes salt][compressed SDP]
func EncodeSessionData(data *SessionData) (string, error) {
	compressed, err := CompressSDP(data.SDP)
	if err != nil {
		return "", err
	}

	// Build binary payload: port (2 bytes) + salt (16 bytes) + compressed SDP
	payload := make([]byte, 2+16+len(compressed))
	payload[0] = byte(data.HostPort >> 8)
	payload[1] = byte(data.HostPort & 0xFF)
	copy(payload[2:18], data.Salt)
	copy(payload[18:], compressed)

	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeSessionData decodes a URL-safe base64 string to session data
func DecodeSessionData(encoded string) (*SessionData, error) {
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	if len(payload) < 18 {
		return nil, fmt.Errorf("payload too short: %d bytes", len(payload))
	}

	port := uint16(payload[0])<<8 | uint16(payload[1])
	salt := payload[2:18]
	compressed := payload[18:]

	sdp, err := DecompressSDP(compressed)
	if err != nil {
		return nil, err
	}

	return &SessionData{
		SDP:      sdp,
		Salt:     salt,
		HostPort: port,
	}, nil
}

// GetCompressionRatio returns the compression ratio for an SDP
func GetCompressionRatio(sdp string) (float64, error) {
	compressed, err := CompressSDP(sdp)
	if err != nil {
		return 0, err
	}
	return float64(len(compressed)) / float64(len(sdp)), nil
}
