//go:build windows

package server

import (
	"errors"
	"io"
)

var ErrWindowsNotSupported = errors.New("PTY not supported on Windows - use WSL")

// PTY manages a pseudo-terminal (stub for Windows)
type PTY struct {
	closed bool
}

// StartPTY is not supported on Windows
func StartPTY(shell string) (*PTY, error) {
	return nil, ErrWindowsNotSupported
}

// ReattachPTY is not supported on Windows
func ReattachPTY(ptyPath string, shellPID int) (*PTY, error) {
	return nil, ErrWindowsNotSupported
}

// IsProcessRunning checks if a process is running (stub for Windows)
func IsProcessRunning(pid int) bool {
	return false
}

// IsReattached returns whether this PTY was reattached (stub for Windows)
func (p *PTY) IsReattached() bool {
	return false
}

func (p *PTY) Read(buf []byte) (int, error)   { return 0, ErrWindowsNotSupported }
func (p *PTY) Write(data []byte) (int, error) { return 0, ErrWindowsNotSupported }
func (p *PTY) Resize(rows, cols uint16) error { return ErrWindowsNotSupported }
func (p *PTY) Close() error                   { return nil }
func (p *PTY) Wait() error                    { return ErrWindowsNotSupported }
func (p *PTY) Fd() uintptr                    { return 0 }
func (p *PTY) Name() string                   { return "" }
func (p *PTY) PID() int                       { return 0 }

// Bridge connects the PTY to a data channel (stub for Windows)
type Bridge struct {
	closed bool
}

func NewBridge(pty *PTY, send func([]byte) error) *Bridge {
	return &Bridge{}
}

func (b *Bridge) Start()                               {}
func (b *Bridge) HandleData(data []byte) error         { return ErrWindowsNotSupported }
func (b *Bridge) HandleResize(rows, cols uint16) error { return ErrWindowsNotSupported }
func (b *Bridge) Close() error                         { return io.ErrClosedPipe }
func (b *Bridge) CloseWithoutPTY()                     {}
