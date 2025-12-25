package server

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

//go:embed testdata
var testFS embed.FS

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func TestNewSignalingServer(t *testing.T) {
	server, err := NewSignalingServer("test-offer", "test-session", "dGVzdHNhbHQ=", testFS)
	if err != nil {
		t.Fatalf("NewSignalingServer failed: %v", err)
	}
	defer server.Close()

	if server.Port() <= 0 {
		t.Errorf("port should be positive, got %d", server.Port())
	}
}

func TestSignalingServerOfferEndpoint(t *testing.T) {
	offer := "v=0\ntest-sdp"
	server, err := NewSignalingServer(offer, "test-session", "dGVzdHNhbHQ=", testFS)
	if err != nil {
		t.Fatalf("NewSignalingServer failed: %v", err)
	}
	defer server.Close()

	server.Start()
	time.Sleep(50 * time.Millisecond) // Give server time to start

	resp, err := http.Get("http://localhost:" + itoa(server.Port()) + "/offer")
	if err != nil {
		t.Fatalf("GET /offer failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if result["offer"] != offer {
		t.Errorf("offer = %q, want %q", result["offer"], offer)
	}

	if result["sessionId"] != "test-session" {
		t.Errorf("sessionId = %q, want %q", result["sessionId"], "test-session")
	}
}

func TestSignalingServerAnswerEndpoint(t *testing.T) {
	server, err := NewSignalingServer("test-offer", "test-session", "dGVzdHNhbHQ=", testFS)
	if err != nil {
		t.Fatalf("NewSignalingServer failed: %v", err)
	}
	defer server.Close()

	server.Start()
	time.Sleep(50 * time.Millisecond)

	// Start waiting for answer in goroutine
	answerChan := make(chan string, 1)
	go func() {
		answer, err := server.WaitForAnswer(5 * time.Second)
		if err != nil {
			return
		}
		answerChan <- answer
	}()

	// Submit answer
	payload := map[string]string{"answer": "v=0\ntest-answer"}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(
		"http://localhost:"+itoa(server.Port())+"/answer",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST /answer failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Check that answer was received
	select {
	case answer := <-answerChan:
		if answer != "v=0\ntest-answer" {
			t.Errorf("answer = %q, want %q", answer, "v=0\ntest-answer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for answer")
	}
}

func TestSignalingServerAnswerTimeout(t *testing.T) {
	server, err := NewSignalingServer("test-offer", "test-session", "dGVzdHNhbHQ=", testFS)
	if err != nil {
		t.Fatalf("NewSignalingServer failed: %v", err)
	}
	defer server.Close()

	server.Start()

	_, err = server.WaitForAnswer(100 * time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestSignalingServerHealth(t *testing.T) {
	server, err := NewSignalingServer("test-offer", "test-session", "dGVzdHNhbHQ=", testFS)
	if err != nil {
		t.Fatalf("NewSignalingServer failed: %v", err)
	}
	defer server.Close()

	server.Start()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://localhost:" + itoa(server.Port()) + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestSignalingServerClose(t *testing.T) {
	server, err := NewSignalingServer("test-offer", "test-session", "dGVzdHNhbHQ=", testFS)
	if err != nil {
		t.Fatalf("NewSignalingServer failed: %v", err)
	}

	server.Start()
	time.Sleep(50 * time.Millisecond)

	err = server.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Second close should be idempotent
	err = server.Close()
	if err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

func TestGetLocalIP(t *testing.T) {
	ip, err := GetLocalIP()
	if err != nil {
		t.Skipf("GetLocalIP failed (may not have network): %v", err)
	}

	if ip == "" {
		t.Error("IP should not be empty")
	}

	t.Logf("Local IP: %s", ip)
}

