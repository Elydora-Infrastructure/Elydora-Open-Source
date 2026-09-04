package elydora

import "fmt"

// ListAdminEvents retrieves recent administrative events; limit 0 uses the server default.
func (c *Client) ListAdminEvents(limit int) (*ListAdminEventsResponse, error) {
	path := "/v1/admin/events"
	if limit > 0 {
		path = fmt.Sprintf("%s?limit=%d", path, limit)
	}
	var result ListAdminEventsResponse
	if err := c.doGet(path, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
