package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/artpar/terminal-tunnel/internal/daemon"
)

// Client communicates with the daemon
type Client struct {
	socketPath string
}

// NewClient creates a new daemon client
func NewClient() *Client {
	return &Client{
		socketPath: daemon.GetSocketPath(),
	}
}

// call makes a JSON-RPC call to the daemon
func (c *Client) call(method string, params interface{}) (*daemon.Response, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("daemon not running (could not connect to %s)", c.socketPath)
	}
	defer conn.Close()

	// Set deadlines
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Build request
	req := daemon.Request{
		ID:     fmt.Sprintf("%d", time.Now().UnixNano()),
		Method: method,
	}

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		req.Params = data
	}

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := conn.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp daemon.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

// StartSession starts a new terminal session
func (c *Client) StartSession(password, shell string, noTURN, public, record bool) (*daemon.StartSessionResult, error) {
	params := daemon.StartSessionParams{
		Password: password,
		Shell:    shell,
		NoTURN:   noTURN,
		Public:   public,
		Record:   record,
	}

	resp, err := c.call(daemon.MethodSessionStart, params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result daemon.StartSessionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// StopSession stops a session by ID or short code
func (c *Client) StopSession(idOrCode string) error {
	params := daemon.StopSessionParams{
		ID: idOrCode,
	}

	resp, err := c.call(daemon.MethodSessionStop, params)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return resp.Error
	}

	return nil
}

// ListSessions lists all sessions
func (c *Client) ListSessions() ([]daemon.SessionInfo, error) {
	resp, err := c.call(daemon.MethodSessionList, nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result daemon.ListSessionsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return result.Sessions, nil
}

// Status gets daemon status
func (c *Client) Status() (*daemon.DaemonStatusResult, error) {
	resp, err := c.call(daemon.MethodDaemonStatus, nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result daemon.DaemonStatusResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// Shutdown requests daemon shutdown
func (c *Client) Shutdown() (*daemon.ShutdownResult, error) {
	resp, err := c.call(daemon.MethodDaemonStop, nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result daemon.ShutdownResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// IsDaemonRunning checks if daemon is running
func (c *Client) IsDaemonRunning() bool {
	_, err := c.Status()
	return err == nil
}
