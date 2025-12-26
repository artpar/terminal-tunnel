package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Default timeouts
const (
	DefaultIdleTimeout    = 30 * time.Minute // Cleanup disconnected sessions after 30 mins
	DefaultCleanupInterval = 1 * time.Minute  // Check for idle sessions every minute
)

// Daemon represents the terminal-tunnel daemon
type Daemon struct {
	statePath       string
	listener        net.Listener
	sessions        *SessionManager
	startTime       time.Time
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	shutdownCh      chan struct{}
	idleTimeout     time.Duration // How long a disconnected session can remain idle
	cleanupInterval time.Duration // How often to check for idle sessions
}

// NewDaemon creates a new daemon instance
func NewDaemon() (*Daemon, error) {
	if err := EnsureStateDir(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	d := &Daemon{
		statePath:       GetStateDir(),
		startTime:       time.Now(),
		ctx:             ctx,
		cancel:          cancel,
		shutdownCh:      make(chan struct{}),
		idleTimeout:     DefaultIdleTimeout,
		cleanupInterval: DefaultCleanupInterval,
	}

	d.sessions = NewSessionManager(d)

	return d, nil
}

// Start starts the daemon
func (d *Daemon) Start() error {
	// Check if already running
	if running, pid := IsDaemonRunning(); running {
		return fmt.Errorf("daemon already running (PID %d)", pid)
	}

	// Write PID file
	if err := WritePID(); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Remove stale socket
	socketPath := GetSocketPath()
	os.Remove(socketPath)

	// Create Unix socket listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		RemovePID()
		return fmt.Errorf("failed to create socket: %w", err)
	}
	d.listener = listener

	// Set socket permissions
	if err := os.Chmod(socketPath, 0600); err != nil {
		d.listener.Close()
		RemovePID()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	// Load existing sessions from disk
	if err := d.sessions.LoadFromDisk(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load sessions: %v\n", err)
	}

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
			fmt.Println("\nReceived shutdown signal")
			d.Shutdown()
		case <-d.ctx.Done():
		}
	}()

	// Start idle session cleanup goroutine
	go d.cleanupLoop()

	fmt.Printf("Daemon started (PID %d)\n", os.Getpid())
	fmt.Printf("Socket: %s\n", socketPath)

	// Accept connections
	d.acceptConnections()

	return nil
}

// acceptConnections accepts and handles incoming connections
func (d *Daemon) acceptConnections() {
	for {
		conn, err := d.listener.Accept()
		if err != nil {
			select {
			case <-d.ctx.Done():
				return
			default:
				fmt.Fprintf(os.Stderr, "Accept error: %v\n", err)
				continue
			}
		}

		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.handleConnection(conn)
		}()
	}
}

// handleConnection handles a single client connection
func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}

	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		resp := NewErrorResponse("", ErrCodeInvalidParams, "invalid request format")
		d.sendResponse(conn, resp)
		return
	}

	resp := d.handleRequest(&req)
	d.sendResponse(conn, resp)
}

// handleRequest processes a request and returns a response
func (d *Daemon) handleRequest(req *Request) *Response {
	switch req.Method {
	case MethodSessionStart:
		return d.handleSessionStart(req)
	case MethodSessionStop:
		return d.handleSessionStop(req)
	case MethodSessionList:
		return d.handleSessionList(req)
	case MethodDaemonStatus:
		return d.handleDaemonStatus(req)
	case MethodDaemonStop:
		return d.handleDaemonShutdown(req)
	default:
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "unknown method: "+req.Method)
	}
}

// handleSessionStart handles session.start requests
func (d *Daemon) handleSessionStart(req *Request) *Response {
	var params StartSessionParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "invalid params: "+err.Error())
		}
	}

	info, err := d.sessions.StartSession(params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeSessionCreateFailed, err.Error())
	}

	result := StartSessionResult{
		ID:        info.ID,
		ShortCode: info.ShortCode,
		Password:  info.Password,
		ClientURL: info.ClientURL,
		Status:    string(info.Status),
	}

	resp, err := NewSuccessResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeInternalError, err.Error())
	}
	return resp
}

// handleSessionStop handles session.stop requests
func (d *Daemon) handleSessionStop(req *Request) *Response {
	var params StopSessionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "invalid params: "+err.Error())
	}

	if err := d.sessions.StopSession(params.ID); err != nil {
		return NewErrorResponse(req.ID, ErrCodeSessionNotFound, err.Error())
	}

	result := StopSessionResult{
		Success: true,
		Message: "Session stopped",
	}

	resp, err := NewSuccessResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeInternalError, err.Error())
	}
	return resp
}

// handleSessionList handles session.list requests
func (d *Daemon) handleSessionList(req *Request) *Response {
	sessions := d.sessions.ListSessions()

	result := ListSessionsResult{
		Sessions: sessions,
	}

	resp, err := NewSuccessResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeInternalError, err.Error())
	}
	return resp
}

// handleDaemonStatus handles daemon.status requests
func (d *Daemon) handleDaemonStatus(req *Request) *Response {
	sessions := d.sessions.ListSessions()
	activeCount := 0
	for _, s := range sessions {
		if s.Status == StatusConnected {
			activeCount++
		}
	}

	uptime := time.Since(d.startTime).Round(time.Second).String()

	result := DaemonStatusResult{
		Running:      true,
		PID:          os.Getpid(),
		Uptime:       uptime,
		SessionCount: len(sessions),
		ActiveCount:  activeCount,
	}

	resp, err := NewSuccessResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeInternalError, err.Error())
	}
	return resp
}

// handleDaemonShutdown handles daemon.shutdown requests
func (d *Daemon) handleDaemonShutdown(req *Request) *Response {
	sessionCount := len(d.sessions.ListSessions())

	result := ShutdownResult{
		Success:         true,
		SessionsStopped: sessionCount,
	}

	resp, err := NewSuccessResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeInternalError, err.Error())
	}

	// Trigger shutdown after sending response
	go func() {
		time.Sleep(100 * time.Millisecond)
		d.Shutdown()
	}()

	return resp
}

// sendResponse sends a response to the client
func (d *Daemon) sendResponse(conn net.Conn, resp *Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	conn.Write(append(data, '\n'))
}

// Shutdown gracefully shuts down the daemon
func (d *Daemon) Shutdown() {
	select {
	case <-d.shutdownCh:
		return // Already shutting down
	default:
		close(d.shutdownCh)
	}

	fmt.Println("Shutting down daemon...")

	// Stop all sessions
	d.sessions.StopAllSessions()

	// Cancel context
	d.cancel()

	// Close listener
	if d.listener != nil {
		d.listener.Close()
	}

	// Wait for connections to finish
	d.wg.Wait()

	// Cleanup
	Cleanup()

	fmt.Println("Daemon stopped")
}

// GetContext returns the daemon's context
func (d *Daemon) GetContext() context.Context {
	return d.ctx
}

// cleanupLoop periodically checks for and removes idle sessions
func (d *Daemon) cleanupLoop() {
	ticker := time.NewTicker(d.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cleaned := d.sessions.CleanupIdleSessions(d.idleTimeout)
			if cleaned > 0 {
				fmt.Printf("Cleaned up %d idle session(s)\n", cleaned)
			}
		case <-d.ctx.Done():
			return
		}
	}
}

// GetIdleTimeout returns the configured idle timeout
func (d *Daemon) GetIdleTimeout() time.Duration {
	return d.idleTimeout
}
