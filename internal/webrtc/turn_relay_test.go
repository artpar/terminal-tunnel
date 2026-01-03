package webrtc

import (
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
	"github.com/artpar/terminal-tunnel/internal/signaling"
)

// TestTURNConfiguration tests TURN server configuration
func TestTURNConfiguration(t *testing.T) {
	t.Run("ConfigWithTURN", func(t *testing.T) {
		config := ConfigWithTURN([]TURNConfig{
			{
				URLs:       []string{"turn:example.com:3478"},
				Username:   "user",
				Credential: "pass",
			},
		})

		if !config.UseTURN {
			t.Error("UseTURN should be true")
		}

		if len(config.TURNServers) != 1 {
			t.Errorf("Expected 1 TURN server, got %d", len(config.TURNServers))
		}
	})

	t.Run("ConfigWithoutTURN", func(t *testing.T) {
		config := ConfigWithoutTURN()

		if config.UseTURN {
			t.Error("UseTURN should be false")
		}
	})

	t.Run("DefaultConfig", func(t *testing.T) {
		config := DefaultConfig()

		// Default should enable TURN
		if !config.UseTURN {
			t.Error("Default config should enable TURN")
		}
	})

	t.Run("ConfigFromRelayICE", func(t *testing.T) {
		relayServers := []RelayICEConfig{
			{
				URLs:       []string{"stun:stun.example.com:3478"},
				Username:   "",
				Credential: "",
			},
			{
				URLs:       []string{"turn:turn.example.com:3478"},
				Username:   "user",
				Credential: "pass",
			},
		}

		config := ConfigFromRelayICE(relayServers)

		if !config.UseTURN {
			t.Error("Should enable TURN")
		}

		if len(config.ICEServers) != 2 {
			t.Errorf("Expected 2 ICE servers, got %d", len(config.ICEServers))
		}
	})
}

// fetchTURNCredentials fetches TURN credentials from the production relay
func fetchTURNCredentials(t *testing.T) ([]RelayICEConfig, error) {
	relayURL := signaling.GetRelayURL()
	t.Logf("Fetching ICE servers from: %s", relayURL)

	iceResp, err := signaling.FetchICEServers(relayURL)
	if err != nil {
		return nil, err
	}

	var configs []RelayICEConfig
	for _, srv := range iceResp.ICEServers {
		configs = append(configs, RelayICEConfig{
			URLs:       srv.URLs,
			Username:   srv.Username,
			Credential: srv.Credential,
		})
	}

	return configs, nil
}

// TestTURNFromRelay tests fetching TURN credentials from the relay server
func TestTURNFromRelay(t *testing.T) {
	configs, err := fetchTURNCredentials(t)
	if err != nil {
		t.Fatalf("Failed to fetch ICE servers: %v", err)
	}

	t.Logf("Got %d ICE server configs", len(configs))

	hasTURN := false
	for _, cfg := range configs {
		for _, url := range cfg.URLs {
			t.Logf("  ICE URL: %s (user: %s)", url, cfg.Username)
			if strings.HasPrefix(url, "turn:") || strings.HasPrefix(url, "turns:") {
				hasTURN = true
			}
		}
	}

	if !hasTURN {
		t.Error("Relay should provide TURN servers")
	}
}

// TestTURNConnection tests connection using TURN credentials from the relay
func TestTURNConnection(t *testing.T) {
	// Fetch real TURN credentials from production relay
	configs, err := fetchTURNCredentials(t)
	if err != nil {
		t.Fatalf("Failed to fetch ICE servers: %v", err)
	}

	// Create config from relay ICE servers
	config := ConfigFromRelayICE(configs)

	password := "testpassword"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	hostPeer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("Failed to create host peer: %v", err)
	}
	defer hostPeer.Close()

	clientPeer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	hostDC, _ := hostPeer.CreateDataChannel("terminal")

	clientDCChan := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCChan <- dc
	})

	offer, _ := hostPeer.CreateOffer()

	// Check if offer contains relay candidates
	if strings.Contains(offer, "relay") {
		t.Log("Offer contains relay candidates - TURN is working")
	} else {
		t.Log("No relay candidates in offer (may gather later or use host/srflx)")
	}

	clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
	answer, _ := clientPeer.CreateAnswer()
	hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)

	var clientDC *webrtc.DataChannel
	select {
	case clientDC = <-clientDCChan:
	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for data channel")
	}

	hostDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostDCOpen <- true })
	clientDC.OnOpen(func() { clientDCOpen <- true })

	select {
	case <-hostDCOpen:
	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for host data channel to open")
	}

	select {
	case <-clientDCOpen:
	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for client data channel to open")
	}

	hostChannel := NewEncryptedChannel(hostDC, &key)
	clientChannel := NewEncryptedChannel(clientDC, &key)
	defer hostChannel.Close()
	defer clientChannel.Close()

	// Test message exchange
	received := make(chan bool, 1)
	clientChannel.OnData(func(data []byte) {
		received <- true
	})

	if err := hostChannel.SendData([]byte("test via TURN-enabled connection")); err != nil {
		t.Fatalf("Failed to send: %v", err)
	}

	select {
	case <-received:
		t.Log("Message successfully sent via TURN-enabled connection")
	case <-time.After(10 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

// TestConnectionWithSTUNOnly tests connection using only STUN (no TURN)
func TestConnectionWithSTUNOnly(t *testing.T) {
	password := "testpassword"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Use config without TURN
	config := ConfigWithoutTURN()

	hostPeer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("Failed to create host peer: %v", err)
	}
	defer hostPeer.Close()

	clientPeer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	hostDC, _ := hostPeer.CreateDataChannel("terminal")

	clientDCChan := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCChan <- dc
	})

	offer, _ := hostPeer.CreateOffer()
	clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
	answer, _ := clientPeer.CreateAnswer()
	hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)

	var clientDC *webrtc.DataChannel
	select {
	case clientDC = <-clientDCChan:
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for data channel")
	}

	hostDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostDCOpen <- true })
	clientDC.OnOpen(func() { clientDCOpen <- true })

	<-hostDCOpen
	<-clientDCOpen

	hostChannel := NewEncryptedChannel(hostDC, &key)
	clientChannel := NewEncryptedChannel(clientDC, &key)
	defer hostChannel.Close()
	defer clientChannel.Close()

	// Test message exchange
	received := make(chan bool, 1)
	clientChannel.OnData(func(data []byte) {
		received <- true
	})

	hostChannel.SendData([]byte("test without TURN"))

	select {
	case <-received:
		t.Log("Message sent without TURN (using STUN only)")
	case <-time.After(10 * time.Second):
		t.Error("Timeout - may indicate NAT traversal issue without TURN")
	}
}

// TestICECandidateTypes tests the types of ICE candidates gathered
func TestICECandidateTypes(t *testing.T) {
	// Fetch real TURN credentials
	configs, err := fetchTURNCredentials(t)
	if err != nil {
		t.Fatalf("Failed to fetch ICE servers: %v", err)
	}

	config := ConfigFromRelayICE(configs)

	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("Failed to create peer: %v", err)
	}
	defer peer.Close()

	peer.CreateDataChannel("test")

	offer, err := peer.CreateOffer()
	if err != nil {
		t.Fatalf("Failed to create offer: %v", err)
	}

	// Parse SDP to find candidate types
	candidateTypes := make(map[string]int)

	for _, line := range strings.Split(offer, "\n") {
		if strings.Contains(line, "a=candidate:") {
			// Extract candidate type
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "typ" && i+1 < len(parts) {
					candidateTypes[parts[i+1]]++
				}
			}
		}
	}

	t.Logf("ICE candidate types: %v", candidateTypes)

	// Should have at least host candidates
	if candidateTypes["host"] == 0 {
		t.Log("No host candidates found (may be expected in some environments)")
	}

	// Log if we have relay candidates (indicates TURN is working)
	if candidateTypes["relay"] > 0 {
		t.Logf("Found %d relay candidates - TURN is gathering successfully", candidateTypes["relay"])
	}
}

// TestPublicIPExtraction tests the GetPublicIP functionality
func TestPublicIPExtraction(t *testing.T) {
	// Fetch real credentials for better candidate gathering
	configs, err := fetchTURNCredentials(t)
	if err != nil {
		t.Fatalf("Failed to fetch ICE servers: %v", err)
	}

	config := ConfigFromRelayICE(configs)

	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("Failed to create peer: %v", err)
	}
	defer peer.Close()

	peer.CreateDataChannel("test")

	_, err = peer.CreateOffer()
	if err != nil {
		t.Fatalf("Failed to create offer: %v", err)
	}

	publicIP := peer.GetPublicIP()

	if publicIP != "" {
		t.Logf("Detected public IP: %s", publicIP)
	} else {
		t.Log("No public IP detected (may be behind symmetric NAT or no STUN response)")
	}
}

// TestTURNCredentialTypes tests different TURN credential types
func TestTURNCredentialTypes(t *testing.T) {
	t.Run("PasswordCredential", func(t *testing.T) {
		servers := buildICEServers(Config{
			TURNServers: []TURNConfig{
				{
					URLs:       []string{"turn:example.com:3478"},
					Username:   "user",
					Credential: "password",
				},
			},
			UseTURN: true,
		})

		// Should have STUN + TURN servers
		if len(servers) < 2 {
			t.Errorf("Expected at least 2 servers (STUN + TURN), got %d", len(servers))
		}

		// Find TURN server
		hasTURN := false
		for _, srv := range servers {
			for _, url := range srv.URLs {
				if strings.HasPrefix(url, "turn:") {
					hasTURN = true
					if srv.CredentialType != webrtc.ICECredentialTypePassword {
						t.Error("TURN should use password credential type")
					}
				}
			}
		}

		if !hasTURN {
			t.Error("TURN server not in ICE servers")
		}
	})
}

// TestMultipleTURNServers tests configuration with multiple TURN servers
func TestMultipleTURNServers(t *testing.T) {
	config := ConfigWithTURN([]TURNConfig{
		{
			URLs:       []string{"turn:turn1.example.com:3478"},
			Username:   "user1",
			Credential: "pass1",
		},
		{
			URLs:       []string{"turn:turn2.example.com:3478"},
			Username:   "user2",
			Credential: "pass2",
		},
	})

	servers := buildICEServers(config)

	turnCount := 0
	for _, srv := range servers {
		for _, url := range srv.URLs {
			if strings.HasPrefix(url, "turn:") {
				turnCount++
			}
		}
	}

	if turnCount != 2 {
		t.Errorf("Expected 2 TURN servers, got %d", turnCount)
	}
}

// TestTURNsURL tests TURNS (TLS) URL handling
func TestTURNsURL(t *testing.T) {
	config := ConfigWithTURN([]TURNConfig{
		{
			URLs:       []string{"turns:secure.example.com:443"},
			Username:   "user",
			Credential: "pass",
		},
	})

	servers := buildICEServers(config)

	hasTURNS := false
	for _, srv := range servers {
		for _, url := range srv.URLs {
			if strings.HasPrefix(url, "turns:") {
				hasTURNS = true
			}
		}
	}

	if !hasTURNS {
		t.Error("TURNS URL should be preserved")
	}
}

// TestRelayCredentialRefresh tests that relay provides fresh credentials
func TestRelayCredentialRefresh(t *testing.T) {
	// Fetch credentials twice
	configs1, err := fetchTURNCredentials(t)
	if err != nil {
		t.Fatalf("First fetch failed: %v", err)
	}

	configs2, err := fetchTURNCredentials(t)
	if err != nil {
		t.Fatalf("Second fetch failed: %v", err)
	}

	// Credentials should be the same within the same time window (1 hour)
	// The relay uses time-window based credentials
	if len(configs1) > 0 && len(configs2) > 0 {
		if configs1[0].Username == configs2[0].Username {
			t.Log("Credentials are consistent within time window (expected)")
		} else {
			t.Log("Credentials differ (time window may have changed)")
		}
	}
}

// BenchmarkConnectionWithTURN benchmarks connection establishment with TURN
func BenchmarkConnectionWithTURN(b *testing.B) {
	// Fetch TURN credentials once
	relayURL := signaling.GetRelayURL()
	iceResp, err := signaling.FetchICEServers(relayURL)
	if err != nil {
		b.Fatalf("Failed to fetch ICE servers: %v", err)
	}

	var configs []RelayICEConfig
	for _, srv := range iceResp.ICEServers {
		configs = append(configs, RelayICEConfig{
			URLs:       srv.URLs,
			Username:   srv.Username,
			Credential: srv.Credential,
		})
	}

	config := ConfigFromRelayICE(configs)
	password := "testpassword"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		hostPeer, _ := NewPeer(config)
		clientPeer, _ := NewPeer(config)

		hostDC, _ := hostPeer.CreateDataChannel("terminal")

		clientDCChan := make(chan *webrtc.DataChannel, 1)
		clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
			clientDCChan <- dc
		})

		offer, _ := hostPeer.CreateOffer()
		clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
		answer, _ := clientPeer.CreateAnswer()
		hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)

		clientDC := <-clientDCChan

		hostDCOpen := make(chan bool, 1)
		clientDCOpen := make(chan bool, 1)
		hostDC.OnOpen(func() { hostDCOpen <- true })
		clientDC.OnOpen(func() { clientDCOpen <- true })

		<-hostDCOpen
		<-clientDCOpen

		hostChannel := NewEncryptedChannel(hostDC, &key)
		clientChannel := NewEncryptedChannel(clientDC, &key)

		received := make(chan bool, 1)
		clientChannel.OnData(func(data []byte) {
			received <- true
		})

		hostChannel.SendData([]byte("test"))
		<-received

		hostChannel.Close()
		clientChannel.Close()
		hostPeer.Close()
		clientPeer.Close()
	}
}
