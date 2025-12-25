package webrtc

import (
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestNewPeer(t *testing.T) {
	peer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}
	defer peer.Close()

	if peer.pc == nil {
		t.Error("peer connection should not be nil")
	}
}

func TestNewPeerEmptyConfig(t *testing.T) {
	// Empty config should use defaults
	peer, err := NewPeer(Config{})
	if err != nil {
		t.Fatalf("NewPeer with empty config failed: %v", err)
	}
	defer peer.Close()
}

func TestCreateDataChannel(t *testing.T) {
	peer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}
	defer peer.Close()

	dc, err := peer.CreateDataChannel("terminal")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	if dc.Label() != "terminal" {
		t.Errorf("data channel label = %q, want %q", dc.Label(), "terminal")
	}
}

func TestCreateOffer(t *testing.T) {
	peer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}
	defer peer.Close()

	// Must create data channel before offer
	_, err = peer.CreateDataChannel("terminal")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	sdp, err := peer.CreateOffer()
	if err != nil {
		t.Fatalf("CreateOffer failed: %v", err)
	}

	// SDP should contain expected fields
	if !strings.Contains(sdp, "v=0") {
		t.Error("SDP should contain version line")
	}
	if !strings.Contains(sdp, "a=ice-ufrag") {
		t.Error("SDP should contain ice-ufrag")
	}
	if !strings.Contains(sdp, "a=ice-pwd") {
		t.Error("SDP should contain ice-pwd")
	}
}

func TestPeerConnection(t *testing.T) {
	// Create host peer
	hostPeer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewPeer (host) failed: %v", err)
	}
	defer hostPeer.Close()

	// Create client peer
	clientPeer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewPeer (client) failed: %v", err)
	}
	defer clientPeer.Close()

	// Host creates data channel
	hostDC, err := hostPeer.CreateDataChannel("terminal")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	// Track when client receives data channel
	clientDCReceived := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCReceived <- dc
	})

	// Host creates offer
	offer, err := hostPeer.CreateOffer()
	if err != nil {
		t.Fatalf("CreateOffer failed: %v", err)
	}

	// Client sets remote description (offer)
	err = clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
	if err != nil {
		t.Fatalf("SetRemoteDescription (offer) failed: %v", err)
	}

	// Client creates answer
	answer, err := clientPeer.CreateAnswer()
	if err != nil {
		t.Fatalf("CreateAnswer failed: %v", err)
	}

	// Host sets remote description (answer)
	err = hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)
	if err != nil {
		t.Fatalf("SetRemoteDescription (answer) failed: %v", err)
	}

	// Wait for connection
	connected := make(chan bool)
	hostPeer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected {
			connected <- true
		}
	})

	// Wait for data channel on client side
	select {
	case dc := <-clientDCReceived:
		if dc.Label() != "terminal" {
			t.Errorf("received data channel label = %q, want %q", dc.Label(), "terminal")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for data channel")
	}

	// Wait for host data channel to open
	hostDCOpen := make(chan bool)
	hostDC.OnOpen(func() {
		hostDCOpen <- true
	})

	select {
	case <-hostDCOpen:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for host data channel to open")
	}
}

func TestConnectionState(t *testing.T) {
	peer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}
	defer peer.Close()

	state := peer.ConnectionState()
	if state != webrtc.PeerConnectionStateNew {
		t.Errorf("initial state = %v, want New", state)
	}
}
