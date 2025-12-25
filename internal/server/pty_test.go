package server

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestStartPTY(t *testing.T) {
	pty, err := StartPTY("/bin/sh")
	if err != nil {
		t.Fatalf("StartPTY failed: %v", err)
	}
	defer pty.Close()

	if pty.ptmx == nil {
		t.Error("ptmx should not be nil")
	}
	if pty.cmd == nil {
		t.Error("cmd should not be nil")
	}
}

func TestStartPTYDefaultShell(t *testing.T) {
	pty, err := StartPTY("")
	if err != nil {
		t.Fatalf("StartPTY with empty shell failed: %v", err)
	}
	defer pty.Close()
}

func TestPTYReadWrite(t *testing.T) {
	pty, err := StartPTY("/bin/sh")
	if err != nil {
		t.Fatalf("StartPTY failed: %v", err)
	}
	defer pty.Close()

	// Write a command
	testCmd := "echo hello\n"
	_, err = pty.Write([]byte(testCmd))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read output (may include echo of command + result)
	buf := make([]byte, 1024)
	var output bytes.Buffer
	done := make(chan bool)

	go func() {
		for {
			n, err := pty.Read(buf)
			if err != nil {
				break
			}
			output.Write(buf[:n])
			if strings.Contains(output.String(), "hello") {
				done <- true
				return
			}
		}
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for output")
	}

	if !strings.Contains(output.String(), "hello") {
		t.Errorf("output should contain 'hello', got: %q", output.String())
	}
}

func TestPTYResize(t *testing.T) {
	pty, err := StartPTY("/bin/sh")
	if err != nil {
		t.Fatalf("StartPTY failed: %v", err)
	}
	defer pty.Close()

	err = pty.Resize(40, 120)
	if err != nil {
		t.Errorf("Resize failed: %v", err)
	}
}

func TestPTYClose(t *testing.T) {
	pty, err := StartPTY("/bin/sh")
	if err != nil {
		t.Fatalf("StartPTY failed: %v", err)
	}

	err = pty.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Second close should be idempotent
	err = pty.Close()
	if err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

func TestPTYResizeAfterClose(t *testing.T) {
	pty, err := StartPTY("/bin/sh")
	if err != nil {
		t.Fatalf("StartPTY failed: %v", err)
	}

	pty.Close()

	err = pty.Resize(40, 120)
	if err == nil {
		t.Error("Resize after close should fail")
	}
}

func TestBridge(t *testing.T) {
	pty, err := StartPTY("/bin/sh")
	if err != nil {
		t.Fatalf("StartPTY failed: %v", err)
	}
	defer pty.Close()

	received := make(chan []byte, 10)
	bridge := NewBridge(pty, func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		received <- cp
		return nil
	})

	bridge.Start()
	defer bridge.Close()

	// Send a command through the bridge
	err = bridge.HandleData([]byte("echo test\n"))
	if err != nil {
		t.Fatalf("HandleData failed: %v", err)
	}

	// Wait for output
	timeout := time.After(5 * time.Second)
	var output bytes.Buffer

	for {
		select {
		case data := <-received:
			output.Write(data)
			if strings.Contains(output.String(), "test") {
				return // Success
			}
		case <-timeout:
			t.Fatalf("timeout waiting for output, got: %q", output.String())
		}
	}
}

func TestBridgeResize(t *testing.T) {
	pty, err := StartPTY("/bin/sh")
	if err != nil {
		t.Fatalf("StartPTY failed: %v", err)
	}
	defer pty.Close()

	bridge := NewBridge(pty, func(data []byte) error {
		return nil
	})
	defer bridge.Close()

	err = bridge.HandleResize(48, 160)
	if err != nil {
		t.Errorf("HandleResize failed: %v", err)
	}
}

func TestBridgeClose(t *testing.T) {
	pty, err := StartPTY("/bin/sh")
	if err != nil {
		t.Fatalf("StartPTY failed: %v", err)
	}

	bridge := NewBridge(pty, func(data []byte) error {
		return nil
	})

	bridge.Start()

	err = bridge.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Second close should be idempotent
	err = bridge.Close()
	if err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}
