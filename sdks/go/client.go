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
	maxResponseBytes  = 10 << 20
	requestTimeout    = 30 * time.Second
)

// Config holds the configuration for creating a new Client.
type Config struct {
	OrgID      string
	AgentID    string
	PrivateKey string // base64url-encoded 32-byte Ed25519 seed
	BaseURL    string // defaults to "https://api.elydora.com"
	TTLMs      int    // defaults to 30000
	MaxRetries int    // defaults to 3
	Token      string // API token for authenticated requests
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
		baseURL:    normalizeBaseURL(cfg.BaseURL),
		ttlMs:      ttlMs,
		maxRetries: maxRetries,
		token:      cfg.Token,
		httpClient: &http.Client{Timeout: requestTimeout},
	}, nil
}

// SetToken sets the API token used for authenticated API requests.
func (c *Client) SetToken(token string) {
	c.token = token
}

func normalizeBaseURL(baseURL string) string {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func newRequest(method, url string, body []byte, token, accept string) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("elydora: create request: %w", err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func marshalBody(body interface{}) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("elydora: marshal request body: %w", err)
	}
	return data, nil
}

func readBody(resp *http.Response, limit int64) ([]byte, error) {
	defer resp.Body.Close()
	var reader io.Reader = resp.Body
	if limit > 0 {
		reader = io.LimitReader(resp.Body, limit)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("elydora: read response body: %w", err)
	}
	return body, nil
}

func decodeSuccess(body []byte, result interface{}) error {
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("elydora: unmarshal response: %w", err)
	}
	return nil
}

func apiError(status int, body []byte) *ElydoraError {
	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Code != "" {
		return &ElydoraError{
			StatusCode: status,
			Code:       errResp.Error.Code,
			Message:    errResp.Error.Message,
			RequestID:  errResp.Error.RequestID,
			Details:    errResp.Error.Details,
		}
	}
	return &ElydoraError{StatusCode: status, Code: ErrorCodeInternalError, Message: string(body)}
}

func retryable(status int) bool {
	return status >= 500 || status == http.StatusTooManyRequests
}

// A lost response to a non-idempotent request must not be replayed.
func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

// doRequest executes an authenticated request; only idempotent methods retry.
func (c *Client) doRequest(method, path string, body interface{}, result interface{}) error {
	data, err := marshalBody(body)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * 100 * time.Millisecond)
		}
		req, err := newRequest(method, c.baseURL+path, data, c.token, "application/json")
		if err != nil {
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("elydora: http request: %w", err)
			if !idempotent(method) {
				return lastErr
			}
			continue
		}
		respBody, err := readBody(resp, maxResponseBytes)
		if err != nil {
			lastErr = err
			if !idempotent(method) {
				return lastErr
			}
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return decodeSuccess(respBody, result)
		}
		lastErr = apiError(resp.StatusCode, respBody)
		if !retryable(resp.StatusCode) || !idempotent(method) {
			return lastErr
		}
	}
	return lastErr
}

func (c *Client) doGet(path string, result interface{}) error {
	return c.doRequest(http.MethodGet, path, nil, result)
}

func (c *Client) doPost(path string, body interface{}, result interface{}) error {
	return c.doRequest(http.MethodPost, path, body, result)
}

// publicRequest sends one unauthenticated request and decodes accepted statuses.
func publicRequest(method, url string, body interface{}, result interface{}, accepted ...int) error {
	data, err := marshalBody(body)
	if err != nil {
		return err
	}
	req, err := newRequest(method, url, data, "", "application/json")
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: requestTimeout}).Do(req)
	if err != nil {
		return fmt.Errorf("elydora: http request: %w", err)
	}
	respBody, err := readBody(resp, maxResponseBytes)
	if err != nil {
		return err
	}
	for _, status := range accepted {
		if resp.StatusCode == status {
			return decodeSuccess(respBody, result)
		}
	}
	if len(accepted) == 0 && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return decodeSuccess(respBody, result)
	}
	return apiError(resp.StatusCode, respBody)
}
