package signaling

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/skip2/go-qrcode"
)

// ManualSignaling handles QR code and copy-paste based SDP exchange
type ManualSignaling struct {
	offer string
	salt  []byte
}

// NewManualSignaling creates a new manual signaling handler
func NewManualSignaling(offer string, salt []byte) *ManualSignaling {
	return &ManualSignaling{
		offer: offer,
		salt:  salt,
	}
}

// CompactOffer creates a compressed, base64-encoded offer for QR/text
// Format: base64(version[1] + salt[16] + zstd(SDP))
func (m *ManualSignaling) CompactOffer() (string, error) {
	// Compress SDP with zstd
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return "", fmt.Errorf("failed to create compressor: %w", err)
	}
	compressed := encoder.EncodeAll([]byte(m.offer), nil)

	// Build compact format: version + salt + compressed_sdp
	data := make([]byte, 1+SaltSize+len(compressed))
	data[0] = CompactVersion
	copy(data[1:1+SaltSize], m.salt)
	copy(data[1+SaltSize:], compressed)

	// Encode as URL-safe base64
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// DecodeCompactOffer decodes a compact offer string
func DecodeCompactOffer(encoded string) (sdp string, salt []byte, err error) {
	// Decode base64
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		// Try standard base64 as fallback
		data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return "", nil, fmt.Errorf("invalid base64: %w", err)
		}
	}

	// Verify minimum length
	if len(data) < 1+SaltSize+1 {
		return "", nil, fmt.Errorf("data too short")
	}

	// Check version
	version := data[0]
	if version != CompactVersion {
		return "", nil, fmt.Errorf("unsupported version: %d", version)
	}

	// Extract salt
	salt = make([]byte, SaltSize)
	copy(salt, data[1:1+SaltSize])

	// Decompress SDP
	compressed := data[1+SaltSize:]
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create decompressor: %w", err)
	}
	defer decoder.Close()

	sdpBytes, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decompress: %w", err)
	}

	return string(sdpBytes), salt, nil
}

// CompactAnswer creates a compressed, base64-encoded answer
// Format: base64(version[1] + zstd(SDP)) - no salt needed for answer
func CompactAnswer(answer string) (string, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return "", fmt.Errorf("failed to create compressor: %w", err)
	}
	compressed := encoder.EncodeAll([]byte(answer), nil)

	// Build compact format: version + compressed_sdp
	data := make([]byte, 1+len(compressed))
	data[0] = CompactVersion
	copy(data[1:], compressed)

	return base64.RawURLEncoding.EncodeToString(data), nil
}

// DecodeCompactAnswer decodes a compact answer string
func DecodeCompactAnswer(encoded string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return "", fmt.Errorf("invalid base64: %w", err)
		}
	}

	if len(data) < 2 {
		return "", fmt.Errorf("data too short")
	}

	version := data[0]
	if version != CompactVersion {
		return "", fmt.Errorf("unsupported version: %d", version)
	}

	compressed := data[1:]
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create decompressor: %w", err)
	}
	defer decoder.Close()

	sdpBytes, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decompress: %w", err)
	}

	return string(sdpBytes), nil
}

// GenerateQR creates an ASCII QR code from the compact offer
func (m *ManualSignaling) GenerateQR() (string, error) {
	compact, err := m.CompactOffer()
	if err != nil {
		return "", err
	}

	// Generate QR code as string
	qr, err := qrcode.New(compact, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %w", err)
	}

	return qr.ToSmallString(false), nil
}

// GenerateQRPNG creates a PNG QR code and writes to the given writer
func (m *ManualSignaling) GenerateQRPNG(w io.Writer, size int) error {
	compact, err := m.CompactOffer()
	if err != nil {
		return err
	}

	png, err := qrcode.Encode(compact, qrcode.Medium, size)
	if err != nil {
		return fmt.Errorf("failed to generate QR PNG: %w", err)
	}

	_, err = w.Write(png)
	return err
}

// PrintInstructions displays user-friendly connection instructions
func (m *ManualSignaling) PrintInstructions(sessionID, password string) {
	fmt.Println()
	fmt.Println("=== Manual Connection Mode ===")
	fmt.Println()
	fmt.Println("No direct connectivity available. Use one of these methods:")
	fmt.Println()
	fmt.Println("Option 1: Scan QR Code")
	fmt.Println("   Scan the QR code below with your device's camera")
	fmt.Println()

	qr, err := m.GenerateQR()
	if err == nil {
		fmt.Println(qr)
	}

	fmt.Println()
	fmt.Println("Option 2: Copy Connection Code")
	compact, _ := m.CompactOffer()
	fmt.Println("   Share this connection code:")
	fmt.Println()
	fmt.Printf("   %s\n", compact)
	fmt.Println()
	fmt.Printf("   Session ID: %s\n", sessionID)
	fmt.Printf("   Password: %s (share separately!)\n", password)
	fmt.Println()
	fmt.Println("Waiting for answer...")
	fmt.Println()
}

// ReadAnswer reads an answer from stdin
func ReadAnswer() (string, error) {
	fmt.Print("Enter the answer code from the client: ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read answer: %w", err)
	}

	// Decode the compact answer
	return DecodeCompactAnswer(strings.TrimSpace(line))
}
