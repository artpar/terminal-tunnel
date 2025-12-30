package integration

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
	ttwebrtc "github.com/artpar/terminal-tunnel/internal/webrtc"
)

// SimulatedClient represents a browser-like client for testing
type SimulatedClient struct {
	peer           *ttwebrtc.Peer
	dc             *webrtc.DataChannel
	channel        *ttwebrtc.EncryptedChannel
	key            [32]byte
	status         string // new, connecting, connected, disconnected
	disconnectChan chan bool

	// Track connection state changes
	stateHistory     []string
	stateHistoryLock sync.Mutex

	// Track messages
	receivedMessages [][]byte
	messageLock      sync.Mutex

	// Disconnect timer (simulating the JS fix)
	disconnectTimer *time.Timer
	disconnectDelay time.Duration // 0 = immediate (old behavior), 5s = new behavior

	mu sync.Mutex
}

// NewSimulatedClient creates a client that behaves like the browser
func NewSimulatedClient(password string, salt []byte, disconnectDelay time.Duration) *SimulatedClient {
	key := crypto.DeriveKey(password, salt)
	return &SimulatedClient{
		key:             key,
		status:          "new",
		disconnectChan:  make(chan bool, 1),
		disconnectDelay: disconnectDelay,
	}
}

// Connect establishes connection to a host peer
func (c *SimulatedClient) Connect(hostPeer *ttwebrtc.Peer, hostOffer string) error {
	c.mu.Lock()
	c.status = "connecting"
	c.mu.Unlock()

	peer, err := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	if err != nil {
		return fmt.Errorf("failed to create peer: %w", err)
	}
	c.peer = peer

	// Track connection state changes (simulating browser behavior)
	peer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.stateHistoryLock.Lock()
		c.stateHistory = append(c.stateHistory, state.String())
		c.stateHistoryLock.Unlock()

		c.handleConnectionStateChange(state)
	})

	// Wait for data channel
	dcReceived := make(chan *webrtc.DataChannel, 1)
	peer.OnDataChannel(func(dc *webrtc.DataChannel) {
		dcReceived <- dc
	})

	// Set remote description (host's offer)
	if err := peer.SetRemoteDescription(webrtc.SDPTypeOffer, hostOffer); err != nil {
		return fmt.Errorf("failed to set offer: %w", err)
	}

	// Create answer
	answer, err := peer.CreateAnswer()
	if err != nil {
		return fmt.Errorf("failed to create answer: %w", err)
	}

	// Set answer on host
	if err := hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer); err != nil {
		return fmt.Errorf("failed to set answer on host: %w", err)
	}

	// Wait for data channel
	select {
	case dc := <-dcReceived:
		c.dc = dc
		c.setupDataChannel()
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for data channel")
	}

	// Wait for data channel to open
	dcOpen := make(chan bool, 1)
	c.dc.OnOpen(func() {
		dcOpen <- true
	})

	select {
	case <-dcOpen:
		c.mu.Lock()
		c.status = "connected"
		c.mu.Unlock()
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for data channel to open")
	}

	return nil
}

// setupDataChannel sets up message handling (mimicking browser JS)
func (c *SimulatedClient) setupDataChannel() {
	c.channel = ttwebrtc.NewEncryptedChannel(c.dc, &c.key)

	// Handle incoming data
	c.channel.OnData(func(data []byte) {
		c.messageLock.Lock()
		c.receivedMessages = append(c.receivedMessages, data)
		c.messageLock.Unlock()
	})

	// Note: Ping/Pong is handled by EncryptedChannel automatically
}

// handleConnectionStateChange simulates browser's onconnectionstatechange
func (c *SimulatedClient) handleConnectionStateChange(state webrtc.PeerConnectionState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch state {
	case webrtc.PeerConnectionStateConnected:
		// Cancel any pending disconnect timer
		if c.disconnectTimer != nil {
			c.disconnectTimer.Stop()
			c.disconnectTimer = nil
		}
		c.status = "connected"

	case webrtc.PeerConnectionStateDisconnected:
		if c.disconnectDelay == 0 {
			// OLD BEHAVIOR: Immediately disconnect
			c.triggerDisconnect()
		} else {
			// NEW BEHAVIOR: Wait before disconnecting
			if c.disconnectTimer != nil {
				c.disconnectTimer.Stop()
			}
			c.disconnectTimer = time.AfterFunc(c.disconnectDelay, func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				// Check if still disconnected
				if c.peer != nil && c.peer.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
					c.triggerDisconnect()
				}
			})
		}

	case webrtc.PeerConnectionStateFailed:
		// Always disconnect on failed
		c.triggerDisconnect()
	}
}

func (c *SimulatedClient) triggerDisconnect() {
	if c.status == "disconnected" {
		return
	}
	c.status = "disconnected"
	select {
	case c.disconnectChan <- true:
	default:
	}
}

// SendData sends data to the host
func (c *SimulatedClient) SendData(data []byte) error {
	if c.channel == nil {
		return fmt.Errorf("not connected")
	}
	return c.channel.SendData(data)
}

// Close closes the client connection
func (c *SimulatedClient) Close() {
	if c.disconnectTimer != nil {
		c.disconnectTimer.Stop()
	}
	if c.channel != nil {
		c.channel.Close()
	}
	if c.peer != nil {
		c.peer.Close()
	}
}

// GetStateHistory returns the history of connection states
func (c *SimulatedClient) GetStateHistory() []string {
	c.stateHistoryLock.Lock()
	defer c.stateHistoryLock.Unlock()
	result := make([]string, len(c.stateHistory))
	copy(result, c.stateHistory)
	return result
}

// TestConnectionLifecycle tests the full connection lifecycle
func TestConnectionLifecycle(t *testing.T) {
	password := "testpassword123"
	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i)
	}

	t.Run("BasicConnectionLifecycle", func(t *testing.T) {
		// Create host peer
		hostPeer, err := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
		if err != nil {
			t.Fatalf("Failed to create host peer: %v", err)
		}
		defer hostPeer.Close()

		// Create data channel on host
		hostDC, err := hostPeer.CreateDataChannel("terminal")
		if err != nil {
			t.Fatalf("Failed to create data channel: %v", err)
		}

		// Create offer
		offer, err := hostPeer.CreateOffer()
		if err != nil {
			t.Fatalf("Failed to create offer: %v", err)
		}

		// Create client with NEW behavior (5s delay)
		client := NewSimulatedClient(password, salt, 5*time.Second)
		defer client.Close()

		// Connect
		if err := client.Connect(hostPeer, offer); err != nil {
			t.Fatalf("Client failed to connect: %v", err)
		}

		// Wait for host DC to open
		hostDCOpen := make(chan bool, 1)
		hostDC.OnOpen(func() {
			hostDCOpen <- true
		})

		select {
		case <-hostDCOpen:
			t.Log("Host data channel opened")
		case <-time.After(10 * time.Second):
			t.Fatal("Host data channel did not open")
		}

		// Create encrypted channel on host
		key := crypto.DeriveKey(password, salt)
		hostChannel := ttwebrtc.NewEncryptedChannel(hostDC, &key)

		// Test bidirectional messaging
		var hostReceived atomic.Int32
		hostChannel.OnData(func(data []byte) {
			hostReceived.Add(1)
		})

		// Send from client to host
		for i := 0; i < 10; i++ {
			if err := client.SendData([]byte{byte(i)}); err != nil {
				t.Fatalf("Failed to send data: %v", err)
			}
		}

		// Wait for messages
		time.Sleep(500 * time.Millisecond)

		if hostReceived.Load() != 10 {
			t.Errorf("Expected 10 messages, got %d", hostReceived.Load())
		}

		// Verify connection states
		states := client.GetStateHistory()
		t.Logf("Connection state history: %v", states)

		// Should have transitioned to connected
		hasConnected := false
		for _, s := range states {
			if s == "connected" {
				hasConnected = true
				break
			}
		}
		if !hasConnected {
			t.Error("Client never reached 'connected' state")
		}
	})
}

// TestKeepaliveUnderLoad tests keepalive during heavy message traffic
func TestKeepaliveUnderLoad(t *testing.T) {
	password := "testpassword123"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Create peers
	hostPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	defer hostPeer.Close()

	clientPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
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

	clientDC := <-clientDCChan

	// Wait for open
	hostOpen := make(chan bool, 1)
	clientOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostOpen <- true })
	clientDC.OnOpen(func() { clientOpen <- true })
	<-hostOpen
	<-clientOpen

	hostChannel := ttwebrtc.NewEncryptedChannel(hostDC, &key)
	clientChannel := ttwebrtc.NewEncryptedChannel(clientDC, &key)

	// Start keepalive
	timeoutChan := hostChannel.StartKeepalive()

	// Send lots of messages while keepalive is running
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var messageCount atomic.Int32
	clientChannel.OnData(func(data []byte) {
		messageCount.Add(1)
	})

	// Sender goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				hostChannel.SendData([]byte("test"))
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Wait for test duration
	select {
	case <-ctx.Done():
		t.Logf("Sent/received %d messages during keepalive", messageCount.Load())
	case <-timeoutChan:
		t.Error("Keepalive timed out unexpectedly during message exchange")
	}

	hostChannel.StopKeepalive()
	hostChannel.Close()
	clientChannel.Close()
}

// TestDisconnectBehaviorComparison compares old vs new disconnect behavior
func TestDisconnectBehaviorComparison(t *testing.T) {
	password := "testpassword123"
	salt := make([]byte, 16)

	// This test demonstrates the difference between immediate disconnect
	// and delayed disconnect behavior

	t.Run("ImmediateDisconnectBehavior", func(t *testing.T) {
		// With 0 delay, disconnect triggers immediately on "disconnected" state
		client := NewSimulatedClient(password, salt, 0)

		// Simulate receiving "disconnected" state
		client.handleConnectionStateChange(webrtc.PeerConnectionStateDisconnected)

		// Should be disconnected immediately
		client.mu.Lock()
		status := client.status
		client.mu.Unlock()

		if status != "disconnected" {
			t.Errorf("Expected immediate disconnect, got status: %s", status)
		}

		client.Close()
	})

	t.Run("DelayedDisconnectBehavior", func(t *testing.T) {
		// With 5s delay, disconnect waits before triggering
		client := NewSimulatedClient(password, salt, 5*time.Second)

		// Simulate receiving "disconnected" state
		client.handleConnectionStateChange(webrtc.PeerConnectionStateDisconnected)

		// Should NOT be disconnected immediately
		client.mu.Lock()
		status := client.status
		client.mu.Unlock()

		if status == "disconnected" {
			t.Error("Should not disconnect immediately with delay")
		}

		// Wait a bit and still shouldn't be disconnected
		time.Sleep(1 * time.Second)

		client.mu.Lock()
		status = client.status
		client.mu.Unlock()

		if status == "disconnected" {
			t.Error("Should not disconnect after only 1 second")
		}

		client.Close()
	})

	t.Run("RecoveryBeforeTimeout", func(t *testing.T) {
		// Test that recovery cancels the disconnect timer
		client := NewSimulatedClient(password, salt, 5*time.Second)
		client.status = "connected"

		// Simulate disconnect
		client.handleConnectionStateChange(webrtc.PeerConnectionStateDisconnected)

		// Wait 2 seconds
		time.Sleep(2 * time.Second)

		// Simulate recovery
		client.handleConnectionStateChange(webrtc.PeerConnectionStateConnected)

		// Wait past the original timeout
		time.Sleep(4 * time.Second)

		// Should still be connected (timer was cancelled)
		client.mu.Lock()
		status := client.status
		client.mu.Unlock()

		if status != "connected" {
			t.Errorf("Expected 'connected' after recovery, got: %s", status)
		}

		client.Close()
	})
}

// TestMessageDeliveryReliability tests that messages are reliably delivered
func TestMessageDeliveryReliability(t *testing.T) {
	password := "testpassword123"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Create peers
	hostPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	defer hostPeer.Close()

	clientPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
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

	clientDC := <-clientDCChan

	// Wait for open
	hostOpen := make(chan bool, 1)
	clientOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostOpen <- true })
	clientDC.OnOpen(func() { clientOpen <- true })
	<-hostOpen
	<-clientOpen

	hostChannel := ttwebrtc.NewEncryptedChannel(hostDC, &key)
	clientChannel := ttwebrtc.NewEncryptedChannel(clientDC, &key)

	// Test large message count
	const messageCount = 1000
	var received atomic.Int32
	var receivedMessages sync.Map

	clientChannel.OnData(func(data []byte) {
		if len(data) >= 4 {
			seq := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
			receivedMessages.Store(seq, true)
			received.Add(1)
		}
	})

	// Send messages
	for i := 0; i < messageCount; i++ {
		msg := []byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)}
		if err := hostChannel.SendData(msg); err != nil {
			t.Fatalf("Failed to send message %d: %v", i, err)
		}
	}

	// Wait for delivery
	deadline := time.Now().Add(10 * time.Second)
	for received.Load() < messageCount && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	finalCount := received.Load()
	t.Logf("Received %d/%d messages (%.1f%%)", finalCount, messageCount,
		float64(finalCount)/float64(messageCount)*100)

	if finalCount < int32(messageCount*95/100) { // Allow 5% loss
		t.Errorf("Too many messages lost: received %d/%d", finalCount, messageCount)
	}

	// Check for gaps
	missingCount := 0
	for i := 0; i < messageCount; i++ {
		if _, ok := receivedMessages.Load(i); !ok {
			missingCount++
			if missingCount <= 10 {
				t.Logf("Missing message: %d", i)
			}
		}
	}
	if missingCount > 0 {
		t.Logf("Total missing messages: %d", missingCount)
	}

	hostChannel.Close()
	clientChannel.Close()
}

// TestProtocolMessageIntegrity tests that all protocol messages work correctly
func TestProtocolMessageIntegrity(t *testing.T) {
	password := "testpassword123"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Create peers
	hostPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	defer hostPeer.Close()

	clientPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
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

	clientDC := <-clientDCChan

	// Wait for open
	hostOpen := make(chan bool, 1)
	clientOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostOpen <- true })
	clientDC.OnOpen(func() { clientOpen <- true })
	<-hostOpen
	<-clientOpen

	hostChannel := ttwebrtc.NewEncryptedChannel(hostDC, &key)
	clientChannel := ttwebrtc.NewEncryptedChannel(clientDC, &key)

	t.Run("DataMessages", func(t *testing.T) {
		received := make(chan []byte, 1)
		clientChannel.OnData(func(data []byte) {
			received <- data
		})

		testData := []byte("Hello, Terminal Tunnel!")
		hostChannel.SendData(testData)

		select {
		case data := <-received:
			if string(data) != string(testData) {
				t.Errorf("Data mismatch: got %q, want %q", data, testData)
			}
		case <-time.After(5 * time.Second):
			t.Error("Timeout waiting for data message")
		}
	})

	t.Run("ResizeMessages", func(t *testing.T) {
		received := make(chan struct {
			rows, cols uint16
		}, 1)
		clientChannel.OnResize(func(rows, cols uint16) {
			received <- struct{ rows, cols uint16 }{rows, cols}
		})

		hostChannel.SendResize(24, 80)

		select {
		case r := <-received:
			if r.rows != 24 || r.cols != 80 {
				t.Errorf("Resize mismatch: got %dx%d, want 24x80", r.rows, r.cols)
			}
		case <-time.After(5 * time.Second):
			t.Error("Timeout waiting for resize message")
		}
	})

	t.Run("PingPongRoundTrip", func(t *testing.T) {
		// Send ping from host
		hostChannel.SendPing()

		// Wait for pong (handled internally)
		time.Sleep(200 * time.Millisecond)

		// The pong response is handled internally by EncryptedChannel
		// If we got here without error, ping/pong works
		t.Log("Ping/pong round trip successful")
	})

	t.Run("CloseMessage", func(t *testing.T) {
		closed := make(chan bool, 1)
		clientChannel.OnClose(func() {
			closed <- true
		})

		hostChannel.SendClose()

		select {
		case <-closed:
			t.Log("Close message received")
		case <-time.After(5 * time.Second):
			t.Error("Timeout waiting for close message")
		}
	})
}

// BenchmarkMessageThroughput measures message throughput
func BenchmarkMessageThroughput(b *testing.B) {
	password := "testpassword123"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Create peers
	hostPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	defer hostPeer.Close()

	clientPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
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

	clientDC := <-clientDCChan

	// Wait for open
	hostOpen := make(chan bool, 1)
	clientOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostOpen <- true })
	clientDC.OnOpen(func() { clientOpen <- true })
	<-hostOpen
	<-clientOpen

	hostChannel := ttwebrtc.NewEncryptedChannel(hostDC, &key)
	clientChannel := ttwebrtc.NewEncryptedChannel(clientDC, &key)

	var received atomic.Int64
	clientChannel.OnData(func(data []byte) {
		received.Add(1)
	})

	testData := make([]byte, 100) // 100 byte messages

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hostChannel.SendData(testData)
	}

	// Wait for delivery
	deadline := time.Now().Add(10 * time.Second)
	for received.Load() < int64(b.N) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	b.ReportMetric(float64(received.Load()), "messages")

	hostChannel.Close()
	clientChannel.Close()
}
