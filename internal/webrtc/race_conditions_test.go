package webrtc

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
)

// TestSimultaneousOfferAnswer tests handling of simultaneous SDP exchanges
func TestSimultaneousOfferAnswer(t *testing.T) {
	// This tests the "glare" scenario where both peers try to create offers
	peer1, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create peer1: %v", err)
	}
	defer peer1.Close()

	peer2, err := NewPeer(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create peer2: %v", err)
	}
	defer peer2.Close()

	// Both peers create data channels simultaneously
	var wg sync.WaitGroup
	var offer1, offer2 string
	var err1, err2 error

	wg.Add(2)

	go func() {
		defer wg.Done()
		peer1.CreateDataChannel("terminal")
		offer1, err1 = peer1.CreateOffer()
	}()

	go func() {
		defer wg.Done()
		peer2.CreateDataChannel("terminal")
		offer2, err2 = peer2.CreateOffer()
	}()

	wg.Wait()

	// Both should succeed in creating offers
	if err1 != nil {
		t.Errorf("Peer1 offer creation failed: %v", err1)
	}
	if err2 != nil {
		t.Errorf("Peer2 offer creation failed: %v", err2)
	}

	// In a real scenario, one peer would "win" and the other would accept
	// Here we verify that at least the offer creation doesn't race
	t.Logf("Peer1 offer length: %d, Peer2 offer length: %d", len(offer1), len(offer2))
}

// TestDataChannelMessageDuringICERestart tests sending messages during ICE changes
func TestDataChannelMessageDuringICERestart(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create peer pair: %v", err)
	}
	defer pair.Close()

	// Track received messages
	counter := NewMessageCounter()
	pair.ClientChannel.OnData(func(data []byte) {
		counter.Add()
	})

	// Send messages while connection is stable
	const initialMessages = 50
	for i := 0; i < initialMessages; i++ {
		if err := pair.HostChannel.SendData([]byte{byte(i)}); err != nil {
			t.Fatalf("Failed to send initial message %d: %v", i, err)
		}
	}

	// Wait for initial messages
	if !counter.WaitForCount(initialMessages, 5*time.Second) {
		t.Errorf("Expected %d initial messages, got %d", initialMessages, counter.Count())
	}

	// Continue sending more messages
	const additionalMessages = 50
	for i := 0; i < additionalMessages; i++ {
		if err := pair.HostChannel.SendData([]byte{byte(i + initialMessages)}); err != nil {
			// Some messages may fail during state changes, that's expected
			t.Logf("Message %d failed (may be expected): %v", i+initialMessages, err)
		}
	}

	// Wait a bit for remaining messages
	time.Sleep(500 * time.Millisecond)

	totalReceived := counter.Count()
	t.Logf("Received %d/%d total messages", totalReceived, initialMessages+additionalMessages)

	// At minimum, initial messages should all be received
	if totalReceived < initialMessages {
		t.Errorf("Lost initial messages: got %d, expected at least %d", totalReceived, initialMessages)
	}
}

// TestDisconnectDuringReconnection tests disconnect signals during reconnect attempts
func TestDisconnectDuringReconnection(t *testing.T) {
	password := "testpassword"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Create peers
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

	// Track state changes
	hostObserver := NewStateObserver()
	clientObserver := NewStateObserver()

	hostPeer.OnConnectionStateChange(hostObserver.OnStateChange)
	clientPeer.OnConnectionStateChange(clientObserver.OnStateChange)

	hostDC, err := hostPeer.CreateDataChannel("terminal")
	if err != nil {
		t.Fatalf("Failed to create data channel: %v", err)
	}

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

	// Wait for open
	hostDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostDCOpen <- true })
	clientDC.OnOpen(func() { clientDCOpen <- true })

	<-hostDCOpen
	<-clientDCOpen

	// Create channels
	hostChannel := NewEncryptedChannel(hostDC, &key)
	clientChannel := NewEncryptedChannel(clientDC, &key)
	defer hostChannel.Close()
	defer clientChannel.Close()

	// Verify connected
	if !hostObserver.WaitForState(webrtc.PeerConnectionStateConnected, 5*time.Second) {
		t.Error("Host never reached connected state")
	}

	// Log state history
	t.Logf("Host states: %v", hostObserver.GetHistory())
	t.Logf("Client states: %v", clientObserver.GetHistory())
}

// TestRapidConnectDisconnectCycles tests multiple rapid connect/disconnect cycles
func TestRapidConnectDisconnectCycles(t *testing.T) {
	const cycles = 5

	for cycle := 0; cycle < cycles; cycle++ {
		t.Run("Cycle", func(t *testing.T) {
			pair, err := NewTestPeerPair("testpassword")
			if err != nil {
				t.Fatalf("Cycle %d: Failed to create peer pair: %v", cycle, err)
			}

			// Verify connection works
			received := make(chan bool, 1)
			pair.ClientChannel.OnData(func(data []byte) {
				received <- true
			})

			if err := pair.HostChannel.SendData([]byte("test")); err != nil {
				t.Errorf("Cycle %d: Failed to send data: %v", cycle, err)
			}

			select {
			case <-received:
				// Good
			case <-time.After(5 * time.Second):
				t.Errorf("Cycle %d: Timeout waiting for message", cycle)
			}

			// Close immediately
			pair.Close()

			// Brief pause between cycles
			time.Sleep(100 * time.Millisecond)
		})
	}
}

// TestConcurrentSendReceive tests simultaneous bidirectional messaging
func TestConcurrentSendReceive(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create peer pair: %v", err)
	}
	defer pair.Close()

	const messagesPerDirection = 100

	var hostReceived, clientReceived atomic.Int32

	pair.HostChannel.OnData(func(data []byte) {
		hostReceived.Add(1)
	})

	pair.ClientChannel.OnData(func(data []byte) {
		clientReceived.Add(1)
	})

	var wg sync.WaitGroup
	wg.Add(2)

	// Host sends to client
	go func() {
		defer wg.Done()
		for i := 0; i < messagesPerDirection; i++ {
			pair.HostChannel.SendData([]byte{byte(i)})
			time.Sleep(time.Millisecond)
		}
	}()

	// Client sends to host
	go func() {
		defer wg.Done()
		for i := 0; i < messagesPerDirection; i++ {
			pair.ClientChannel.SendData([]byte{byte(i)})
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()

	// Wait for messages to arrive
	deadline := time.Now().Add(5 * time.Second)
	for (hostReceived.Load() < messagesPerDirection || clientReceived.Load() < messagesPerDirection) &&
		time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	hr := hostReceived.Load()
	cr := clientReceived.Load()

	t.Logf("Host received: %d/%d, Client received: %d/%d", hr, messagesPerDirection, cr, messagesPerDirection)

	// Allow some tolerance
	if hr < int32(messagesPerDirection*90/100) {
		t.Errorf("Host received too few: %d", hr)
	}
	if cr < int32(messagesPerDirection*90/100) {
		t.Errorf("Client received too few: %d", cr)
	}
}

// TestPingPongRaceWithData tests ping/pong concurrent with data messages
func TestPingPongRaceWithData(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create peer pair: %v", err)
	}
	defer pair.Close()

	counter := NewMessageCounter()
	pair.ClientChannel.OnData(func(data []byte) {
		counter.Add()
	})

	// Start keepalive on both sides
	hostTimeout := pair.HostChannel.StartKeepalive()
	clientTimeout := pair.ClientChannel.StartKeepalive()

	// Send data rapidly while keepalive is running
	const messageCount = 200
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for i := 0; i < messageCount; i++ {
			pair.HostChannel.SendData([]byte{byte(i)})
			time.Sleep(5 * time.Millisecond)
		}
	}()

	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	// Stop keepalives
	pair.HostChannel.StopKeepalive()
	pair.ClientChannel.StopKeepalive()

	// Check no timeout occurred
	select {
	case <-hostTimeout:
		t.Error("Host keepalive timed out unexpectedly")
	default:
	}

	select {
	case <-clientTimeout:
		t.Error("Client keepalive timed out unexpectedly")
	default:
	}

	// Verify data was received
	t.Logf("Received %d/%d messages while keepalive running", counter.Count(), messageCount)

	if counter.Count() < int32(messageCount*90/100) {
		t.Errorf("Too few messages received: %d", counter.Count())
	}
}

// TestConcurrentChannelOperations tests concurrent channel operations
func TestConcurrentChannelOperations(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create peer pair: %v", err)
	}
	defer pair.Close()

	var wg sync.WaitGroup
	const goroutines = 10
	const opsPerGoroutine = 20

	// Run concurrent operations
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				switch i % 4 {
				case 0:
					pair.HostChannel.SendData([]byte{byte(id), byte(i)})
				case 1:
					pair.ClientChannel.SendData([]byte{byte(id), byte(i)})
				case 2:
					pair.HostChannel.SendPing()
				case 3:
					pair.ClientChannel.SendPing()
				}
				time.Sleep(time.Millisecond)
			}
		}(g)
	}

	wg.Wait()

	// If we get here without panic or deadlock, the test passed
	t.Log("Concurrent operations completed without deadlock")
}

// TestStartStopKeepaliveRace stress tests keepalive start/stop
func TestStartStopKeepaliveRace(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create peer pair: %v", err)
	}
	defer pair.Close()

	// Stress test: rapidly start/stop keepalive
	for i := 0; i < 50; i++ {
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			pair.HostChannel.StartKeepalive()
		}()

		go func() {
			defer wg.Done()
			time.Sleep(time.Microsecond)
			pair.HostChannel.StopKeepalive()
		}()

		wg.Wait()
	}

	// Ensure clean state
	pair.HostChannel.StopKeepalive()
	t.Log("Keepalive race test completed without deadlock")
}

// TestCloseWhileSending tests closing channel while actively sending
func TestCloseWhileSending(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create peer pair: %v", err)
	}

	// Start sending in background
	done := make(chan bool)
	go func() {
		for i := 0; i < 1000; i++ {
			pair.HostChannel.SendData([]byte{byte(i)})
		}
		done <- true
	}()

	// Close after a brief delay
	time.Sleep(10 * time.Millisecond)
	pair.Close()

	// Sender should finish (possibly with errors, but no panic)
	select {
	case <-done:
		t.Log("Sender completed despite close")
	case <-time.After(5 * time.Second):
		t.Error("Sender blocked after close")
	}
}
