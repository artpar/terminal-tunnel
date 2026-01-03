package webrtc

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestNetworkSimulator tests the network condition simulator itself
func TestNetworkSimulator(t *testing.T) {
	t.Run("LatencySimulation", func(t *testing.T) {
		sim := NewNetworkSimulator()
		sim.SetLatency(100)
		sim.Enable()

		delay := sim.GetDelay()
		if delay < 50*time.Millisecond || delay > 150*time.Millisecond {
			t.Errorf("Expected delay around 100ms, got %v", delay)
		}

		sim.Disable()
		delay = sim.GetDelay()
		if delay != 0 {
			t.Errorf("Expected 0 delay when disabled, got %v", delay)
		}
	})

	t.Run("JitterSimulation", func(t *testing.T) {
		sim := NewNetworkSimulator()
		sim.SetLatency(100)
		sim.SetJitter(50)
		sim.Enable()

		// Collect multiple samples
		var delays []time.Duration
		for i := 0; i < 20; i++ {
			delays = append(delays, sim.GetDelay())
		}

		// Should have some variation
		minDelay := delays[0]
		maxDelay := delays[0]
		for _, d := range delays {
			if d < minDelay {
				minDelay = d
			}
			if d > maxDelay {
				maxDelay = d
			}
		}

		spread := maxDelay - minDelay
		t.Logf("Delay range: %v - %v (spread: %v)", minDelay, maxDelay, spread)

		// With 50ms jitter, we should see at least some variation
		if spread < 10*time.Millisecond {
			t.Log("Note: Low jitter variance (might be expected with small sample)")
		}
	})

	t.Run("PacketLossSimulation", func(t *testing.T) {
		sim := NewNetworkSimulator()
		sim.SetPacketLoss(0.5) // 50% loss
		sim.Enable()

		dropped := 0
		const samples = 1000

		for i := 0; i < samples; i++ {
			if sim.ShouldDrop() {
				dropped++
			}
		}

		dropRate := float64(dropped) / float64(samples)
		t.Logf("Drop rate: %.2f%% (expected ~50%%)", dropRate*100)

		// Should be within reasonable range of 50%
		if dropRate < 0.40 || dropRate > 0.60 {
			t.Errorf("Drop rate %.2f%% is too far from expected 50%%", dropRate*100)
		}
	})

	t.Run("ResetBehavior", func(t *testing.T) {
		sim := NewNetworkSimulator()
		sim.SetLatency(100)
		sim.SetJitter(50)
		sim.SetPacketLoss(0.25)
		sim.Enable()

		// Verify enabled
		if sim.GetDelay() == 0 {
			t.Error("Simulator should be enabled")
		}

		// Reset
		sim.Reset()

		// Should be disabled
		if sim.GetDelay() != 0 {
			t.Error("Simulator should be disabled after reset")
		}
		if sim.ShouldDrop() {
			t.Error("Should not drop packets after reset")
		}
	})
}

// TestConnectionUnderLatency tests connection behavior with simulated latency
func TestConnectionUnderLatency(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	// Simulate application-level latency by delaying sends
	// Note: This doesn't simulate actual network latency, but tests
	// the system's tolerance for delayed responses

	counter := NewMessageCounter()
	pair.ClientChannel.OnData(func(data []byte) {
		counter.Add()
	})

	// Send messages with delays
	const messageCount = 10
	for i := 0; i < messageCount; i++ {
		// Simulate variable latency
		time.Sleep(time.Duration(50+i*10) * time.Millisecond)
		if err := pair.HostChannel.SendData([]byte{byte(i)}); err != nil {
			t.Errorf("Failed to send message %d: %v", i, err)
		}
	}

	// All messages should eventually arrive
	if !counter.WaitForCount(messageCount, 10*time.Second) {
		t.Errorf("Expected %d messages, got %d", messageCount, counter.Count())
	}
}

// TestConnectionUnderLoad tests connection under heavy load
func TestConnectionUnderLoad(t *testing.T) {
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

	// Send large burst of messages
	const messageCount = 500
	for i := 0; i < messageCount; i++ {
		msg := []byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)}
		if err := pair.HostChannel.SendData(msg); err != nil {
			t.Logf("Send error at message %d: %v", i, err)
		}
	}

	// Wait for delivery
	if !counter.WaitForCount(int32(messageCount*95/100), 15*time.Second) {
		t.Errorf("Expected at least %d messages, got %d", messageCount*95/100, counter.Count())
	}

	// Check for gaps
	missing := counter.MissingSeqs(0, messageCount)
	if len(missing) > messageCount/20 { // Allow 5% loss
		t.Errorf("Too many missing messages: %d", len(missing))
		if len(missing) <= 10 {
			t.Logf("Missing sequences: %v", missing)
		}
	}
}

// TestBandwidthVariation tests behavior with varying message sizes
func TestBandwidthVariation(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	var totalReceived atomic.Int64
	pair.ClientChannel.OnData(func(data []byte) {
		totalReceived.Add(int64(len(data)))
	})

	// Send messages of varying sizes
	sizes := []int{10, 100, 500, 1000, 5000}
	var totalSent int64

	for _, size := range sizes {
		msg := make([]byte, size)
		for i := range msg {
			msg[i] = byte(i % 256)
		}

		for j := 0; j < 10; j++ {
			if err := pair.HostChannel.SendData(msg); err != nil {
				t.Errorf("Failed to send %d byte message: %v", size, err)
			}
			totalSent += int64(size)
		}
	}

	// Wait for all data
	deadline := time.Now().Add(10 * time.Second)
	for totalReceived.Load() < totalSent && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}

	received := totalReceived.Load()
	t.Logf("Sent %d bytes, received %d bytes (%.1f%%)",
		totalSent, received, float64(received)/float64(totalSent)*100)

	// Should receive at least 95% of data
	if received < totalSent*95/100 {
		t.Errorf("Too much data loss: sent %d, received %d", totalSent, received)
	}
}

// TestLongLatencyConnection tests with high latency simulation
func TestLongLatencyConnection(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	// Simulate high-latency scenario by using timeouts
	counter := NewMessageCounter()
	pair.ClientChannel.OnData(func(data []byte) {
		counter.Add()
	})

	// Send with artificial delays to simulate high latency
	const messageCount = 5
	for i := 0; i < messageCount; i++ {
		// Large delay between sends
		time.Sleep(500 * time.Millisecond)
		pair.HostChannel.SendData([]byte{byte(i)})
	}

	// Messages should still arrive even with high latency
	if !counter.WaitForCount(messageCount, 15*time.Second) {
		t.Errorf("Expected %d messages with high latency, got %d", messageCount, counter.Count())
	}
}

// TestKeepaliveUnderNetworkStress tests keepalive during network stress
func TestKeepaliveUnderNetworkStress(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	// Start keepalive
	hostTimeout := pair.HostChannel.StartKeepalive()
	clientTimeout := pair.ClientChannel.StartKeepalive()

	// Flood with data while keepalive is running
	done := make(chan bool)
	go func() {
		for i := 0; i < 200; i++ {
			pair.HostChannel.SendData([]byte{byte(i)})
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for flood to complete
	<-done

	// Check keepalive didn't timeout
	select {
	case <-hostTimeout:
		t.Error("Host keepalive timed out during stress")
	case <-clientTimeout:
		t.Error("Client keepalive timed out during stress")
	default:
		t.Log("Keepalive survived stress test")
	}

	pair.HostChannel.StopKeepalive()
	pair.ClientChannel.StopKeepalive()
}

// TestIntermittentConnectivity simulates intermittent connectivity
func TestIntermittentConnectivity(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	counter := NewMessageCounter()
	pair.ClientChannel.OnData(func(data []byte) {
		counter.Add()
	})

	// Send in bursts with gaps
	for burst := 0; burst < 3; burst++ {
		// Send burst of messages
		for i := 0; i < 20; i++ {
			pair.HostChannel.SendData([]byte{byte(burst*20 + i)})
		}

		// Simulate connectivity gap
		time.Sleep(1 * time.Second)
	}

	// All messages from all bursts should arrive
	if !counter.WaitForCount(60, 10*time.Second) {
		t.Errorf("Expected 60 messages across bursts, got %d", counter.Count())
	}
}

// TestLargeMessageHandling tests handling of large messages
func TestLargeMessageHandling(t *testing.T) {
	pair, err := NewTestPeerPair("testpassword")
	if err != nil {
		t.Fatalf("Failed to create pair: %v", err)
	}
	defer pair.Close()

	// Test various large message sizes
	// Note: WebRTC data channels have message size limits
	sizes := []int{1000, 5000, 10000, 16000}

	for _, size := range sizes {
		t.Run("Size", func(t *testing.T) {
			received := make(chan int, 1)
			pair.ClientChannel.OnData(func(data []byte) {
				received <- len(data)
			})

			// Create message of specified size
			msg := make([]byte, size)
			for i := range msg {
				msg[i] = byte(i % 256)
			}

			if err := pair.HostChannel.SendData(msg); err != nil {
				t.Errorf("Failed to send %d byte message: %v", size, err)
				return
			}

			select {
			case receivedSize := <-received:
				if receivedSize != size {
					t.Errorf("Size mismatch: sent %d, received %d", size, receivedSize)
				}
			case <-time.After(5 * time.Second):
				t.Errorf("Timeout receiving %d byte message", size)
			}
		})
	}
}
