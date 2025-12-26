package daemon

import (
	"encoding/json"
	"time"
)

// RPC Methods
const (
	MethodSessionStart = "session.start"
	MethodSessionStop  = "session.stop"
	MethodSessionList  = "session.list"
	MethodDaemonStatus = "daemon.status"
	MethodDaemonStop   = "daemon.shutdown"
)

// Error codes
const (
	ErrCodeDaemonNotRunning    = 1001
	ErrCodeSessionNotFound     = 1002
	ErrCodeSessionCreateFailed = 1003
	ErrCodeInvalidParams       = 1004
	ErrCodeInternalError       = 1005
)

// Request represents a JSON-RPC request from CLI to daemon
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC response from daemon to CLI
type Response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// RPCError represents an error in the response
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return e.Message
}

// NewErrorResponse creates an error response
func NewErrorResponse(id string, code int, message string) *Response {
	return &Response{
		ID: id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}

// NewSuccessResponse creates a success response
func NewSuccessResponse(id string, result interface{}) (*Response, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &Response{
		ID:     id,
		Result: data,
	}, nil
}

// --- Request Parameters ---

// StartSessionParams represents parameters for session.start
type StartSessionParams struct {
	Password string `json:"password,omitempty"` // Auto-generated if empty
	Shell    string `json:"shell,omitempty"`    // Default to $SHELL
	NoTURN   bool   `json:"no_turn,omitempty"`  // Disable TURN relay (P2P only)
}

// StopSessionParams represents parameters for session.stop
type StopSessionParams struct {
	ID string `json:"id"` // Session ID or short code
}

// --- Response Results ---

// SessionStatus represents the status of a session
type SessionStatus string

const (
	StatusWaiting      SessionStatus = "waiting"
	StatusConnected    SessionStatus = "connected"
	StatusDisconnected SessionStatus = "disconnected"
	StatusRecovered    SessionStatus = "recovered" // Shell alive but no signaling after daemon restart
)

// SessionInfo represents information about a session
type SessionInfo struct {
	ID        string        `json:"id"`
	ShortCode string        `json:"short_code"`
	Status    SessionStatus `json:"status"`
	Shell     string        `json:"shell"`
	CreatedAt time.Time     `json:"created_at"`
	LastSeen  time.Time     `json:"last_seen"`
	ClientURL string        `json:"client_url"`
}

// StartSessionResult represents the result of session.start
type StartSessionResult struct {
	ID        string `json:"id"`
	ShortCode string `json:"short_code"`
	Password  string `json:"password"` // Return generated password
	ClientURL string `json:"client_url"`
	Status    string `json:"status"`
}

// StopSessionResult represents the result of session.stop
type StopSessionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// ListSessionsResult represents the result of session.list
type ListSessionsResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

// DaemonStatusResult represents the result of daemon.status
type DaemonStatusResult struct {
	Running      bool   `json:"running"`
	PID          int    `json:"pid"`
	Uptime       string `json:"uptime"`
	SessionCount int    `json:"session_count"`
	ActiveCount  int    `json:"active_count"` // Currently connected
}

// ShutdownResult represents the result of daemon.shutdown
type ShutdownResult struct {
	Success          bool `json:"success"`
	SessionsStopped  int  `json:"sessions_stopped"`
}
