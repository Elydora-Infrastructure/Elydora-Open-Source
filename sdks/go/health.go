package elydora

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Health checks the health of the Elydora API. This is a public endpoint
// that does not require authentication.
func Health(baseURL string) (*HealthResponse, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/health", nil)
	if err != nil {
		return nil, fmt.Errorf("elydora: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elydora: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("elydora: read response body: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var result HealthResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("elydora: unmarshal response: %w", err)
		}
		return &result, nil
	}

	var errResp errorResponse
	if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error.Code != "" {
		return nil, &ElydoraError{
			StatusCode: resp.StatusCode,
			Code:       errResp.Error.Code,
			Message:    errResp.Error.Message,
			RequestID:  errResp.Error.RequestID,
			Details:    errResp.Error.Details,
		}
	}

	return nil, &ElydoraError{
		StatusCode: resp.StatusCode,
		Code:       ErrorCodeInternalError,
		Message:    string(respBody),
	}
}
