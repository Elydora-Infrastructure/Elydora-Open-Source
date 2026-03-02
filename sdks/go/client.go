package elydora

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL    = "https://api.elydora.com"
	defaultTTLMs      = 30000
	defaultMaxRetries = 3
)

// Config holds the configuration for creating a new Client.
type Config struct {
	OrgID      string
	AgentID    string
	PrivateKey string // base64url-encoded 32-byte Ed25519 seed
	BaseURL    string // defaults to "https://api.elydora.com"
	TTLMs      int    // defaults to 30000
	MaxRetries int    // defaults to 3
	Token      string // JWT token for authenticated requests
}

// Client is the Elydora SDK client.
type Client struct {
	orgID      string
	agentID    string
	privateKey string
	baseURL    string
	ttlMs      int
	maxRetries int
	token      string
	httpClient *http.Client
}

// NewClient creates a new Elydora Client with the given configuration.
func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("elydora: config must not be nil")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	ttlMs := cfg.TTLMs
	if ttlMs <= 0 {
		ttlMs = defaultTTLMs
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	return &Client{
		orgID:      cfg.OrgID,
		agentID:    cfg.AgentID,
		privateKey: cfg.PrivateKey,
		baseURL:    baseURL,
		ttlMs:      ttlMs,
		maxRetries: maxRetries,
		token:      cfg.Token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// doRequest executes an HTTP request with retries and error handling.
func (c *Client) doRequest(method, path string, body interface{}, result interface{}) error {
	u := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("elydora: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Reset body reader for retry
			if body != nil {
				data, _ := json.Marshal(body)
				bodyReader = bytes.NewReader(data)
			}
			time.Sleep(time.Duration(attempt*attempt) * 100 * time.Millisecond)
		}

		req, err := http.NewRequest(method, u, bodyReader)
		if err != nil {
			return fmt.Errorf("elydora: create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("elydora: http request: %w", err)
			continue
		}

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("elydora: read response body: %w", err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if result != nil && len(respBody) > 0 {
				if err := json.Unmarshal(respBody, result); err != nil {
					return fmt.Errorf("elydora: unmarshal response: %w", err)
				}
			}
			return nil
		}

		// Parse error response
		var errResp errorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error.Code != "" {
			apiErr := &ElydoraError{
				StatusCode: resp.StatusCode,
				Code:       errResp.Error.Code,
				Message:    errResp.Error.Message,
				RequestID:  errResp.Error.RequestID,
				Details:    errResp.Error.Details,
			}
			// Don't retry 4xx errors (except 429)
			if resp.StatusCode < 500 && resp.StatusCode != 429 {
				return apiErr
			}
			lastErr = apiErr
			continue
		}

		lastErr = &ElydoraError{
			StatusCode: resp.StatusCode,
			Code:       ErrorCodeInternalError,
			Message:    string(respBody),
		}
		if resp.StatusCode < 500 && resp.StatusCode != 429 {
			return lastErr
		}
	}
	return lastErr
}

// doGet performs a GET request.
func (c *Client) doGet(path string, result interface{}) error {
	return c.doRequest(http.MethodGet, path, nil, result)
}

// doPost performs a POST request.
func (c *Client) doPost(path string, body interface{}, result interface{}) error {
	return c.doRequest(http.MethodPost, path, body, result)
}

// SetToken sets the JWT token used for authenticated API requests.
func (c *Client) SetToken(token string) {
	c.token = token
}
