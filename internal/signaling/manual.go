package signaling

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

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

// StripSDP removes unnecessary lines from SDP to reduce size
func StripSDP(sdp string) string {
	var result []string
	lines := strings.Split(sdp, "\n")

	seenCandidateTypes := make(map[string]bool)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip unnecessary lines
		if strings.HasPrefix(line, "a=extmap") ||
			strings.HasPrefix(line, "a=rtcp-fb") ||
			strings.HasPrefix(line, "a=ssrc") ||
			strings.HasPrefix(line, "a=msid") ||
			strings.HasPrefix(line, "a=sctpmap") ||
			strings.HasPrefix(line, "a=rtcp:") ||
			strings.HasPrefix(line, "a=rtpmap") ||
			strings.HasPrefix(line, "a=fmtp") ||
			strings.HasPrefix(line, "a=max-message-size") ||
			strings.HasPrefix(line, "a=sctp-port") ||
			strings.HasPrefix(line, "a=end-of-candidates") {
			continue
		}

		// Keep only one candidate per type (host, srflx, relay)
		// Prefer srflx (public IP) over host (private IP)
		if strings.HasPrefix(line, "a=candidate") {
			candidateType := "host"
			if strings.Contains(line, " srflx ") {
				candidateType = "srflx"
			} else if strings.Contains(line, " relay ") {
				candidateType = "relay"
			}

			// Keep only UDP candidates, skip TCP
			if strings.Contains(line, " TCP ") || strings.Contains(line, " tcp ") {
				continue
			}

			// Skip host candidates if we have srflx (they're redundant for NAT traversal)
			if candidateType == "host" && seenCandidateTypes["srflx"] {
				continue
			}

			if seenCandidateTypes[candidateType] {
				continue
			}
			seenCandidateTypes[candidateType] = true
		}

		result = append(result, line)
	}

	return strings.Join(result, "\r\n") + "\r\n"
}

// CompactOffer creates a compressed, base64-encoded offer for QR/text
// Format: base64(version[1] + salt[16] + deflate(SDP))
func (m *ManualSignaling) CompactOffer() (string, error) {
	// Strip SDP to reduce size
	strippedSDP := StripSDP(m.offer)

	// Compress SDP with deflate (compatible with browser's pako)
	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return "", fmt.Errorf("failed to create compressor: %w", err)
	}
	writer.Write([]byte(strippedSDP))
	writer.Close()
	compressed := buf.Bytes()

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

	// Decompress SDP with deflate
	compressed := data[1+SaltSize:]
	reader := flate.NewReader(bytes.NewReader(compressed))
	defer reader.Close()

	sdpBytes, err := io.ReadAll(reader)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decompress: %w", err)
	}

	return string(sdpBytes), salt, nil
}

// CompactAnswer creates a compressed, base64-encoded answer
// Format: base64(version[1] + deflate(SDP)) - no salt needed for answer
func CompactAnswer(answer string) (string, error) {
	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return "", fmt.Errorf("failed to create compressor: %w", err)
	}
	writer.Write([]byte(answer))
	writer.Close()
	compressed := buf.Bytes()

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
	reader := flate.NewReader(bytes.NewReader(compressed))
	defer reader.Close()

	sdpBytes, err := io.ReadAll(reader)
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

	// Use Low error correction for smallest QR size
	qr, err := qrcode.New(compact, qrcode.Low)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %w", err)
	}

	return qr.ToSmallString(false), nil
}

// GetCompactOfferSize returns the size of the compact offer in characters
func (m *ManualSignaling) GetCompactOfferSize() int {
	compact, err := m.CompactOffer()
	if err != nil {
		return 0
	}
	return len(compact)
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
	compact, _ := m.CompactOffer()
	codeSize := len(compact)

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("  Terminal Tunnel - Manual Mode")
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println()
	fmt.Printf("  Session ID: %s\n", sessionID)
	fmt.Printf("  Password:   %s\n", password)
	fmt.Println()

	// Show QR code only if reasonably small (< 400 chars = ~QR version 12)
	if codeSize < 400 {
		fmt.Println("  Scan QR code or copy the code below:")
		fmt.Println()
		qr, err := m.GenerateQR()
		if err == nil {
			fmt.Println(qr)
		}
	} else {
		fmt.Printf("  Code too large for QR (%d chars). Copy the code below:\n", codeSize)
	}

	fmt.Println()
	fmt.Println("  ─── Connection Code ───")
	fmt.Println()

	// Print code in chunks for readability
	for i := 0; i < len(compact); i += 70 {
		end := i + 70
		if end > len(compact) {
			end = len(compact)
		}
		fmt.Printf("  %s\n", compact[i:end])
	}

	fmt.Println()
	fmt.Println("  ─────────────────────────")
	fmt.Println()
	fmt.Println("  Open terminal-tunnel client and paste this code.")
	fmt.Println("  Then enter the answer code below:")
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
