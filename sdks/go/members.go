package elydora

// ListMembers retrieves all members of the organization.
func (c *Client) ListMembers() (*ListMembersResponse, error) {
	var result ListMembersResponse
	if err := c.doGet("/v1/members", &result); err != nil {
		return nil, err
	}
	return &result, nil
}
