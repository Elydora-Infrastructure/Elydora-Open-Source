package elydora

import "fmt"

// ElydoraError represents an error returned by the Elydora API.
type ElydoraError struct {
	StatusCode int
	Code       ErrorCode              `json:"code"`
	Message    string                 `json:"message"`
	RequestID  string                 `json:"request_id"`
	Details    map[string]interface{} `json:"details,omitempty"`
}

func (e *ElydoraError) Error() string {
	return fmt.Sprintf("elydora: %s (code=%s, status=%d, request_id=%s)", e.Message, e.Code, e.StatusCode, e.RequestID)
}

// errorResponse is the wire format for API error responses.
type errorResponse struct {
	Error struct {
		Code      ErrorCode              `json:"code"`
		Message   string                 `json:"message"`
		RequestID string                 `json:"request_id"`
		Details   map[string]interface{} `json:"details,omitempty"`
	} `json:"error"`
}
