package elydora

import (
	"fmt"
	"net/http"
)

// RegisterAgent registers a new AI agent within the organization.
func (c *Client) RegisterAgent(req *RegisterAgentRequest) (*RegisterAgentResponse, error) {
	var result RegisterAgentResponse
	if err := c.doPost("/v1/agents/register", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAgent retrieves an agent by ID.
func (c *Client) GetAgent(agentID string) (*GetAgentResponse, error) {
	var result GetAgentResponse
	if err := c.doGet(fmt.Sprintf("/v1/agents/%s", agentID), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// FreezeAgent freezes an agent, preventing it from submitting operations.
func (c *Client) FreezeAgent(agentID, reason string) error {
	return c.doPost(fmt.Sprintf("/v1/agents/%s/freeze", agentID), &FreezeAgentRequest{
		Reason: reason,
	}, nil)
}

// RevokeKey revokes a specific key for an agent.
func (c *Client) RevokeKey(agentID, kid, reason string) error {
	return c.doPost(fmt.Sprintf("/v1/agents/%s/revoke", agentID), &RevokeAgentRequest{
		KID:    kid,
		Reason: reason,
	}, nil)
}

// ListAgents retrieves all agents for the organization.
func (c *Client) ListAgents() (*ListAgentsResponse, error) {
	var result ListAgentsResponse
	if err := c.doGet("/v1/agents", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UnfreezeAgent unfreezes a previously frozen agent.
func (c *Client) UnfreezeAgent(agentID, reason string) error {
	return c.doPost(fmt.Sprintf("/v1/agents/%s/unfreeze", agentID), &UnfreezeAgentRequest{
		Reason: reason,
	}, nil)
}

// DeleteAgent permanently deletes an agent and all associated data.
func (c *Client) DeleteAgent(agentID string) (*DeleteAgentResponse, error) {
	var result DeleteAgentResponse
	if err := c.doRequest(http.MethodDelete, fmt.Sprintf("/v1/agents/%s", agentID), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
