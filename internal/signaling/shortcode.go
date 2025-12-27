package signaling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ShortCodeClient handles short code based signaling via HTTP
type ShortCodeClient struct {
	relayURL   string
	clientURL  string
	code       string
	viewerCode string // Viewer session code (ends with V)
	sdp        string
	salt       string
	viewerSDP  string // SDP for viewer peer
	viewerKey  string // Base64-encoded viewer encryption key
	client     *http.Client
}

// SessionCreateResponse is the response from creating a session
type SessionCreateResponse struct {
	Code       string `json:"code"`
	ViewerCode string `json:"viewer_code,omitempty"` // Only if viewer session was created
	ExpiresIn  int    `json:"expires_in"`
	URL        string `json:"url,omitempty"`
}

// SessionGetResponse is the response from getting a session
type SessionGetResponse struct {
	SDP  string `json:"sdp"`
	Salt string `json:"salt"`
}

// AnswerPollResponse is the response from polling for an answer
type AnswerPollResponse struct {
	SDP    string `json:"sdp,omitempty"`
	Status string `json:"status,omitempty"`
}

// NewShortCodeClient creates a new short code client
func NewShortCodeClient(relayURL, clientURL string) *ShortCodeClient {
	return &ShortCodeClient{
		relayURL:  strings.TrimSuffix(relayURL, "/"),
		clientURL: strings.TrimSuffix(clientURL, "/"),
		client: &http.Client{
			Timeout: 35 * time.Second, // Slightly longer than long-poll timeout
		},
	}
}

// CreateSession creates a new session and returns a short code
func (c *ShortCodeClient) CreateSession(sdp, salt string) (string, error) {
	c.sdp = sdp
	c.salt = salt

	body, err := json.Marshal(map[string]string{
		"sdp":  sdp,
		"salt": salt,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.client.Post(c.relayURL+"/session", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("relay returned error: %s", string(bodyBytes))
	}

	var result SessionCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	c.code = result.Code
	return result.Code, nil
}

// GetCode returns the session code
func (c *ShortCodeClient) GetCode() string {
	return c.code
}

// GetViewerCode returns the viewer session code (ends with V)
func (c *ShortCodeClient) GetViewerCode() string {
	return c.viewerCode
}

// GetClientURL returns the URL for clients to connect
func (c *ShortCodeClient) GetClientURL() string {
	if c.clientURL != "" {
		return fmt.Sprintf("%s/?c=%s", c.clientURL, c.code)
	}
	return fmt.Sprintf("%s/?c=%s", GetClientURL(), c.code)
}

// GetViewerURL returns the URL for viewers to connect (read-only)
func (c *ShortCodeClient) GetViewerURL() string {
	if c.viewerCode == "" {
		return ""
	}
	if c.clientURL != "" {
		return fmt.Sprintf("%s/?c=%s", c.clientURL, c.viewerCode)
	}
	return fmt.Sprintf("%s/?c=%s", GetClientURL(), c.viewerCode)
}

// CreateSessionWithViewer creates a session with both control and viewer codes
func (c *ShortCodeClient) CreateSessionWithViewer(sdp, salt, viewerSDP, viewerKey string) (string, string, error) {
	c.sdp = sdp
	c.salt = salt
	c.viewerSDP = viewerSDP
	c.viewerKey = viewerKey

	body, err := json.Marshal(map[string]string{
		"sdp":        sdp,
		"salt":       salt,
		"viewer_sdp": viewerSDP,
		"viewer_key": viewerKey,
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.client.Post(c.relayURL+"/session", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("relay returned error: %s", string(bodyBytes))
	}

	var result SessionCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to decode response: %w", err)
	}

	c.code = result.Code
	c.viewerCode = result.ViewerCode
	return result.Code, result.ViewerCode, nil
}

// UpdateSession updates an existing session with a new offer (for reconnection)
func (c *ShortCodeClient) UpdateSession(sdp, salt string) error {
	c.sdp = sdp
	c.salt = salt

	body, err := json.Marshal(map[string]string{
		"sdp":  sdp,
		"salt": salt,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, c.relayURL+"/session/"+c.code, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("relay returned error: %s", string(bodyBytes))
	}

	return nil
}

// WaitForAnswer polls the relay for an answer with context support
func (c *ShortCodeClient) WaitForAnswer(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.WaitForAnswerWithContext(ctx)
}

// WaitForAnswerWithContext polls the relay for an answer with cancellation support
func (c *ShortCodeClient) WaitForAnswerWithContext(ctx context.Context) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.relayURL+"/session/"+c.code+"/answer", nil)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			// Retry on network errors
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return "", fmt.Errorf("session expired or not found")
		}

		var result AnswerPollResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}
		resp.Body.Close()

		if result.SDP != "" {
			return result.SDP, nil
		}

		// status == "waiting", continue polling
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// GetSession fetches session info by code (for client use)
func GetSession(relayURL, code string) (*SessionGetResponse, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(relayURL + "/session/" + strings.ToUpper(code))
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session not found")
	}

	var result SessionGetResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// SubmitAnswer submits an answer for a session (for client use)
func SubmitAnswer(relayURL, code, sdp string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	body, err := json.Marshal(map[string]string{"sdp": sdp})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := client.Post(relayURL+"/session/"+strings.ToUpper(code)+"/answer", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to submit answer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("relay returned error: %s", string(bodyBytes))
	}

	return nil
}

// WaitForViewerAnswerWithContext polls for a viewer's answer
func (c *ShortCodeClient) WaitForViewerAnswerWithContext(ctx context.Context) (string, error) {
	if c.viewerCode == "" {
		return "", fmt.Errorf("no viewer session created")
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.relayURL+"/session/"+c.viewerCode+"/answer", nil)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			// Retry on network errors
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return "", fmt.Errorf("viewer session expired or not found")
		}

		var result AnswerPollResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}
		resp.Body.Close()

		if result.SDP != "" {
			return result.SDP, nil
		}

		// status == "waiting", continue polling
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// GetViewerSession fetches viewer session info by code (for client use)
func GetViewerSession(relayURL, code string) (*ViewerSessionResponse, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(relayURL + "/session/" + strings.ToUpper(code))
	if err != nil {
		return nil, fmt.Errorf("failed to get viewer session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("viewer session not found")
	}

	var result ViewerSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
