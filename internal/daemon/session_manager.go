package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/artpar/terminal-tunnel/internal/server"
)

// Security: Minimum password length to prevent brute-force attacks
const MinPasswordLength = 12

// MaxSessions limits the number of concurrent sessions (DoS protection)
const MaxSessions = 100

// ErrPasswordTooShort is returned when password doesn't meet minimum length
var ErrPasswordTooShort = errors.New("password must be at least 12 characters")

// ErrTooManySessions is returned when session limit is reached
var ErrTooManySessions = errors.New("maximum session limit reached")

// ManagedSession represents a session managed by the daemon
type ManagedSession struct {
	State    *SessionState
	Server   *server.Server
	Cancel   context.CancelFunc
	Password string      // Not persisted, kept in memory
	pty      *server.PTY // For recovered sessions without server
}

// SessionState represents the persistent state of a session
type SessionState struct {
	ID         string        `json:"id"`
	ShortCode  string        `json:"short_code"`
	PTYPath    string        `json:"pty_path"`
	ShellPID   int           `json:"shell_pid"`
	Shell      string        `json:"shell"`
	Salt       string        `json:"salt"`
	Status     SessionStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
	LastSeen   time.Time     `json:"last_seen"`
	RelayURL   string        `json:"relay_url"`
	ClientURL  string        `json:"client_url"`
	Public     bool          `json:"public,omitempty"`      // True if public viewer mode enabled
	ViewerCode string        `json:"viewer_code,omitempty"` // Code for public viewers (ends with V)
	ViewerURL  string        `json:"viewer_url,omitempty"`  // URL for public viewers
}

// SessionStartResult contains info returned when starting a session
type SessionStartResult struct {
	ID         string
	ShortCode  string
	Password   string
	ClientURL  string
	Status     SessionStatus
	Public     bool   // True if public viewer mode enabled
	ViewerCode string // Code for public viewers (ends with V)
	ViewerURL  string // URL for public viewers
}

// SessionManager manages all sessions
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*ManagedSession // keyed by ID
	byCode   map[string]*ManagedSession // keyed by short code
	daemon   *Daemon
}

// NewSessionManager creates a new session manager
func NewSessionManager(d *Daemon) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*ManagedSession),
		byCode:   make(map[string]*ManagedSession),
		daemon:   d,
	}
}

// generateID generates a unique session ID
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// generatePassword generates a random password
func generatePassword() string {
	b := make([]byte, 12)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// StartSession starts a new session
func (sm *SessionManager) StartSession(params StartSessionParams) (*SessionStartResult, error) {
	sm.mu.Lock()

	// Security: Check session limit (DoS protection)
	if len(sm.sessions) >= MaxSessions {
		sm.mu.Unlock()
		return nil, ErrTooManySessions
	}

	// Generate ID and password
	id := generateID()
	password := params.Password
	if password == "" {
		password = generatePassword()
	} else {
		// Security: Validate user-provided password length
		if len(password) < MinPasswordLength {
			sm.mu.Unlock()
			return nil, ErrPasswordTooShort
		}
	}

	shell := params.Shell
	if shell == "" {
		shell = "/bin/sh"
	}

	// Create server options
	opts := server.Options{
		Password: password,
		Shell:    shell,
		Timeout:  0, // No timeout for daemon-managed sessions
		NoTURN:   params.NoTURN,
		Public:   params.Public,
		Record:   params.Record,
	}

	// Create context for this session
	ctx, cancel := context.WithCancel(sm.daemon.GetContext())

	// Create server instance
	srv, err := server.New(opts)
	if err != nil {
		cancel()
		sm.mu.Unlock()
		return nil, fmt.Errorf("failed to create server: %w", err)
	}

	// Create managed session
	ms := &ManagedSession{
		State: &SessionState{
			ID:        id,
			Status:    StatusWaiting,
			Shell:     shell,
			CreatedAt: time.Now(),
			LastSeen:  time.Now(),
			Public:    params.Public,
		},
		Server:   srv,
		Cancel:   cancel,
		Password: password,
	}

	// Store session
	sm.sessions[id] = ms

	// Channel to wait for short code
	shortCodeReady := make(chan struct{}, 1)

	// Set up callbacks to update state
	srv.SetCallbacks(server.Callbacks{
		OnShortCodeReady: func(code, clientURL string) {
			sm.mu.Lock()
			ms.State.ShortCode = code
			ms.State.ClientURL = clientURL
			sm.byCode[code] = ms
			sm.mu.Unlock()
			// Signal that short code is ready
			select {
			case shortCodeReady <- struct{}{}:
			default:
			}
		},
		OnViewerCodeReady: func(viewerCode, viewerURL string) {
			sm.mu.Lock()
			ms.State.ViewerCode = viewerCode
			ms.State.ViewerURL = viewerURL
			sm.mu.Unlock()
		},
		OnClientConnect: func() {
			sm.mu.Lock()
			ms.State.Status = StatusConnected
			ms.State.LastSeen = time.Now()
			sm.mu.Unlock()
		},
		OnClientDisconnect: func() {
			sm.mu.Lock()
			ms.State.Status = StatusDisconnected
			sm.mu.Unlock()
		},
		OnViewerConnect: func() {
			sm.mu.Lock()
			ms.State.LastSeen = time.Now()
			sm.mu.Unlock()
		},
		OnViewerDisconnect: func() {
			// Viewers disconnecting doesn't change session status
		},
		OnPTYReady: func(ptyPath string, shellPID int) {
			sm.mu.Lock()
			ms.State.PTYPath = ptyPath
			ms.State.ShellPID = shellPID
			sm.mu.Unlock()
			// Save state to disk
			sm.SaveSession(ms)
		},
	})

	sm.mu.Unlock()

	// Start server in background
	go func() {
		defer func() {
			sm.mu.Lock()
			delete(sm.sessions, id)
			if ms.State.ShortCode != "" {
				delete(sm.byCode, ms.State.ShortCode)
			}
			sm.mu.Unlock()
		}()

		// Start the server
		if err := srv.Start(ctx); err != nil {
			if ctx.Err() == nil {
				fmt.Printf("Session %s error: %v\n", id, err)
			}
		}
	}()

	// Wait for short code to be ready (up to 10 seconds)
	select {
	case <-shortCodeReady:
		// Short code is ready
	case <-time.After(10 * time.Second):
		// Timeout - return what we have
	case <-ctx.Done():
		return nil, fmt.Errorf("session startup cancelled")
	}

	sm.mu.RLock()
	result := &SessionStartResult{
		ID:         id,
		ShortCode:  ms.State.ShortCode,
		Password:   password,
		ClientURL:  ms.State.ClientURL,
		Status:     ms.State.Status,
		Public:     ms.State.Public,
		ViewerCode: ms.State.ViewerCode,
		ViewerURL:  ms.State.ViewerURL,
	}
	sm.mu.RUnlock()

	return result, nil
}

// StopSession stops a session by ID or short code
func (sm *SessionManager) StopSession(idOrCode string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Try to find by ID first
	ms, ok := sm.sessions[idOrCode]
	if !ok {
		// Try by short code
		ms, ok = sm.byCode[idOrCode]
	}
	if !ok {
		return fmt.Errorf("session not found: %s", idOrCode)
	}

	// Cancel the context to stop the server (if running)
	if ms.Cancel != nil {
		ms.Cancel()
	}

	// Close PTY for recovered sessions without server
	if ms.pty != nil && ms.Server == nil {
		ms.pty.Close()
	}

	// Remove from maps
	delete(sm.sessions, ms.State.ID)
	if ms.State.ShortCode != "" {
		delete(sm.byCode, ms.State.ShortCode)
	}

	// Remove state file
	RemoveSessionState(ms.State.ShortCode)

	return nil
}

// StopAllSessions stops all sessions
func (sm *SessionManager) StopAllSessions() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, ms := range sm.sessions {
		// Cancel server if running
		if ms.Cancel != nil {
			ms.Cancel()
		}
		// Close PTY for recovered sessions without server
		if ms.pty != nil && ms.Server == nil {
			ms.pty.Close()
		}
		if ms.State.ShortCode != "" {
			RemoveSessionState(ms.State.ShortCode)
		}
	}

	sm.sessions = make(map[string]*ManagedSession)
	sm.byCode = make(map[string]*ManagedSession)
}

// ListSessions returns info about all sessions
func (sm *SessionManager) ListSessions() []SessionInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]SessionInfo, 0, len(sm.sessions))
	for _, ms := range sm.sessions {
		result = append(result, SessionInfo{
			ID:        ms.State.ID,
			ShortCode: ms.State.ShortCode,
			Status:    ms.State.Status,
			Shell:     ms.State.Shell,
			CreatedAt: ms.State.CreatedAt,
			LastSeen:  ms.State.LastSeen,
			ClientURL: ms.State.ClientURL,
		})
	}
	return result
}

// GetSession returns a session by ID or short code
func (sm *SessionManager) GetSession(idOrCode string) (*SessionInfo, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	ms, ok := sm.sessions[idOrCode]
	if !ok {
		ms, ok = sm.byCode[idOrCode]
	}
	if !ok {
		return nil, fmt.Errorf("session not found: %s", idOrCode)
	}

	return &SessionInfo{
		ID:        ms.State.ID,
		ShortCode: ms.State.ShortCode,
		Status:    ms.State.Status,
		Shell:     ms.State.Shell,
		CreatedAt: ms.State.CreatedAt,
		LastSeen:  ms.State.LastSeen,
		ClientURL: ms.State.ClientURL,
	}, nil
}

// SaveSession saves session state to disk
func (sm *SessionManager) SaveSession(ms *ManagedSession) error {
	if ms.State.ShortCode == "" {
		return nil // Can't save without short code
	}
	return SaveSessionState(ms.State)
}

// CleanupIdleSessions removes sessions that have been disconnected/recovered for too long
func (sm *SessionManager) CleanupIdleSessions(idleTimeout time.Duration) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	toRemove := make([]string, 0)

	for id, ms := range sm.sessions {
		// Only cleanup disconnected or recovered sessions
		if ms.State.Status != StatusDisconnected && ms.State.Status != StatusRecovered {
			continue
		}

		// Check if session has been idle too long
		if now.Sub(ms.State.LastSeen) > idleTimeout {
			toRemove = append(toRemove, id)
		}
	}

	// Remove idle sessions
	for _, id := range toRemove {
		ms := sm.sessions[id]

		// Cancel server if running
		if ms.Cancel != nil {
			ms.Cancel()
		}

		// Close PTY for recovered sessions
		if ms.pty != nil && ms.Server == nil {
			ms.pty.Close()
		}

		// Remove from maps
		delete(sm.sessions, id)
		if ms.State.ShortCode != "" {
			delete(sm.byCode, ms.State.ShortCode)
		}

		// Remove state file
		RemoveSessionState(ms.State.ShortCode)

		fmt.Printf("Cleaned up idle session %s (code: %s, idle for: %v)\n",
			id, ms.State.ShortCode, now.Sub(ms.State.LastSeen).Round(time.Second))
	}

	return len(toRemove)
}

// LoadFromDisk loads existing sessions from disk and attempts to reconnect
func (sm *SessionManager) LoadFromDisk() error {
	states, err := LoadAllSessionStates()
	if err != nil {
		return err
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	recoveredCount := 0
	for _, state := range states {
		// Check if shell process is still running
		if !server.IsProcessRunning(state.ShellPID) {
			// Process dead, remove state file
			fmt.Printf("Session %s: shell process %d no longer running, cleaning up\n",
				state.ShortCode, state.ShellPID)
			RemoveSessionState(state.ShortCode)
			continue
		}

		// Attempt to reattach PTY
		pty, err := server.ReattachPTY(state.PTYPath, state.ShellPID)
		if err != nil {
			fmt.Printf("Session %s: failed to reattach PTY: %v, cleaning up\n",
				state.ShortCode, err)
			RemoveSessionState(state.ShortCode)
			continue
		}

		// Create a managed session with the reattached PTY
		// Note: No server is running - password required to resume signaling
		state.Status = StatusRecovered
		state.LastSeen = time.Now()

		ms := &ManagedSession{
			State:    state,
			Server:   nil, // No server until resumed with password
			Cancel:   nil,
			Password: "", // Password not persisted for security
			pty:      pty,
		}

		sm.sessions[state.ID] = ms
		if state.ShortCode != "" {
			sm.byCode[state.ShortCode] = ms
		}

		// Update state file
		SaveSessionState(state)

		fmt.Printf("✓ Recovered session %s (code: %s, PID: %d)\n",
			state.ID, state.ShortCode, state.ShellPID)
		recoveredCount++
	}

	if recoveredCount > 0 {
		fmt.Printf("Recovered %d session(s) from previous daemon\n", recoveredCount)
	}

	return nil
}
