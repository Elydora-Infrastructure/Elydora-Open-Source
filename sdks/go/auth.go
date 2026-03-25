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

// RegisterOption is a functional option for the Register function.
type RegisterOption func(*AuthRegisterRequest)

// WithDisplayName sets the display_name on a registration request.
func WithDisplayName(name string) RegisterOption {
	return func(req *AuthRegisterRequest) {
		req.DisplayName = name
	}
}

// WithOrgName sets the org_name on a registration request.
func WithOrgName(name string) RegisterOption {
	return func(req *AuthRegisterRequest) {
		req.OrgName = name
	}
}

// Register creates a new user and organization.
//
// Deprecated: Use Better Auth endpoints directly. See docs.
func Register(baseURL, email, password string, opts ...RegisterOption) (*AuthRegisterResponse, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	req := &AuthRegisterRequest{
		Email:    email,
		Password: password,
	}
	for _, opt := range opts {
		opt(req)
	}

	var result AuthRegisterResponse
	if err := authPost(baseURL, "/v1/auth/register", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Login authenticates a user and returns a session token.
//
// Deprecated: Use Better Auth endpoints directly. See docs.
func Login(baseURL, email, password string) (*AuthLoginResponse, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	req := &AuthLoginRequest{
		Email:    email,
		Password: password,
	}

	var result AuthLoginResponse
	if err := authPost(baseURL, "/v1/auth/login", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// authPost is a helper for unauthenticated POST requests used by Register and Login.
func authPost(baseURL, path string, body interface{}, result interface{}) error {
	u := baseURL + path

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("elydora: marshal request body: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("elydora: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("elydora: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fmt.Errorf("elydora: read response body: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if result != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, result); err != nil {
				return fmt.Errorf("elydora: unmarshal response: %w", err)
			}
		}
		return nil
	}

	var errResp errorResponse
	if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error.Code != "" {
		return &ElydoraError{
			StatusCode: resp.StatusCode,
			Code:       errResp.Error.Code,
			Message:    errResp.Error.Message,
			RequestID:  errResp.Error.RequestID,
			Details:    errResp.Error.Details,
		}
	}

	return &ElydoraError{
		StatusCode: resp.StatusCode,
		Code:       ErrorCodeInternalError,
		Message:    string(respBody),
	}
}

// GetMe retrieves the current authenticated user's profile.
func (c *Client) GetMe() (*GetMeResponse, error) {
	var result GetMeResponse
	if err := c.doGet("/v1/auth/me", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// IssueApiToken creates a new API token with an optional TTL.
func (c *Client) IssueApiToken(req *IssueApiTokenRequest) (*IssueApiTokenResponse, error) {
	var result IssueApiTokenResponse
	if err := c.doPost("/v1/auth/token", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
