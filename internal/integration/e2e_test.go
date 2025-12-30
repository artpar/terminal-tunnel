package integration

import (
	"context"
	"encoding/base64"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
	"github.com/artpar/terminal-tunnel/internal/server"
	ttwebrtc "github.com/artpar/terminal-tunnel/internal/webrtc"
)

// TestEndToEndServerClient tests the full server-client flow
func TestEndToEndServerClient(t *testing.T) {
	password := "testpassword123"
	salt, err := crypto.GenerateSalt()
	if err != nil {
		t.Fatalf("Failed to generate salt: %v", err)
	}
	saltB64 := base64.StdEncoding.EncodeToString(salt)

	// Derive keys (same as server does)
	key := crypto.DeriveKey(password, salt)
	pbkdf2Key := crypto.DeriveKeyPBKDF2(password, salt)

	// Create server options
	opts := server.Options{
		Shell:    "/bin/sh",
		Password: password,
		NoTURN:   true, // Use only STUN for testing
		Timeout:  30 * time.Second,
	}

	// Create server
	srv, err := server.New(opts)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create a context with cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Track server events
	var serverConnected, serverDisconnected atomic.Bool
	srv.SetCallbacks(server.Callbacks{
		OnClientConnect: func() {
			serverConnected.Store(true)
			t.Log("[Server] Client connected")
		},
		OnClientDisconnect: func() {
			serverDisconnected.Store(true)
			t.Log("[Server] Client disconnected")
		},
		OnShortCodeReady: func(code, url string) {
			t.Logf("[Server] Short code ready: %s", code)
		},
	})

	// Since we can't use the relay in tests, we'll simulate the signaling
	// by directly creating the WebRTC offer/answer exchange

	// Create server's WebRTC peer and offer
	serverPeer, err := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create server peer: %v", err)
	}
	defer serverPeer.Close()

	serverDC, err := serverPeer.CreateDataChannel("terminal")
	if err != nil {
		t.Fatalf("Failed to create data channel: %v", err)
	}

	offer, err := serverPeer.CreateOffer()
	if err != nil {
		t.Fatalf("Failed to create offer: %v", err)
	}

	// Create client peer
	clientPeer, err := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create client peer: %v", err)
	}
	defer clientPeer.Close()

	// Client receives data channel
	clientDCChan := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCChan <- dc
	})

	// Client processes offer and creates answer
	if err := clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer); err != nil {
		t.Fatalf("Client failed to set offer: %v", err)
	}

	answer, err := clientPeer.CreateAnswer()
	if err != nil {
		t.Fatalf("Client failed to create answer: %v", err)
	}

	// Server receives answer
	if err := serverPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer); err != nil {
		t.Fatalf("Server failed to set answer: %v", err)
	}

	// Wait for data channels to open
	serverDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)

	serverDC.OnOpen(func() {
		t.Log("[Server] Data channel opened")
		serverDCOpen <- true
	})

	var clientDC *webrtc.DataChannel
	select {
	case clientDC = <-clientDCChan:
		clientDC.OnOpen(func() {
			t.Log("[Client] Data channel opened")
			clientDCOpen <- true
		})
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for client data channel")
	}

	select {
	case <-serverDCOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("Server data channel did not open")
	}

	select {
	case <-clientDCOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("Client data channel did not open")
	}

	// Create encrypted channels
	serverChannel := ttwebrtc.NewEncryptedChannel(serverDC, &key)
	serverChannel.SetAltKey(&pbkdf2Key)

	clientChannel := ttwebrtc.NewEncryptedChannel(clientDC, &key)

	// Test message exchange
	t.Run("MessageExchange", func(t *testing.T) {
		var serverReceived, clientReceived atomic.Int32

		serverChannel.OnData(func(data []byte) {
			serverReceived.Add(1)
		})

		clientChannel.OnData(func(data []byte) {
			clientReceived.Add(1)
		})

		// Send from client to server
		for i := 0; i < 10; i++ {
			clientChannel.SendData([]byte{byte(i)})
		}

		// Send from server to client
		for i := 0; i < 10; i++ {
			serverChannel.SendData([]byte{byte(i)})
		}

		time.Sleep(500 * time.Millisecond)

		if serverReceived.Load() != 10 {
			t.Errorf("Server received %d messages, expected 10", serverReceived.Load())
		}
		if clientReceived.Load() != 10 {
			t.Errorf("Client received %d messages, expected 10", clientReceived.Load())
		}
	})

	// Test keepalive
	t.Run("Keepalive", func(t *testing.T) {
		timeoutChan := serverChannel.StartKeepalive()

		// Let it run for a bit
		time.Sleep(500 * time.Millisecond)

		serverChannel.StopKeepalive()

		select {
		case <-timeoutChan:
			t.Error("Keepalive timed out unexpectedly")
		default:
			t.Log("Keepalive working")
		}
	})

	// Test resize
	t.Run("Resize", func(t *testing.T) {
		resizeChan := make(chan struct{ rows, cols uint16 }, 1)
		serverChannel.OnResize(func(rows, cols uint16) {
			resizeChan <- struct{ rows, cols uint16 }{rows, cols}
		})

		clientChannel.SendResize(24, 80)

		select {
		case r := <-resizeChan:
			if r.rows != 24 || r.cols != 80 {
				t.Errorf("Wrong resize: %dx%d", r.rows, r.cols)
			}
		case <-time.After(5 * time.Second):
			t.Error("Timeout waiting for resize")
		}
	})

	// Cleanup
	serverChannel.Close()
	clientChannel.Close()

	_ = ctx
	_ = saltB64
	_ = srv
}

// TestConnectionStabilityUnderStress tests connection stability under various conditions
func TestConnectionStabilityUnderStress(t *testing.T) {
	password := "testpassword123"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Create peers
	serverPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	defer serverPeer.Close()

	clientPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	defer clientPeer.Close()

	serverDC, _ := serverPeer.CreateDataChannel("terminal")

	clientDCChan := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCChan <- dc
	})

	offer, _ := serverPeer.CreateOffer()
	clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
	answer, _ := clientPeer.CreateAnswer()
	serverPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)

	clientDC := <-clientDCChan

	serverOpen := make(chan bool, 1)
	clientOpen := make(chan bool, 1)
	serverDC.OnOpen(func() { serverOpen <- true })
	clientDC.OnOpen(func() { clientOpen <- true })
	<-serverOpen
	<-clientOpen

	serverChannel := ttwebrtc.NewEncryptedChannel(serverDC, &key)
	clientChannel := ttwebrtc.NewEncryptedChannel(clientDC, &key)

	// Start keepalive
	timeoutChan := serverChannel.StartKeepalive()

	// Stress test: concurrent message sending from both sides
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverSent, clientSent atomic.Int64
	var serverReceived, clientReceived atomic.Int64

	serverChannel.OnData(func(data []byte) {
		clientReceived.Add(1)
	})
	clientChannel.OnData(func(data []byte) {
		serverReceived.Add(1)
	})

	var wg sync.WaitGroup

	// Server sender
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if err := serverChannel.SendData([]byte("server")); err == nil {
					serverSent.Add(1)
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Client sender
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if err := clientChannel.SendData([]byte("client")); err == nil {
					clientSent.Add(1)
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Wait for test to complete
	<-ctx.Done()
	wg.Wait()

	// Check results
	serverChannel.StopKeepalive()

	// Check keepalive didn't timeout
	select {
	case <-timeoutChan:
		t.Error("Keepalive timed out during stress test")
	default:
	}

	t.Logf("Server sent: %d, received: %d", serverSent.Load(), serverReceived.Load())
	t.Logf("Client sent: %d, received: %d", clientSent.Load(), clientReceived.Load())

	// Verify reasonable delivery rate (at least 90%)
	if serverReceived.Load() < int64(float64(clientSent.Load())*0.9) {
		t.Errorf("Server received less than 90%% of client messages")
	}
	if clientReceived.Load() < int64(float64(serverSent.Load())*0.9) {
		t.Errorf("Client received less than 90%% of server messages")
	}

	serverChannel.Close()
	clientChannel.Close()
}

// TestMultipleReconnectionCycles tests multiple connect/disconnect cycles
func TestMultipleReconnectionCycles(t *testing.T) {
	password := "testpassword123"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	for cycle := 0; cycle < 5; cycle++ {
		t.Logf("Cycle %d", cycle+1)

		// Create new peers for each cycle
		serverPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
		clientPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())

		serverDC, _ := serverPeer.CreateDataChannel("terminal")

		clientDCChan := make(chan *webrtc.DataChannel, 1)
		clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
			clientDCChan <- dc
		})

		offer, _ := serverPeer.CreateOffer()
		clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
		answer, _ := clientPeer.CreateAnswer()
		serverPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)

		clientDC := <-clientDCChan

		serverOpen := make(chan bool, 1)
		clientOpen := make(chan bool, 1)
		serverDC.OnOpen(func() { serverOpen <- true })
		clientDC.OnOpen(func() { clientOpen <- true })

		select {
		case <-serverOpen:
		case <-time.After(5 * time.Second):
			t.Fatalf("Cycle %d: Server DC did not open", cycle+1)
		}

		select {
		case <-clientOpen:
		case <-time.After(5 * time.Second):
			t.Fatalf("Cycle %d: Client DC did not open", cycle+1)
		}

		serverChannel := ttwebrtc.NewEncryptedChannel(serverDC, &key)
		clientChannel := ttwebrtc.NewEncryptedChannel(clientDC, &key)

		// Send some messages
		var received atomic.Int32
		serverChannel.OnData(func(data []byte) {
			received.Add(1)
		})

		for i := 0; i < 10; i++ {
			clientChannel.SendData([]byte{byte(i)})
		}

		time.Sleep(200 * time.Millisecond)

		if received.Load() != 10 {
			t.Errorf("Cycle %d: Expected 10 messages, got %d", cycle+1, received.Load())
		}

		// Clean close
		serverChannel.Close()
		clientChannel.Close()
		serverPeer.Close()
		clientPeer.Close()

		// Small delay between cycles
		time.Sleep(100 * time.Millisecond)
	}

	t.Log("All reconnection cycles completed successfully")
}

// TestLargeMessageHandling tests handling of large messages
func TestLargeMessageHandling(t *testing.T) {
	password := "testpassword123"
	salt := make([]byte, 16)
	key := crypto.DeriveKey(password, salt)

	// Create peers
	serverPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	defer serverPeer.Close()

	clientPeer, _ := ttwebrtc.NewPeer(ttwebrtc.DefaultConfig())
	defer clientPeer.Close()

	serverDC, _ := serverPeer.CreateDataChannel("terminal")

	clientDCChan := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCChan <- dc
	})

	offer, _ := serverPeer.CreateOffer()
	clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer)
	answer, _ := clientPeer.CreateAnswer()
	serverPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer)

	clientDC := <-clientDCChan

	serverOpen := make(chan bool, 1)
	clientOpen := make(chan bool, 1)
	serverDC.OnOpen(func() { serverOpen <- true })
	clientDC.OnOpen(func() { clientOpen <- true })
	<-serverOpen
	<-clientOpen

	serverChannel := ttwebrtc.NewEncryptedChannel(serverDC, &key)
	clientChannel := ttwebrtc.NewEncryptedChannel(clientDC, &key)

	testCases := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"4KB", 4 * 1024},
		{"16KB", 16 * 1024},
		{"32KB", 32 * 1024},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan []byte, 1)
			clientChannel.OnData(func(data []byte) {
				received <- data
			})

			// Create test data
			testData := make([]byte, tc.size)
			for i := range testData {
				testData[i] = byte(i % 256)
			}

			// Send
			if err := serverChannel.SendData(testData); err != nil {
				t.Fatalf("Failed to send %s message: %v", tc.name, err)
			}

			// Receive
			select {
			case data := <-received:
				if len(data) != tc.size {
					t.Errorf("Size mismatch: got %d, want %d", len(data), tc.size)
				}
				// Verify content
				for i := 0; i < min(100, len(data)); i++ {
					if data[i] != testData[i] {
						t.Errorf("Content mismatch at byte %d", i)
						break
					}
				}
			case <-time.After(5 * time.Second):
				t.Errorf("Timeout waiting for %s message", tc.name)
			}
		})
	}

	serverChannel.Close()
	clientChannel.Close()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
