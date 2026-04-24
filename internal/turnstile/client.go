package turnstile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Verifier verifies Turnstile tokens against Siteverify.
type Verifier interface {
	Verify(ctx context.Context, req VerifyRequest) (*VerifyResponse, error)
}

// VerifyRequest contains Turnstile verification inputs.
type VerifyRequest struct {
	Token    string
	RemoteIP string
}

// VerifyResponse mirrors Turnstile Siteverify response fields.
type VerifyResponse struct {
	Success     bool     `json:"success"`
	ChallengeTS string   `json:"challenge_ts,omitempty"`
	Hostname    string   `json:"hostname,omitempty"`
	ErrorCodes  []string `json:"error-codes,omitempty"`
	Action      string   `json:"action,omitempty"`
	CData       string   `json:"cdata,omitempty"`
}

// Client is a Turnstile Siteverify API client.
type Client struct {
	httpClient *http.Client
	secretKey  string
	verifyURL  string
}

// NewClient creates a Turnstile Siteverify client.
func NewClient(httpClient *http.Client, secretKey, verifyURL string) (*Client, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("http client is required")
	}
	if strings.TrimSpace(secretKey) == "" {
		return nil, fmt.Errorf("turnstile secret key is required")
	}
	if strings.TrimSpace(verifyURL) == "" {
		return nil, fmt.Errorf("turnstile verify url is required")
	}
	return &Client{
		httpClient: httpClient,
		secretKey:  strings.TrimSpace(secretKey),
		verifyURL:  strings.TrimSpace(verifyURL),
	}, nil
}

// Verify validates a token through Cloudflare Turnstile Siteverify API.
func (c *Client) Verify(ctx context.Context, req VerifyRequest) (*VerifyResponse, error) {
	if strings.TrimSpace(req.Token) == "" {
		return nil, fmt.Errorf("token is required")
	}

	values := url.Values{}
	values.Set("secret", c.secretKey)
	values.Set("response", req.Token)
	if strings.TrimSpace(req.RemoteIP) != "" {
		values.Set("remoteip", strings.TrimSpace(req.RemoteIP))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.verifyURL, bytes.NewBufferString(values.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("siteverify request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read siteverify response: %w", err)
	}

	var parsed VerifyResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse siteverify response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("siteverify returned status %d", resp.StatusCode)
	}

	return &parsed, nil
}
