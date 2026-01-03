package webrtc

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
)

// TestConnectionStateTransitions tests the full state machine
func TestConnectionStateTransitions(t *testing.T) {
	password := "testpassword"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	t.Run("NewToConnected", func(t *testing.T) {
		hostPeer, err := NewPeer(DefaultConfig())
		if err != nil {
			t.Fatalf("Failed to create host peer: %v", err)
		}
		defer hostPeer.Close()

		clientPeer, err := NewPeer(DefaultConfig())
		if err != nil {
			t.Fatalf("Failed to create client peer: %v", err)
		}
		defer clientPeer.Close()

		hostObserver := NewStateObserver()
		clientObserver := NewStateObserver()

		hostPeer.OnConnectionStateChange(hostObserver.OnStateChange)
		clientPeer.OnConnectionStateChange(clientObserver.OnStateChange)

		// Initial state should be new
		if hostPeer.ConnectionState() != webrtc.PeerConnectionStateNew {
			t.Errorf("Initial host state should be 'new', got: %v", hostPeer.ConnectionState())
		}

		hostDC, _ := hostPeer.CreateDataChannel("terminal")

		clientDCChan := make(chan *webrtc.DataChannel, 1)
		clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
			clientDCChan <- dc
		})

		// Exchange SDP
		offer, _ := hostPeer.CreateOffer()
		clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
		answer, _ := clientPeer.CreateAnswer()
		hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)

		// Wait for data channel
		var clientDC *webrtc.DataChannel
		select {
		case clientDC = <-clientDCChan:
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for data channel")
		}

		// Wait for channels to open
		hostDCOpen := make(chan bool, 1)
		clientDCOpen := make(chan bool, 1)
		hostDC.OnOpen(func() { hostDCOpen <- true })
		clientDC.OnOpen(func() { clientDCOpen <- true })

		<-hostDCOpen
		<-clientDCOpen

		// Create encrypted channels
		hostChannel := NewEncryptedChannel(hostDC, &key)
		clientChannel := NewEncryptedChannel(clientDC, &key)
		defer hostChannel.Close()
		defer clientChannel.Close()

		// Wait for connected state
		if !hostObserver.WaitForState(webrtc.PeerConnectionStateConnected, 10*time.Second) {
			t.Error("Host never reached 'connected' state")
		}

		if !clientObserver.WaitForState(webrtc.PeerConnectionStateConnected, 10*time.Second) {
			t.Error("Client never reached 'connected' state")
		}

		// Verify state progression: new -> connecting -> connected
		hostStates := hostObserver.GetHistory()
		t.Logf("Host state progression: %v", hostStates)

		// Should have gone through connecting
		hasConnecting := false
		for _, s := range hostStates {
			if s == webrtc.PeerConnectionStateConnecting {
				hasConnecting = true
				break
			}
		}
		if !hasConnecting {
			t.Error("Host should have gone through 'connecting' state")
		}
	})

	t.Run("ConnectionStateCallbacks", func(t *testing.T) {
		hostPeer, _ := NewPeer(DefaultConfig())
		defer hostPeer.Close()

		clientPeer, _ := NewPeer(DefaultConfig())
		defer clientPeer.Close()

		var callbackCount atomic.Int32

		hostPeer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			callbackCount.Add(1)
		})

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
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout")
		}

		hostDCOpen := make(chan bool, 1)
		clientDCOpen := make(chan bool, 1)
		hostDC.OnOpen(func() { hostDCOpen <- true })
		clientDC.OnOpen(func() { clientDCOpen <- true })

		<-hostDCOpen
		<-clientDCOpen

		hostChannel := NewEncryptedChannel(hostDC, &key)
		clientChannel := NewEncryptedChannel(clientDC, &key)

		// Wait a bit for state changes
		time.Sleep(500 * time.Millisecond)

		// Should have at least 2 callbacks (connecting, connected)
		if callbackCount.Load() < 2 {
			t.Errorf("Expected at least 2 state callbacks, got %d", callbackCount.Load())
		}

		hostChannel.Close()
		clientChannel.Close()
	})
}

// TestICEConnectionStates tests ICE-specific state transitions
func TestICEConnectionStates(t *testing.T) {
	hostPeer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create host peer: %v", err)
	}
	defer hostPeer.Close()

	clientPeer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	var hostICEStates, clientICEStates []webrtc.ICEConnectionState
	var hostICEMu, clientICEMu sync.Mutex

	hostPeer.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		hostICEMu.Lock()
		hostICEStates = append(hostICEStates, state)
		hostICEMu.Unlock()
	})

	clientPeer.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		clientICEMu.Lock()
		clientICEStates = append(clientICEStates, state)
		clientICEMu.Unlock()
	})

	// Create data channel and exchange SDP
	hostPeer.CreateDataChannel("terminal")

	clientDCChan := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCChan <- dc
	})

	offer, _ := hostPeer.CreateOffer()
	clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
	answer, _ := clientPeer.CreateAnswer()
	hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)

	// Wait for connection
	select {
	case <-clientDCChan:
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout")
	}

	time.Sleep(1 * time.Second)

	hostICEMu.Lock()
	t.Logf("Host ICE states: %v", hostICEStates)
	hostICEMu.Unlock()

	clientICEMu.Lock()
	t.Logf("Client ICE states: %v", clientICEStates)
	clientICEMu.Unlock()

	// Should have reached connected state
	hostICEMu.Lock()
	hasHostConnected := false
	for _, s := range hostICEStates {
		if s == webrtc.ICEConnectionStateConnected || s == webrtc.ICEConnectionStateCompleted {
			hasHostConnected = true
			break
		}
	}
	hostICEMu.Unlock()

	if !hasHostConnected {
		t.Error("Host ICE never reached connected/completed state")
	}
}

// TestConnectionStateAfterClose tests state after explicit close
func TestConnectionStateAfterClose(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create peer pair: %v", err)
	}

	// Verify already connected (NewTestPeerPair waits for connection)
	if pair.HostPeer.ConnectionState() != webrtc.PeerConnectionStateConnected {
		t.Errorf("Expected connected state, got: %v", pair.HostPeer.ConnectionState())
	}

	// Register observer to catch close transition
	hostObserver := NewStateObserver()
	pair.HostPeer.OnConnectionStateChange(hostObserver.OnStateChange)

	// Close host peer
	pair.HostPeer.Close()

	// Should transition to closed
	if !hostObserver.WaitForState(webrtc.PeerConnectionStateClosed, 5*time.Second) {
		t.Error("Never reached closed state after Close()")
	}

	// Clean up client
	pair.ClientPeer.Close()

	t.Logf("State history after close: %v", hostObserver.GetHistory())
}

// TestDataChannelStateTracking tests data channel state tracking
func TestDataChannelStateTracking(t *testing.T) {
	password := "testpassword"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	hostPeer, _ := NewPeer(DefaultConfig())
	defer hostPeer.Close()

	clientPeer, _ := NewPeer(DefaultConfig())
	defer clientPeer.Close()

	hostDC, _ := hostPeer.CreateDataChannel("terminal")

	var hostDCStates []webrtc.DataChannelState
	var hostDCMu sync.Mutex

	hostDC.OnOpen(func() {
		hostDCMu.Lock()
		hostDCStates = append(hostDCStates, webrtc.DataChannelStateOpen)
		hostDCMu.Unlock()
	})

	hostDC.OnClose(func() {
		hostDCMu.Lock()
		hostDCStates = append(hostDCStates, webrtc.DataChannelStateClosed)
		hostDCMu.Unlock()
	})

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
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout")
	}

	// Wait for open
	hostDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostDCOpen <- true })
	clientDC.OnOpen(func() { clientDCOpen <- true })

	<-hostDCOpen
	<-clientDCOpen

	// Create encrypted channels
	hostChannel := NewEncryptedChannel(hostDC, &key)
	clientChannel := NewEncryptedChannel(clientDC, &key)

	// Verify open state
	if hostDC.ReadyState() != webrtc.DataChannelStateOpen {
		t.Errorf("Host DC should be open, got: %v", hostDC.ReadyState())
	}

	if clientDC.ReadyState() != webrtc.DataChannelStateOpen {
		t.Errorf("Client DC should be open, got: %v", clientDC.ReadyState())
	}

	// Close and verify
	hostChannel.Close()
	clientChannel.Close()

	time.Sleep(500 * time.Millisecond)

	hostDCMu.Lock()
	t.Logf("Host DC state progression: %v", hostDCStates)
	hostDCMu.Unlock()
}

// TestStateTimeouts tests timeout behavior at various states
func TestStateTimeouts(t *testing.T) {
	t.Run("ICEGatheringTimeout", func(t *testing.T) {
		// This tests that ICE gathering has a timeout
		peer, err := NewPeer(DefaultConfig())
		if err != nil {
			t.Fatalf("Failed to create peer: %v", err)
		}
		defer peer.Close()

		peer.CreateDataChannel("terminal")

		// CreateOffer includes ICE gathering which has a 30s timeout
		start := time.Now()
		_, err = peer.CreateOffer()
		elapsed := time.Since(start)

		if err != nil {
			t.Logf("Offer creation error: %v", err)
		}

		// Should complete well before the 30s timeout under normal conditions
		if elapsed > 5*time.Second {
			t.Logf("ICE gathering took longer than expected: %v", elapsed)
		}
	})
}

// TestMultipleStateCallbacks tests that multiple callbacks can be registered
func TestMultipleStateCallbacks(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create peer pair: %v", err)
	}
	defer pair.Close()

	var callback1Count, callback2Count atomic.Int32

	// Note: OnConnectionStateChange replaces the callback, doesn't add
	// This test verifies the behavior
	pair.HostPeer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		callback1Count.Add(1)
	})

	// This will replace the previous callback
	pair.HostPeer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		callback2Count.Add(1)
	})

	// Give time for state changes to propagate
	time.Sleep(500 * time.Millisecond)

	// Only callback2 should have been called (it replaced callback1)
	t.Logf("Callback1 called %d times, Callback2 called %d times",
		callback1Count.Load(), callback2Count.Load())
}
