package webrtc

import (
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// TestKeepaliveRaceCondition tests that StartKeepalive/StopKeepalive
// don't race when called concurrently
func TestKeepaliveRaceCondition(t *testing.T) {
	// Create a peer connection for testing
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	// Create a data channel
	dc, err := pc.CreateDataChannel("test", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create encryption key
	key := [32]byte{}

	// Stress test: rapidly start/stop keepalive to trigger race condition
	for i := 0; i < 100; i++ {
		ec := NewEncryptedChannel(dc, &key)

		var wg sync.WaitGroup
		wg.Add(2)

		// Start keepalive in one goroutine
		go func() {
			defer wg.Done()
			ec.StartKeepalive()
		}()

		// Stop keepalive in another goroutine (race condition trigger)
		go func() {
			defer wg.Done()
			time.Sleep(time.Microsecond) // Small delay to increase race window
			ec.StopKeepalive()
		}()

		wg.Wait()
		ec.StopKeepalive() // Cleanup
	}
}
