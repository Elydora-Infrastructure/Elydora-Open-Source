package elydora

import (
	"fmt"
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
	req, err := newRequest(http.MethodGet, c.baseURL+fmt.Sprintf("/v1/exports/%s/download", exportID), nil, c.token, "")
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elydora: http request: %w", err)
	}
	body, err := readBody(resp, 0)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(resp.StatusCode, body)
	}
	return body, nil
}
