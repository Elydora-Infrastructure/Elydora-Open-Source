package elydora

// GetJWKS retrieves the JSON Web Key Set from the Elydora API.
func (c *Client) GetJWKS() (*JWKSResponse, error) {
	var result JWKSResponse
	if err := c.doGet("/.well-known/elydora/jwks.json", &result); err != nil {
		return nil, err
	}
	return &result, nil
}
