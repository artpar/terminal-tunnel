package webrtc

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
)

// TestAbruptDisconnect tests behavior when connection is suddenly terminated
func TestAbruptDisconnect(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}

	// Verify connection works
	received := make(chan bool, 1)
	pair.ClientChannel.OnData(func(data []byte) {
		received <- true
	})

	pair.HostChannel.SendData([]byte("test"))
	select {
	case <-received:
		t.Log("Connection verified")
	case <-time.After(5 * time.Second):
		t.Fatal("Connection failed")
	}

	// Abruptly close host peer (simulating crash/network failure)
	pair.HostPeer.Close()

	// Give time for disconnect to propagate
	time.Sleep(500 * time.Millisecond)

	// Client should detect the disconnection
	state := pair.ClientPeer.ConnectionState()
	t.Logf("Client state after host close: %v", state)

	// Clean up
	pair.ClientPeer.Close()
}

// TestDisconnectDuringLargeTransfer tests disconnect during active data transfer
func TestDisconnectDuringLargeTransfer(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}

	var received atomic.Int32
	pair.ClientChannel.OnData(func(data []byte) {
		received.Add(1)
	})

	// Start large transfer in background
	done := make(chan bool)
	go func() {
		for i := 0; i < 500; i++ {
			pair.HostChannel.SendData([]byte{byte(i)})
			time.Sleep(2 * time.Millisecond)
		}
		done <- true
	}()

	// Wait a bit then disconnect
	time.Sleep(100 * time.Millisecond)

	// Abruptly close during transfer
	pair.HostPeer.Close()

	// Wait for sender to complete (it may error)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Log("Sender took long after disconnect")
	}

	t.Logf("Received %d messages before/during disconnect", received.Load())

	// Should have received at least some messages
	if received.Load() == 0 {
		t.Error("Should have received at least some messages")
	}

	pair.ClientPeer.Close()
}

// TestSuddenPeerTermination tests when peer terminates without close message
func TestSuddenPeerTermination(t *testing.T) {
	password := "testpassword"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	hostPeer, _ := NewPeer(DefaultConfig())
	clientPeer, _ := NewPeer(DefaultConfig())

	var clientDisconnected atomic.Bool
	clientPeer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateDisconnected ||
			state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed {
			clientDisconnected.Store(true)
		}
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

	clientDC := <-clientDCChan

	hostDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostDCOpen <- true })
	clientDC.OnOpen(func() { clientDCOpen <- true })

	<-hostDCOpen
	<-clientDCOpen

	hostChannel := NewEncryptedChannel(hostDC, &key)
	clientChannel := NewEncryptedChannel(clientDC, &key)

	// Verify connection
	received := make(chan bool, 1)
	clientChannel.OnData(func(data []byte) {
		received <- true
	})

	hostChannel.SendData([]byte("test"))
	<-received

	// Suddenly terminate host without graceful close
	hostPeer.Close()

	// Wait for client to detect
	deadline := time.Now().Add(10 * time.Second)
	for !clientDisconnected.Load() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}

	if !clientDisconnected.Load() {
		t.Error("Client should detect disconnection")
	}

	clientChannel.Close()
	clientPeer.Close()
}

// TestDuplicateMessageHandling tests handling of duplicated messages
func TestDuplicateMessageHandling(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	counter := NewMessageCounter()
	pair.ClientChannel.OnData(func(data []byte) {
		if len(data) >= 4 {
			seq := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
			counter.AddWithSeq(seq)
		}
	})

	// Send same sequence multiple times (simulating duplicates)
	for i := 0; i < 10; i++ {
		msg := []byte{0, 0, 0, byte(i)}
		// Send each message multiple times
		for j := 0; j < 3; j++ {
			pair.HostChannel.SendData(msg)
		}
	}

	// Wait for delivery
	time.Sleep(1 * time.Second)

	// Should receive all 30 messages (duplicates included at application level)
	count := counter.Count()
	t.Logf("Received %d messages (expected 30 with duplicates)", count)

	// The encrypted channel doesn't dedupe - that's application responsibility
	if count < 25 {
		t.Errorf("Too few messages received: %d", count)
	}
}

// TestOutOfOrderDelivery tests handling when messages arrive out of order
func TestOutOfOrderDelivery(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	// WebRTC data channels are ordered by default, so we test the ordered guarantee
	var receivedOrder []int
	var mu sync.Mutex

	pair.ClientChannel.OnData(func(data []byte) {
		if len(data) >= 1 {
			mu.Lock()
			receivedOrder = append(receivedOrder, int(data[0]))
			mu.Unlock()
		}
	})

	// Send messages in order
	for i := 0; i < 50; i++ {
		pair.HostChannel.SendData([]byte{byte(i)})
	}

	// Wait for delivery
	deadline := time.Now().Add(5 * time.Second)
	for len(receivedOrder) < 50 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()

	t.Logf("Received %d messages", len(receivedOrder))

	// Verify order is preserved (WebRTC ordered channels)
	outOfOrder := 0
	for i := 1; i < len(receivedOrder); i++ {
		if receivedOrder[i] < receivedOrder[i-1] {
			outOfOrder++
		}
	}

	if outOfOrder > 0 {
		t.Errorf("Messages out of order: %d occurrences", outOfOrder)
	}
}

// TestRapidOpenClose tests rapid open/close cycles
func TestRapidOpenClose(t *testing.T) {
	for i := 0; i < 5; i++ {
		pair, err := NewTestPeerPair("testpassword")
		if err != nil {
			t.Fatalf("Cycle %d: Failed to create pair: %v", i, err)
		}

		// Quick message exchange
		received := make(chan bool, 1)
		pair.ClientChannel.OnData(func(data []byte) {
			received <- true
		})

		pair.HostChannel.SendData([]byte("test"))

		select {
		case <-received:
		case <-time.After(2 * time.Second):
			t.Errorf("Cycle %d: message not received", i)
		}

		// Immediately close
		pair.Close()
	}
}

// TestConcurrentCloseAndSend tests closing while sending
func TestConcurrentCloseAndSend(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}

	var wg sync.WaitGroup

	// Sender goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			pair.HostChannel.SendData([]byte{byte(i)})
			time.Sleep(time.Millisecond)
		}
	}()

	// Closer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		pair.Close()
	}()

	// Should complete without panic or deadlock
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		t.Log("Concurrent close and send completed")
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for concurrent operations")
	}
}

// TestMessageIntegrityUnderStress tests message integrity during stress
func TestMessageIntegrityUnderStress(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	var corruptedCount atomic.Int32
	var receivedCount atomic.Int32

	pair.ClientChannel.OnData(func(data []byte) {
		receivedCount.Add(1)

		// Verify message integrity (simple checksum)
		if len(data) >= 2 {
			// First byte is sequence, second is checksum (255 - seq)
			seq := data[0]
			checksum := data[1]
			expected := byte(255 - seq)
			if checksum != expected {
				corruptedCount.Add(1)
			}
		}
	})

	// Send messages with checksum
	const messageCount = 200
	for i := 0; i < messageCount; i++ {
		seq := byte(i % 256)
		checksum := byte(255 - seq)
		pair.HostChannel.SendData([]byte{seq, checksum})
	}

	// Wait for delivery
	deadline := time.Now().Add(10 * time.Second)
	for receivedCount.Load() < messageCount && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	t.Logf("Received %d messages, %d corrupted", receivedCount.Load(), corruptedCount.Load())

	if corruptedCount.Load() > 0 {
		t.Errorf("Message corruption detected: %d messages", corruptedCount.Load())
	}
}

// TestKeepaliveFailure tests behavior when keepalive fails
func TestKeepaliveFailure(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}

	// Start keepalive on host
	timeoutChan := pair.HostChannel.StartKeepalive()

	// Close client (simulating failure to respond to pings)
	pair.ClientPeer.Close()

	// Keepalive should eventually timeout
	select {
	case <-timeoutChan:
		t.Log("Keepalive correctly detected failure")
	case <-time.After(30 * time.Second):
		t.Error("Keepalive should have timed out")
	}

	pair.HostChannel.StopKeepalive()
	pair.HostPeer.Close()
}

// TestChannelCloseRace tests racing close operations
func TestChannelCloseRace(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}

	var wg sync.WaitGroup

	// Multiple goroutines trying to close
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pair.HostChannel.Close()
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			pair.ClientChannel.Close()
		}()
	}

	// Should complete without panic
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		t.Log("Multiple close operations completed safely")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout on concurrent close")
	}

	pair.HostPeer.Close()
	pair.ClientPeer.Close()
}

// TestSendAfterClose tests sending after channel is closed
func TestSendAfterClose(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}

	// Close channel
	pair.HostChannel.Close()

	// Try to send after close
	err = pair.HostChannel.SendData([]byte("test"))
	if err == nil {
		t.Log("Send after close succeeded (may buffer)")
	} else {
		t.Logf("Send after close failed as expected: %v", err)
	}

	pair.ClientChannel.Close()
	pair.HostPeer.Close()
	pair.ClientPeer.Close()
}

// TestEncryptionIntegrity tests that encryption/decryption is working correctly
func TestEncryptionIntegrity(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	testCases := [][]byte{
		[]byte(""),
		[]byte("a"),
		[]byte("Hello, World!"),
		make([]byte, 1000),   // 1KB
		make([]byte, 10000),  // 10KB
	}

	for i, tc := range testCases {
		// Fill with pattern
		for j := range tc {
			tc[j] = byte(j % 256)
		}

		received := make(chan []byte, 1)
		pair.ClientChannel.OnData(func(data []byte) {
			received <- data
		})

		if err := pair.HostChannel.SendData(tc); err != nil {
			t.Errorf("Case %d: send failed: %v", i, err)
			continue
		}

		select {
		case data := <-received:
			if len(data) != len(tc) {
				t.Errorf("Case %d: length mismatch: got %d, want %d", i, len(data), len(tc))
			} else {
				// Verify content
				for j := range data {
					if data[j] != tc[j] {
						t.Errorf("Case %d: content mismatch at byte %d", i, j)
						break
					}
				}
			}
		case <-time.After(5 * time.Second):
			t.Errorf("Case %d: timeout", i)
		}
	}
}
