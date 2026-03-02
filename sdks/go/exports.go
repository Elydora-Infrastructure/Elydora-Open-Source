package elydora

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CreateExport creates a new compliance export job.
func (c *Client) CreateExport(params *CreateExportRequest) (*CreateExportResponse, error) {
	var result CreateExportResponse
	if err := c.doPost("/v1/exports", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListExports lists all exports for the organization.
func (c *Client) ListExports() (*ListExportsResponse, error) {
	var result ListExportsResponse
	if err := c.doGet("/v1/exports", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetExport retrieves a specific export by ID.
func (c *Client) GetExport(exportID string) (*GetExportResponse, error) {
	var result GetExportResponse
	if err := c.doGet(fmt.Sprintf("/v1/exports/%s", exportID), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DownloadExport downloads the raw file data for a completed export.
func (c *Client) DownloadExport(exportID string) ([]byte, error) {
	u := c.baseURL + fmt.Sprintf("/v1/exports/%s/download", exportID)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("elydora: create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elydora: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("elydora: read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp errorResponse
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Error.Code != "" {
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
			Message:    string(body),
		}
	}

	return body, nil
}
