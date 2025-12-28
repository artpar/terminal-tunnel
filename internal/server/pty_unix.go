//go:build !windows

// Package server provides the terminal tunnel server implementation
package server

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// PTY manages a pseudo-terminal
type PTY struct {
	ptmx       *os.File
	cmd        *exec.Cmd
	pid        int  // stored PID for reattached PTYs
	reattached bool // true if this PTY was reattached

	mu     sync.Mutex
	closed bool
}

// StartPTY creates a new PTY with the given shell
func StartPTY(shell string) (*PTY, error) {
	if shell == "" {
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	// Set initial size
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
	})

	return &PTY{
		ptmx: ptmx,
		cmd:  cmd,
	}, nil
}

// ReattachPTY reopens an existing PTY device and reconnects to a running shell
// This is used to recover sessions after daemon restart
func ReattachPTY(ptyPath string, shellPID int) (*PTY, error) {
	// Verify the shell process is still running
	if !IsProcessRunning(shellPID) {
		return nil, fmt.Errorf("shell process %d is not running", shellPID)
	}

	// Open the PTY device
	ptmx, err := os.OpenFile(ptyPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open PTY %s: %w", ptyPath, err)
	}

	return &PTY{
		ptmx:       ptmx,
		pid:        shellPID,
		reattached: true,
	}, nil
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
	// On Unix, FindProcess always succeeds, so we send signal 0 to check
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// IsReattached returns true if this PTY was reattached after daemon restart
func (p *PTY) IsReattached() bool {
	return p.reattached
}

// Read reads data from the PTY
func (p *PTY) Read(buf []byte) (int, error) {
	return p.ptmx.Read(buf)
}

// Write writes data to the PTY
func (p *PTY) Write(data []byte) (int, error) {
	return p.ptmx.Write(data)
}

// Resize changes the PTY size
func (p *PTY) Resize(rows, cols uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return io.ErrClosedPipe
	}

	return pty.Setsize(p.ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// Name returns the PTY device path (e.g., /dev/pts/0)
func (p *PTY) Name() string {
	return p.ptmx.Name()
}

// PID returns the shell process PID
func (p *PTY) PID() int {
	if p.reattached {
		return p.pid
	}
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

// Close closes the PTY and terminates the shell process
func (p *PTY) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	// Send SIGHUP to the process group
	pid := p.PID()
	if pid > 0 {
		syscall.Kill(-pid, syscall.SIGHUP)
	}

	// Close the PTY
	if err := p.ptmx.Close(); err != nil {
		return err
	}

	// Wait for the process to exit (only for non-reattached PTYs)
	if !p.reattached && p.cmd != nil {
		p.cmd.Wait()
	}
	return nil
}

// Wait waits for the shell process to exit
func (p *PTY) Wait() error {
	return p.cmd.Wait()
}

// Fd returns the file descriptor of the PTY
func (p *PTY) Fd() uintptr {
	return p.ptmx.Fd()
}

// Bridge connects the PTY to a data channel for bidirectional I/O
type Bridge struct {
	pty         *PTY
	send        func([]byte) error
	viewerSends []func([]byte) error // Additional send functions for viewers (read-only)
	recorder    func([]byte) error   // Optional recording callback
	localOutput io.Writer            // Optional local output (for interactive mode)
	done        chan struct{}
	exited      chan struct{} // Closed when readLoop exits
	closed      bool
	mu          sync.Mutex
}

// NewBridge creates a bridge between a PTY and a send function
func NewBridge(pty *PTY, send func([]byte) error) *Bridge {
	return &Bridge{
		pty:    pty,
		send:   send,
		done:   make(chan struct{}),
		exited: make(chan struct{}),
	}
}

// AddViewerSend adds an additional send function for viewer channels (read-only)
func (b *Bridge) AddViewerSend(send func([]byte) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
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

		n, err := b.pty.Read(buf)
		if err != nil {
			b.Close()
			return
		}

		if n > 0 {
			// Make a copy of the data
			data := make([]byte, n)
			copy(data, buf[:n])

			// Send to primary (control) channel
			if err := b.send(data); err != nil {
				fmt.Printf("  [Debug] Bridge send error: %v\n", err)
				b.Close()
				return
			}

			// Send to viewer channels (best effort - don't fail if viewers disconnect)
			b.mu.Lock()
			for _, viewerSend := range b.viewerSends {
				viewerSend(data) // Ignore errors for viewers
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
