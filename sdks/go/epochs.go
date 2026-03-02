package elydora

import "fmt"

// ListEpochs lists all epochs for the organization.
func (c *Client) ListEpochs() (*ListEpochsResponse, error) {
	var result ListEpochsResponse
	if err := c.doGet("/v1/epochs", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetEpoch retrieves a specific epoch by ID.
func (c *Client) GetEpoch(epochID string) (*GetEpochResponse, error) {
	var result GetEpochResponse
	if err := c.doGet(fmt.Sprintf("/v1/epochs/%s", epochID), &result); err != nil {
		return nil, err
	}
	return &result, nil
}
