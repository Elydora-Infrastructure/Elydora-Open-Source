package elydora

import "net/http"

// Health checks the public health endpoint.
func Health(baseURL string) (*HealthResponse, error) {
	var result HealthResponse
	if err := publicRequest(http.MethodGet, normalizeBaseURL(baseURL)+"/v1/health", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeepHealth checks backend dependencies; 503 reports a degraded API.
func DeepHealth(baseURL string) (*DeepHealthResponse, error) {
	var result DeepHealthResponse
	err := publicRequest(
		http.MethodGet,
		normalizeBaseURL(baseURL)+"/v1/health/deep",
		nil,
		&result,
		http.StatusOK,
		http.StatusServiceUnavailable,
	)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
