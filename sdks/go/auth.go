package elydora

import "net/http"

// RegisterOption configures a registration request.
type RegisterOption func(*AuthRegisterRequest)

// WithDisplayName sets the display_name on a registration request.
func WithDisplayName(name string) RegisterOption {
	return func(req *AuthRegisterRequest) { req.DisplayName = name }
}

// WithOrgName sets the org_name on a registration request.
func WithOrgName(name string) RegisterOption {
	return func(req *AuthRegisterRequest) { req.OrgName = name }
}

// Register creates a new user and organization.
//
// Deprecated: use the Console sign-up flow; password registration is being phased out.
func Register(baseURL, email, password string, opts ...RegisterOption) (*AuthRegisterResponse, error) {
	req := &AuthRegisterRequest{Email: email, Password: password}
	for _, opt := range opts {
		opt(req)
	}
	var result AuthRegisterResponse
	if err := publicRequest(http.MethodPost, normalizeBaseURL(baseURL)+"/v1/auth/register", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Login authenticates a user and returns a session token.
//
// Deprecated: use the Console sign-in flow; password login is being phased out.
func Login(baseURL, email, password string) (*AuthLoginResponse, error) {
	req := &AuthLoginRequest{Email: email, Password: password}
	var result AuthLoginResponse
	if err := publicRequest(http.MethodPost, normalizeBaseURL(baseURL)+"/v1/auth/login", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
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

// RotateApiToken rotates the current API token; the old token stays valid for 24h.
func (c *Client) RotateApiToken() (*RotateApiTokenResponse, error) {
	var result RotateApiTokenResponse
	if err := c.doPost("/v1/auth/rotate", map[string]interface{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
