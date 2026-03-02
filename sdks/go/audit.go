package elydora

// QueryAudit queries the audit log with the given parameters.
func (c *Client) QueryAudit(params *AuditQueryRequest) (*AuditQueryResponse, error) {
	var result AuditQueryResponse
	if err := c.doPost("/v1/audit/query", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
