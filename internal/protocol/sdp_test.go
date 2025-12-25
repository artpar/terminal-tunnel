package protocol

import (
	"strings"
	"testing"
)

// Sample SDP for testing (simplified)
const testSDP = `v=0
o=- 1234567890 2 IN IP4 127.0.0.1
s=-
t=0 0
a=group:BUNDLE 0
a=ice-options:trickle
m=application 9 UDP/DTLS/SCTP webrtc-datachannel
c=IN IP4 0.0.0.0
a=ice-ufrag:abcd1234
a=ice-pwd:abcdefghijklmnopqrstuvwx
a=fingerprint:sha-256 AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99
a=setup:actpass
a=mid:0
a=sctp-port:5000
`

func TestCompressDecompressSDP(t *testing.T) {
	compressed, err := CompressSDP(testSDP)
	if err != nil {
		t.Fatalf("CompressSDP failed: %v", err)
	}

	// Should be smaller than original
	if len(compressed) >= len(testSDP) {
		t.Errorf("compressed size %d should be smaller than original %d", len(compressed), len(testSDP))
	}

	decompressed, err := DecompressSDP(compressed)
	if err != nil {
		t.Fatalf("DecompressSDP failed: %v", err)
	}

	if decompressed != testSDP {
		t.Errorf("decompressed SDP doesn't match original")
	}
}

func TestEncodeDecodeSessionData(t *testing.T) {
	salt := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	data := &SessionData{
		SDP:      testSDP,
		Salt:     salt,
		HostPort: 54321,
	}

	encoded, err := EncodeSessionData(data)
	if err != nil {
		t.Fatalf("EncodeSessionData failed: %v", err)
	}

	// Should be URL-safe
	if strings.ContainsAny(encoded, "+/=") {
		t.Error("encoded data should be URL-safe (no +, /, or =)")
	}

	decoded, err := DecodeSessionData(encoded)
	if err != nil {
		t.Fatalf("DecodeSessionData failed: %v", err)
	}

	if decoded.SDP != testSDP {
		t.Error("decoded SDP doesn't match")
	}

	if decoded.HostPort != 54321 {
		t.Errorf("port = %d, want 54321", decoded.HostPort)
	}

	for i := 0; i < 16; i++ {
		if decoded.Salt[i] != salt[i] {
			t.Errorf("salt[%d] = %d, want %d", i, decoded.Salt[i], salt[i])
		}
	}
}

func TestDecodeSessionDataInvalid(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
	}{
		{"invalid base64", "!!!invalid!!!"},
		{"too short", "AQID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeSessionData(tt.encoded)
			if err == nil {
				t.Error("expected error for invalid data")
			}
		})
	}
}

func TestCompressionRatio(t *testing.T) {
	ratio, err := GetCompressionRatio(testSDP)
	if err != nil {
		t.Fatalf("GetCompressionRatio failed: %v", err)
	}

	// Compression should reduce size
	if ratio >= 1.0 {
		t.Errorf("compression ratio %f should be less than 1.0", ratio)
	}

	t.Logf("Compression ratio: %.2f%% (original: %d bytes)", ratio*100, len(testSDP))
}

func TestEmptySDP(t *testing.T) {
	compressed, err := CompressSDP("")
	if err != nil {
		t.Fatalf("CompressSDP empty failed: %v", err)
	}

	decompressed, err := DecompressSDP(compressed)
	if err != nil {
		t.Fatalf("DecompressSDP empty failed: %v", err)
	}

	if decompressed != "" {
		t.Errorf("expected empty string, got %q", decompressed)
	}
}

func TestLargeSDP(t *testing.T) {
	// Simulate a larger SDP with many candidates
	largeSDP := testSDP
	for i := 0; i < 10; i++ {
		largeSDP += "a=candidate:1 1 UDP 2130706431 192.168.1.100 50000 typ host\n"
	}

	compressed, err := CompressSDP(largeSDP)
	if err != nil {
		t.Fatalf("CompressSDP large failed: %v", err)
	}

	ratio := float64(len(compressed)) / float64(len(largeSDP))
	t.Logf("Large SDP compression: %d -> %d bytes (%.1f%%)", len(largeSDP), len(compressed), ratio*100)

	decompressed, err := DecompressSDP(compressed)
	if err != nil {
		t.Fatalf("DecompressSDP large failed: %v", err)
	}

	if decompressed != largeSDP {
		t.Error("large SDP roundtrip failed")
	}
}

func TestPortEncoding(t *testing.T) {
	// Test various port values
	ports := []uint16{0, 1, 80, 443, 8080, 54321, 65535}

	for _, port := range ports {
		data := &SessionData{
			SDP:      "v=0\n",
			Salt:     make([]byte, 16),
			HostPort: port,
		}

		encoded, err := EncodeSessionData(data)
		if err != nil {
			t.Fatalf("EncodeSessionData port %d failed: %v", port, err)
		}

		decoded, err := DecodeSessionData(encoded)
		if err != nil {
			t.Fatalf("DecodeSessionData port %d failed: %v", port, err)
		}

		if decoded.HostPort != port {
			t.Errorf("port = %d, want %d", decoded.HostPort, port)
		}
	}
}
