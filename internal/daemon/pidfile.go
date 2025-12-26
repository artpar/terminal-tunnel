package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	// DefaultStateDir is the default directory for daemon state
	DefaultStateDir = ".tt"
	// PIDFileName is the name of the PID file
	PIDFileName = "tt.pid"
	// SocketFileName is the name of the Unix socket
	SocketFileName = "tt.sock"
	// SessionsDir is the directory for session state files
	SessionsDir = "sessions"
)

// GetStateDir returns the path to the state directory
func GetStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), DefaultStateDir)
	}
	return filepath.Join(home, DefaultStateDir)
}

// GetPIDPath returns the path to the PID file
func GetPIDPath() string {
	return filepath.Join(GetStateDir(), PIDFileName)
}

// GetSocketPath returns the path to the Unix socket
func GetSocketPath() string {
	return filepath.Join(GetStateDir(), SocketFileName)
}

// GetSessionsDir returns the path to the sessions directory
func GetSessionsDir() string {
	return filepath.Join(GetStateDir(), SessionsDir)
}

// EnsureStateDir creates the state directory if it doesn't exist
func EnsureStateDir() error {
	stateDir := GetStateDir()
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}
	sessionsDir := GetSessionsDir()
	if err := os.MkdirAll(sessionsDir, 0700); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}
	return nil
}

// WritePID writes the current process PID to the PID file
func WritePID() error {
	if err := EnsureStateDir(); err != nil {
		return err
	}
	pidPath := GetPIDPath()
	pid := os.Getpid()
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0600)
}

// ReadPID reads the PID from the PID file
func ReadPID() (int, error) {
	pidPath := GetPIDPath()
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file content: %w", err)
	}
	return pid, nil
}

// RemovePID removes the PID file
func RemovePID() error {
	return os.Remove(GetPIDPath())
}

// RemoveSocket removes the Unix socket file
func RemoveSocket() error {
	return os.Remove(GetSocketPath())
}

// IsProcessRunning checks if a process with the given PID is running
func IsProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Send signal 0 to check if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// IsDaemonRunning checks if the daemon is currently running
// Returns (running, pid)
func IsDaemonRunning() (bool, int) {
	pid, err := ReadPID()
	if err != nil {
		return false, 0
	}
	if !IsProcessRunning(pid) {
		// Stale PID file, clean up
		RemovePID()
		RemoveSocket()
		return false, 0
	}
	return true, pid
}

// Cleanup removes all daemon state files
func Cleanup() {
	RemovePID()
	RemoveSocket()
}
