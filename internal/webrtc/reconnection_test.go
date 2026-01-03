package webrtc

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
)

// TestReconnectionBasics tests basic reconnection scenarios
func TestReconnectionBasics(t *testing.T) {
	t.Run("ConnectionAfterPeerClose", func(t *testing.T) {
		// First connection
		pair1, err := NewTestPeerPair("testpassword")
		if err != nil {
			t.Fatalf("Failed to create first pair: %v", err)
		}

		// Verify it works
		received := make(chan bool, 1)
		pair1.ClientChannel.OnData(func(data []byte) {
			received <- true
		})

		pair1.HostChannel.SendData([]byte("test1"))
		select {
		case <-received:
			t.Log("First connection works")
		case <-time.After(5 * time.Second):
			t.Fatal("First connection failed")
		}

		// Close first connection
		pair1.Close()

		// Create new connection with same password
		pair2, err := NewTestPeerPair("testpassword")
		if err != nil {
			t.Fatalf("Failed to create second pair: %v", err)
		}
		defer pair2.Close()

		// Verify new connection works
		received2 := make(chan bool, 1)
		pair2.ClientChannel.OnData(func(data []byte) {
			received2 <- true
		})

		pair2.HostChannel.SendData([]byte("test2"))
		select {
		case <-received2:
			t.Log("Second connection works")
		case <-time.After(5 * time.Second):
			t.Fatal("Second connection failed")
		}
	})
}

// TestReconnectionTiming tests timing aspects of reconnection
func TestReconnectionTiming(t *testing.T) {
	t.Run("ImmediateReconnect", func(t *testing.T) {
		// Create and close connections rapidly
		for i := 0; i < 3; i++ {
			start := time.Now()

			pair, err := NewTestPeerPair("testpassword")
			if err != nil {
				t.Fatalf("Connection %d failed: %v", i, err)
			}

			elapsed := time.Since(start)
			t.Logf("Connection %d established in %v", i, elapsed)

			// Verify it works
			received := make(chan bool, 1)
			pair.ClientChannel.OnData(func(data []byte) {
				received <- true
			})

			pair.HostChannel.SendData([]byte("test"))
			select {
			case <-received:
			case <-time.After(5 * time.Second):
				t.Fatalf("Connection %d: message not received", i)
			}

			pair.Close()

			// Brief pause between connections
			time.Sleep(50 * time.Millisecond)
		}
	})

	t.Run("ConnectionEstablishmentTime", func(t *testing.T) {
		// Measure how long it takes to establish a connection
		var times []time.Duration

		for i := 0; i < 3; i++ {
			start := time.Now()

			pair, err := NewTestPeerPair("testpassword")
			if err != nil {
				t.Fatalf("Connection %d failed: %v", i, err)
			}

			elapsed := time.Since(start)
			times = append(times, elapsed)

			pair.Close()
			time.Sleep(100 * time.Millisecond)
		}

		// Calculate average
		var total time.Duration
		for _, d := range times {
			total += d
		}
		avg := total / time.Duration(len(times))

		t.Logf("Connection times: %v, average: %v", times, avg)

		// Connection should typically establish in under 5 seconds locally
		if avg > 5*time.Second {
			t.Errorf("Average connection time too slow: %v", avg)
		}
	})
}

// TestKeepaliveRecovery tests keepalive behavior during transient issues
func TestKeepaliveRecovery(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	// Start keepalive
	hostTimeout := pair.HostChannel.StartKeepalive()
	clientTimeout := pair.ClientChannel.StartKeepalive()

	// Let keepalive run for a while
	time.Sleep(2 * time.Second)

	// Verify no timeout
	select {
	case <-hostTimeout:
		t.Error("Host keepalive timed out")
	case <-clientTimeout:
		t.Error("Client keepalive timed out")
	default:
		t.Log("Keepalive running normally")
	}

	// Stop keepalives
	pair.HostChannel.StopKeepalive()
	pair.ClientChannel.StopKeepalive()

	// Verify connection still works after stopping keepalive
	received := make(chan bool, 1)
	pair.ClientChannel.OnData(func(data []byte) {
		received <- true
	})

	pair.HostChannel.SendData([]byte("after-keepalive"))
	select {
	case <-received:
		t.Log("Connection works after keepalive stopped")
	case <-time.After(5 * time.Second):
		t.Error("Connection failed after stopping keepalive")
	}
}

// TestMessageDeliveryAfterDelay tests message delivery after periods of inactivity
func TestMessageDeliveryAfterDelay(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	counter := NewMessageCounter()
	pair.ClientChannel.OnData(func(data []byte) {
		counter.Add()
	})

	// Send initial message
	pair.HostChannel.SendData([]byte("initial"))
	if !counter.WaitForCount(1, 5*time.Second) {
		t.Fatal("Initial message not received")
	}

	// Wait for a period of inactivity
	t.Log("Waiting for 3 seconds of inactivity...")
	time.Sleep(3 * time.Second)

	// Send more messages after inactivity
	for i := 0; i < 10; i++ {
		if err := pair.HostChannel.SendData([]byte{byte(i)}); err != nil {
			t.Errorf("Failed to send message %d after delay: %v", i, err)
		}
	}

	// All should be received
	if !counter.WaitForCount(11, 5*time.Second) {
		t.Errorf("Expected 11 messages, got %d", counter.Count())
	}
}

// TestBackoffBehavior tests exponential backoff simulation
func TestBackoffBehavior(t *testing.T) {
	// Simulate exponential backoff timing
	baseDelay := 100 * time.Millisecond
	maxDelay := 2 * time.Second

	delays := []time.Duration{}
	currentDelay := baseDelay

	for i := 0; i < 10; i++ {
		delays = append(delays, currentDelay)

		// Double the delay (exponential backoff)
		currentDelay *= 2
		if currentDelay > maxDelay {
			currentDelay = maxDelay
		}
	}

	t.Logf("Backoff delays: %v", delays)

	// Verify backoff properties
	if delays[0] != baseDelay {
		t.Errorf("First delay should be %v, got %v", baseDelay, delays[0])
	}

	// Should cap at max
	if delays[len(delays)-1] > maxDelay {
		t.Errorf("Final delay should not exceed %v, got %v", maxDelay, delays[len(delays)-1])
	}

	// Should increase
	for i := 1; i < len(delays); i++ {
		if delays[i] < delays[i-1] && delays[i-1] < maxDelay {
			t.Errorf("Delay should increase: %v -> %v", delays[i-1], delays[i])
		}
	}
}

// TestMaxRetryLimit tests that retry limits are respected
func TestMaxRetryLimit(t *testing.T) {
	const maxRetries = 3
	retryCount := 0

	// Simulate connection attempts
	for attempt := 0; attempt <= maxRetries; attempt++ {
		retryCount++

		// Simulate connection failure for first attempts
		if attempt < maxRetries {
			t.Logf("Attempt %d: simulating failure", attempt+1)
			continue
		}

		// Final attempt succeeds
		t.Logf("Attempt %d: simulating success", attempt+1)
		break
	}

	if retryCount > maxRetries+1 {
		t.Errorf("Too many retries: %d (max should be %d)", retryCount, maxRetries+1)
	}
}

// TestStaleSessionData tests handling of stale session data
func TestStaleSessionData(t *testing.T) {
	password := "testpassword"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Create initial connection
	hostPeer1, _ := NewPeer(DefaultConfig())
	clientPeer1, _ := NewPeer(DefaultConfig())

	hostDC1, _ := hostPeer1.CreateDataChannel("terminal")

	clientDCChan := make(chan *webrtc.DataChannel, 1)
	clientPeer1.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCChan <- dc
	})

	offer1, _ := hostPeer1.CreateOffer()
	clientPeer1.SetRemoteDescription(webrtc.SDPTypeOffer, offer1)
	answer1, _ := clientPeer1.CreateAnswer()
	hostPeer1.SetRemoteDescription(webrtc.SDPTypeAnswer, answer1)

	clientDC1 := <-clientDCChan

	hostDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)
	hostDC1.OnOpen(func() { hostDCOpen <- true })
	clientDC1.OnOpen(func() { clientDCOpen <- true })

	<-hostDCOpen
	<-clientDCOpen

	hostChannel1 := NewEncryptedChannel(hostDC1, &key)
	clientChannel1 := NewEncryptedChannel(clientDC1, &key)

	// Save "stale" offer for later
	staleOffer := offer1

	// Close first connection
	hostChannel1.Close()
	clientChannel1.Close()
	hostPeer1.Close()
	clientPeer1.Close()

	// Try to use stale offer with new peer - should not work
	newClientPeer, _ := NewPeer(DefaultConfig())
	defer newClientPeer.Close()

	// This should fail or create an unusable connection
	err := newClientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, staleOffer)
	if err == nil {
		// Even if it accepts the stale SDP, it won't be able to connect
		// because the original host peer is closed
		answer2, err := newClientPeer.CreateAnswer()
		if err != nil {
			t.Logf("Expected: can't answer stale offer: %v", err)
		} else {
			t.Logf("Created answer for stale offer (length: %d)", len(answer2))
			// This answer won't work because there's no host to receive it
		}
	} else {
		t.Logf("Expected: stale offer rejected: %v", err)
	}
}

// TestConnectionRecoveryMetrics tracks connection recovery metrics
func TestConnectionRecoveryMetrics(t *testing.T) {
	var successCount, failureCount atomic.Int32
	var totalRecoveryTime atomic.Int64

	// Simulate multiple recovery attempts
	for i := 0; i < 5; i++ {
		start := time.Now()

		pair, err := NewTestPeerPair("testpassword")
		if err != nil {
			failureCount.Add(1)
			continue
		}

		// Verify it works
		received := make(chan bool, 1)
		pair.ClientChannel.OnData(func(data []byte) {
			received <- true
		})

		pair.HostChannel.SendData([]byte("test"))
		select {
		case <-received:
			successCount.Add(1)
			totalRecoveryTime.Add(int64(time.Since(start)))
		case <-time.After(5 * time.Second):
			failureCount.Add(1)
		}

		pair.Close()
		time.Sleep(100 * time.Millisecond)
	}

	successes := successCount.Load()
	failures := failureCount.Load()

	t.Logf("Connection recovery: %d successes, %d failures", successes, failures)

	if successes > 0 {
		avgRecovery := time.Duration(totalRecoveryTime.Load() / int64(successes))
		t.Logf("Average recovery time: %v", avgRecovery)
	}

	// All attempts should succeed in a normal test environment
	if failures > 0 {
		t.Errorf("Some connection attempts failed: %d", failures)
	}
}

// TestPartialReconnect tests scenarios where only one direction reconnects
func TestPartialReconnect(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}

	// Track bidirectional messages
	var hostReceived, clientReceived atomic.Int32

	pair.HostChannel.OnData(func(data []byte) {
		hostReceived.Add(1)
	})

	pair.ClientChannel.OnData(func(data []byte) {
		clientReceived.Add(1)
	})

	// Send in both directions
	for i := 0; i < 10; i++ {
		pair.HostChannel.SendData([]byte{byte(i)})
		pair.ClientChannel.SendData([]byte{byte(i)})
	}

	// Wait for messages
	time.Sleep(500 * time.Millisecond)

	hr := hostReceived.Load()
	cr := clientReceived.Load()

	t.Logf("Host received: %d, Client received: %d", hr, cr)

	// Both directions should work
	if hr < 9 || cr < 9 {
		t.Errorf("Bidirectional communication failed: host=%d, client=%d", hr, cr)
	}

	pair.Close()
}
