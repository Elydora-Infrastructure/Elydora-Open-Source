package elydora

import (
	"fmt"
	"net/http"
)

// ListWebhooks retrieves all registered webhooks for the organization.
func (c *Client) ListWebhooks() (*ListWebhooksResponse, error) {
	var result ListWebhooksResponse
	if err := c.doGet("/v1/webhooks", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RegisterWebhook registers a new webhook endpoint for the organization.
func (c *Client) RegisterWebhook(req *RegisterWebhookRequest) (*RegisterWebhookResponse, error) {
	var result RegisterWebhookResponse
	if err := c.doPost("/v1/webhooks", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteWebhook deletes a registered webhook by ID.
func (c *Client) DeleteWebhook(webhookID string) error {
	return c.doRequest(http.MethodDelete, fmt.Sprintf("/v1/webhooks/%s", webhookID), nil, nil)
}
