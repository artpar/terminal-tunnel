// Package server provides the terminal tunnel server implementation
package server

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// PTY manages a pseudo-terminal
type PTY struct {
	ptmx *os.File
	cmd  *exec.Cmd

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
	if p.cmd.Process != nil {
		syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
	}

	// Close the PTY
	if err := p.ptmx.Close(); err != nil {
		return err
	}

	// Wait for the process to exit
	p.cmd.Wait()
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
	pty    *PTY
	send   func([]byte) error
	done   chan struct{}
	closed bool
	mu     sync.Mutex
}

// NewBridge creates a bridge between a PTY and a send function
func NewBridge(pty *PTY, send func([]byte) error) *Bridge {
	return &Bridge{
		pty:  pty,
		send: send,
		done: make(chan struct{}),
	}
}

// Start begins reading from the PTY and sending to the channel
func (b *Bridge) Start() {
	go b.readLoop()
}

// readLoop continuously reads from PTY and sends to channel
func (b *Bridge) readLoop() {
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

			if err := b.send(data); err != nil {
				b.Close()
				return
			}
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

// Close stops the bridge
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
