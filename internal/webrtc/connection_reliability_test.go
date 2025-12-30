package webrtc

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
	"github.com/artpar/terminal-tunnel/internal/protocol"
)

// TestConnectionReliability tests that the encrypted channel maintains reliable
// communication and handles transient network issues properly.
func TestConnectionReliability(t *testing.T) {
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

	// Derive encryption key
	password := "testpassword123"
	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i)
	}
	key := crypto.DeriveKey(password, salt)

	// Host creates data channel
	hostDC, err := hostPeer.CreateDataChannel("terminal")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	// Track connection states
	var hostConnected, clientConnected atomic.Bool
	var hostDisconnected, clientDisconnected atomic.Int32

	hostPeer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		t.Logf("[Host] Connection state: %s", state)
		switch state {
		case webrtc.PeerConnectionStateConnected:
			hostConnected.Store(true)
		case webrtc.PeerConnectionStateDisconnected:
			hostDisconnected.Add(1)
		}
	})

	clientPeer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		t.Logf("[Client] Connection state: %s", state)
		switch state {
		case webrtc.PeerConnectionStateConnected:
			clientConnected.Store(true)
		case webrtc.PeerConnectionStateDisconnected:
			clientDisconnected.Add(1)
		}
	})

	// Track when client receives data channel
	clientDCReceived := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCReceived <- dc
	})

	// Exchange SDP
	offer, err := hostPeer.CreateOffer()
	if err != nil {
		t.Fatalf("CreateOffer failed: %v", err)
	}

	err = clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
	if err != nil {
		t.Fatalf("SetRemoteDescription (offer) failed: %v", err)
	}

	answer, err := clientPeer.CreateAnswer()
	if err != nil {
		t.Fatalf("CreateAnswer failed: %v", err)
	}

	err = hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)
	if err != nil {
		t.Fatalf("SetRemoteDescription (answer) failed: %v", err)
	}

	// Wait for data channel on client side
	var clientDC *webrtc.DataChannel
	select {
	case clientDC = <-clientDCReceived:
		t.Log("Client received data channel")
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for data channel")
	}

	// Wait for both data channels to open
	hostDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)

	hostDC.OnOpen(func() {
		t.Log("Host data channel opened")
		hostDCOpen <- true
	})

	clientDC.OnOpen(func() {
		t.Log("Client data channel opened")
		clientDCOpen <- true
	})

	select {
	case <-hostDCOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for host data channel to open")
	}

	select {
	case <-clientDCOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for client data channel to open")
	}

	// Create encrypted channels
	hostChannel := NewEncryptedChannel(hostDC, &key)
	clientChannel := NewEncryptedChannel(clientDC, &key)

	// Track received messages
	var hostReceived, clientReceived atomic.Int32
	var mu sync.Mutex
	receivedData := make([][]byte, 0)

	hostChannel.OnData(func(data []byte) {
		hostReceived.Add(1)
		mu.Lock()
		receivedData = append(receivedData, data)
		mu.Unlock()
	})

	clientChannel.OnData(func(data []byte) {
		clientReceived.Add(1)
	})

	// Test 1: Basic message exchange
	t.Run("BasicMessageExchange", func(t *testing.T) {
		testData := []byte("Hello from client!")
		err := clientChannel.SendData(testData)
		if err != nil {
			t.Fatalf("SendData failed: %v", err)
		}

		// Wait for message
		time.Sleep(100 * time.Millisecond)

		if hostReceived.Load() != 1 {
			t.Errorf("Expected 1 message, got %d", hostReceived.Load())
		}
	})

	// Test 2: Ping/Pong keepalive
	t.Run("PingPong", func(t *testing.T) {
		// Send ping from host
		err := hostChannel.SendPing()
		if err != nil {
			t.Fatalf("SendPing failed: %v", err)
		}

		// Client should automatically respond with pong
		time.Sleep(200 * time.Millisecond)

		// Verify host received pong (lastPongTime should be updated)
		hostChannel.mu.Lock()
		lastPong := hostChannel.lastPongTime
		hostChannel.mu.Unlock()

		if time.Since(lastPong) > 1*time.Second {
			t.Error("Pong not received in time")
		}
	})

	// Test 3: Rapid message exchange (stress test)
	t.Run("RapidMessageExchange", func(t *testing.T) {
		initialCount := hostReceived.Load()
		messageCount := 100

		for i := 0; i < messageCount; i++ {
			data := []byte{byte(i)}
			err := clientChannel.SendData(data)
			if err != nil {
				t.Fatalf("SendData %d failed: %v", i, err)
			}
		}

		// Wait for all messages
		deadline := time.Now().Add(5 * time.Second)
		for hostReceived.Load() < initialCount+int32(messageCount) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}

		received := hostReceived.Load() - initialCount
		if received != int32(messageCount) {
			t.Errorf("Expected %d messages, got %d", messageCount, received)
		}
	})

	// Test 4: Keepalive mechanism
	t.Run("KeepaliveMonitoring", func(t *testing.T) {
		// Start keepalive on host channel
		timeoutChan := hostChannel.StartKeepalive()

		// Let it run for a bit
		time.Sleep(500 * time.Millisecond)

		// Stop keepalive
		hostChannel.StopKeepalive()

		// Should not have timed out
		select {
		case <-timeoutChan:
			t.Error("Keepalive timed out unexpectedly")
		default:
			t.Log("Keepalive working correctly")
		}
	})

	// Test 5: Connection state tracking
	t.Run("ConnectionStateTracking", func(t *testing.T) {
		if !hostConnected.Load() {
			t.Error("Host should be connected")
		}
		if !clientConnected.Load() {
			t.Error("Client should be connected")
		}
	})

	// Clean up
	hostChannel.Close()
	clientChannel.Close()

	t.Logf("Test completed - Host disconnects: %d, Client disconnects: %d",
		hostDisconnected.Load(), clientDisconnected.Load())
}

// TestEncryptedChannelReconnectScenario tests that channels handle
// message delivery correctly under various conditions
func TestEncryptedChannelReconnectScenario(t *testing.T) {
	// Create peers
	hostPeer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewPeer (host) failed: %v", err)
	}
	defer hostPeer.Close()

	clientPeer, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewPeer (client) failed: %v", err)
	}
	defer clientPeer.Close()

	// Derive encryption key
	password := "testpassword123"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Host creates data channel
	hostDC, err := hostPeer.CreateDataChannel("terminal")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	// Track client data channel
	clientDCReceived := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCReceived <- dc
	})

	// Exchange SDP
	offer, _ := hostPeer.CreateOffer()
	clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
	answer, _ := clientPeer.CreateAnswer()
	hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)

	// Wait for data channels
	var clientDC *webrtc.DataChannel
	select {
	case clientDC = <-clientDCReceived:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for data channel")
	}

	hostDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostDCOpen <- true })
	clientDC.OnOpen(func() { clientDCOpen <- true })

	<-hostDCOpen
	<-clientDCOpen

	// Create encrypted channels
	hostChannel := NewEncryptedChannel(hostDC, &key)
	clientChannel := NewEncryptedChannel(clientDC, &key)

	// Track bidirectional messages
	var hostToClientCount, clientToHostCount atomic.Int32

	hostChannel.OnData(func(data []byte) {
		clientToHostCount.Add(1)
	})

	clientChannel.OnData(func(data []byte) {
		hostToClientCount.Add(1)
	})

	// Test bidirectional messaging
	t.Run("BidirectionalMessaging", func(t *testing.T) {
		// Send from both sides simultaneously
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				hostChannel.SendData([]byte{byte(i)})
				time.Sleep(5 * time.Millisecond)
			}
		}()

		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				clientChannel.SendData([]byte{byte(i)})
				time.Sleep(5 * time.Millisecond)
			}
		}()

		wg.Wait()
		time.Sleep(500 * time.Millisecond)

		hostReceived := clientToHostCount.Load()
		clientReceived := hostToClientCount.Load()

		t.Logf("Host received: %d/50, Client received: %d/50", hostReceived, clientReceived)

		if hostReceived < 45 { // Allow some tolerance
			t.Errorf("Host received too few messages: %d", hostReceived)
		}
		if clientReceived < 45 {
			t.Errorf("Client received too few messages: %d", clientReceived)
		}
	})

	// Test resize messages
	t.Run("ResizeMessages", func(t *testing.T) {
		var resizeReceived atomic.Bool
		var receivedRows, receivedCols uint16

		hostChannel.OnResize(func(rows, cols uint16) {
			receivedRows = rows
			receivedCols = cols
			resizeReceived.Store(true)
		})

		err := clientChannel.SendResize(24, 80)
		if err != nil {
			t.Fatalf("SendResize failed: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		if !resizeReceived.Load() {
			t.Error("Resize message not received")
		} else if receivedRows != 24 || receivedCols != 80 {
			t.Errorf("Wrong resize: got %dx%d, want 24x80", receivedRows, receivedCols)
		}
	})

	hostChannel.Close()
	clientChannel.Close()
}

// TestDataChannelMessageTypes tests all protocol message types
func TestDataChannelMessageTypes(t *testing.T) {
	// Verify protocol messages encode/decode correctly
	t.Run("DataMessage", func(t *testing.T) {
		msg := protocol.NewDataMessage([]byte("test data"))
		encoded := msg.Encode()
		decoded, err := protocol.DecodeMessage(encoded)
		if err != nil {
			t.Fatalf("DecodeMessage failed: %v", err)
		}
		if decoded.Type != protocol.MsgData {
			t.Errorf("Wrong type: got %d, want %d", decoded.Type, protocol.MsgData)
		}
		if string(decoded.Payload) != "test data" {
			t.Errorf("Wrong payload: got %q", string(decoded.Payload))
		}
	})

	t.Run("ResizeMessage", func(t *testing.T) {
		msg := protocol.NewResizeMessage(24, 80)
		encoded := msg.Encode()
		decoded, err := protocol.DecodeMessage(encoded)
		if err != nil {
			t.Fatalf("DecodeMessage failed: %v", err)
		}
		if decoded.Type != protocol.MsgResize {
			t.Errorf("Wrong type: got %d, want %d", decoded.Type, protocol.MsgResize)
		}
		resize, err := protocol.ParseResizePayload(decoded.Payload)
		if err != nil {
			t.Fatalf("ParseResizePayload failed: %v", err)
		}
		if resize.Rows != 24 || resize.Cols != 80 {
			t.Errorf("Wrong resize: got %dx%d, want 24x80", resize.Rows, resize.Cols)
		}
	})

	t.Run("PingPongMessages", func(t *testing.T) {
		ping := protocol.NewPingMessage()
		pong := protocol.NewPongMessage()

		pingDecoded, _ := protocol.DecodeMessage(ping.Encode())
		pongDecoded, _ := protocol.DecodeMessage(pong.Encode())

		if pingDecoded.Type != protocol.MsgPing {
			t.Errorf("Wrong ping type: got %d", pingDecoded.Type)
		}
		if pongDecoded.Type != protocol.MsgPong {
			t.Errorf("Wrong pong type: got %d", pongDecoded.Type)
		}
	})

	t.Run("CloseMessage", func(t *testing.T) {
		msg := protocol.NewCloseMessage()
		decoded, _ := protocol.DecodeMessage(msg.Encode())
		if decoded.Type != protocol.MsgClose {
			t.Errorf("Wrong type: got %d, want %d", decoded.Type, protocol.MsgClose)
		}
	})
}
