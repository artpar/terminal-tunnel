//go:build windows

// Package server provides the terminal tunnel server implementation
package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/UserExistsError/conpty"
)

// PTY manages a pseudo-terminal using Windows ConPTY
type PTY struct {
	cpty *conpty.ConPty
	cmd  *exec.Cmd

	mu     sync.Mutex
	closed bool
}

// StartPTY creates a new PTY with the given shell using ConPTY
func StartPTY(shell string) (*PTY, error) {
	if shell == "" {
		// Default to PowerShell on Windows, fallback to cmd.exe
		shell = "powershell.exe"
		if _, err := exec.LookPath(shell); err != nil {
			shell = "cmd.exe"
		}
	}

	// Create ConPTY with initial size 80x24
	cpty, err := conpty.Start(shell, conpty.ConPtyDimensions(80, 24))
	if err != nil {
		return nil, fmt.Errorf("failed to start ConPTY: %w", err)
	}

	return &PTY{
		cpty: cpty,
	}, nil
}

// ReattachPTY is not fully supported on Windows
// Windows ConPTY doesn't support reattaching to existing sessions
func ReattachPTY(ptyPath string, shellPID int) (*PTY, error) {
	return nil, fmt.Errorf("PTY reattachment not supported on Windows")
}

// IsProcessRunning checks if a process with the given PID is running
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows, FindProcess always succeeds for any PID
	// We need to try to get process info to check if it exists
	// A simple approach is to check if we can open the process
	_ = process
	return false // Conservative - reattachment not supported anyway
}

// IsReattached returns whether this PTY was reattached
func (p *PTY) IsReattached() bool {
	return false // Reattachment not supported on Windows
}

// Read reads data from the PTY
func (p *PTY) Read(buf []byte) (int, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	cpty := p.cpty
	p.mu.Unlock()

	return cpty.Read(buf)
}

// Write writes data to the PTY
func (p *PTY) Write(data []byte) (int, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	cpty := p.cpty
	p.mu.Unlock()

	return cpty.Write(data)
}

// Resize changes the PTY size
func (p *PTY) Resize(rows, cols uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return io.ErrClosedPipe
	}

	return p.cpty.Resize(int(cols), int(rows))
}

// Close closes the PTY and terminates the shell process
func (p *PTY) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	cpty := p.cpty
	p.mu.Unlock()

	return cpty.Close()
}

// Wait waits for the shell process to exit
func (p *PTY) Wait() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	cpty := p.cpty
	p.mu.Unlock()

	_, err := cpty.Wait(context.Background()) // Wait indefinitely
	return err
}

// Fd returns the file descriptor of the PTY (not applicable on Windows)
func (p *PTY) Fd() uintptr {
	return 0 // ConPTY doesn't expose a single FD
}

// Name returns the PTY device path (not applicable on Windows)
func (p *PTY) Name() string {
	return "conpty" // Windows doesn't have /dev/pts paths
}

// PID returns the shell process PID
func (p *PTY) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed || p.cpty == nil {
		return 0
	}

	return p.cpty.Pid()
}

// Bridge connects the PTY to a data channel for bidirectional I/O
type Bridge struct {
	pty           *PTY
	send          func([]byte) error
	viewerSends   []func([]byte) error // Additional send functions for viewers (read-only)
	recorder      func([]byte) error   // Optional recording callback
	localOutput   io.Writer            // Optional local output (for interactive mode)
	done          chan struct{}
	exited        chan struct{} // Closed when readLoop exits
	closed        bool
	paused        bool   // When true, output is buffered instead of sent
	buffer        []byte // Ring buffer for output during pause
	historyBuffer []byte // Always-on buffer for late-join viewer replay
	bufferMax     int    // Maximum buffer size (default 64KB)
	mu            sync.Mutex
}

const defaultBufferMax = 64 * 1024 // 64KB default buffer

// NewBridge creates a bridge between a PTY and a send function
func NewBridge(pty *PTY, send func([]byte) error) *Bridge {
	return &Bridge{
		pty:       pty,
		send:      send,
		done:      make(chan struct{}),
		exited:    make(chan struct{}),
		bufferMax: defaultBufferMax,
	}
}

// Pause switches the bridge to buffering mode
// Output is stored in a ring buffer instead of being sent
func (b *Bridge) Pause() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.paused = true
	b.viewerSends = nil // Clear viewer sends when pausing
	fmt.Printf("  [Debug] Bridge paused, buffering output (max %d bytes)\n", b.bufferMax)
}

// Resume switches back to sending mode and flushes buffered output
// Returns the number of bytes that were buffered and sent
func (b *Bridge) Resume(send func([]byte) error) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	bufferedBytes := len(b.buffer)
	if bufferedBytes > 0 {
		fmt.Printf("  [Debug] Bridge resuming, flushing %d buffered bytes\n", bufferedBytes)
		// Send buffered data to new client
		if err := send(b.buffer); err != nil {
			fmt.Printf("  [Debug] Error flushing buffer: %v\n", err)
		}
		b.buffer = nil // Clear buffer
	}

	b.send = send
	b.paused = false
	return bufferedBytes
}

// IsPaused returns whether the bridge is in paused (buffering) mode
func (b *Bridge) IsPaused() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.paused
}

// AddViewerSend adds an additional send function for viewer channels (read-only)
// This also sends any buffered output to the new viewer for late-join replay
func (b *Bridge) AddViewerSend(send func([]byte) error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Send history buffer to new viewer for late-join replay
	if len(b.historyBuffer) > 0 {
		fmt.Printf("  [Debug] Sending %d bytes of history to new viewer\n", len(b.historyBuffer))
		// Make a copy to avoid race conditions
		history := make([]byte, len(b.historyBuffer))
		copy(history, b.historyBuffer)
		go send(history) // Non-blocking send
	}

	b.viewerSends = append(b.viewerSends, send)
}

// ClearViewerSends removes all viewer send functions
func (b *Bridge) ClearViewerSends() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.viewerSends = nil
}

// SetRecorder sets the recording callback for PTY output
func (b *Bridge) SetRecorder(recorder func([]byte) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recorder = recorder
}

// SetLocalOutput sets a local output writer (for interactive/SSH-like mode)
func (b *Bridge) SetLocalOutput(w io.Writer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.localOutput = w
}

// Start begins reading from the PTY and sending to the channel
func (b *Bridge) Start() {
	go b.readLoop()
}

// readLoop continuously reads from PTY and sends to channel
func (b *Bridge) readLoop() {
	defer close(b.exited) // Signal that readLoop has exited
	buf := make([]byte, 4096)

	for {
		select {
		case <-b.done:
			return
		default:
		}

		// Read from PTY - ConPTY Read blocks until data is available
		// We use a goroutine with timeout to make it interruptible
		readDone := make(chan struct{})
		var n int
		var err error

		go func() {
			n, err = b.pty.Read(buf)
			close(readDone)
		}()

		select {
		case <-b.done:
			return
		case <-readDone:
			if err != nil {
				b.Close()
				return
			}
		case <-time.After(100 * time.Millisecond):
			// Timeout - check if we should exit
			continue
		}

		if n > 0 {
			// Make a copy of the data
			data := make([]byte, n)
			copy(data, buf[:n])

			b.mu.Lock()

			// Always update history buffer for late-join viewer replay
			b.historyBuffer = append(b.historyBuffer, data...)
			if len(b.historyBuffer) > b.bufferMax {
				b.historyBuffer = b.historyBuffer[len(b.historyBuffer)-b.bufferMax:]
			}

			if b.paused {
				// Buffer the data instead of sending
				b.buffer = append(b.buffer, data...)
				// Trim to max buffer size (keep most recent data)
				if len(b.buffer) > b.bufferMax {
					b.buffer = b.buffer[len(b.buffer)-b.bufferMax:]
				}
				b.mu.Unlock()
				continue
			}

			// Send to primary (control) channel
			if err := b.send(data); err != nil {
				fmt.Printf("  [Debug] Bridge send error: %v\n", err)
				b.mu.Unlock()
				b.Close()
				return
			}

			// Send to viewer channels (best effort - don't fail if viewers disconnect)
			// Use goroutines to prevent slow viewers from blocking main stream
			for _, viewerSend := range b.viewerSends {
				vs := viewerSend // Capture for goroutine
				go vs(data)      // Non-blocking send
			}
			// Record if recorder is set (best effort - don't fail on recording errors)
			if b.recorder != nil {
				b.recorder(data)
			}
			// Write to local output if set (for interactive mode)
			if b.localOutput != nil {
				b.localOutput.Write(data)
			}
			b.mu.Unlock()
		}
	}
}

// HandleData writes incoming data to the PTY
func (b *Bridge) HandleData(data []byte) error {
	_, err := b.pty.Write(data)
	return err
}

// HandleResize resizes the PTY
func (b *Bridge) HandleResize(rows, cols uint16) error {
	return b.pty.Resize(rows, cols)
}

// Close stops the bridge and closes the PTY
func (b *Bridge) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	close(b.done)
	b.mu.Unlock()

	return b.pty.Close()
}

// CloseWithoutPTY stops the bridge but keeps the PTY running for reconnection
func (b *Bridge) CloseWithoutPTY() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	close(b.done)
	b.mu.Unlock()
}

// WaitForExit waits for the readLoop to exit (with timeout)
func (b *Bridge) WaitForExit(timeout time.Duration) bool {
	select {
	case <-b.exited:
		return true
	case <-time.After(timeout):
		return false
	}
}
