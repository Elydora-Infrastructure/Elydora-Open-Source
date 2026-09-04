package elydora

import "fmt"

// GenesisChainHash is the base64url encoding of 32 zero bytes.
const GenesisChainHash = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// CreateOperation builds and signs an EOR locally.
func (c *Client) CreateOperation(params *CreateOperationParams) (*EOR, error) {
	if params == nil {
		return nil, fmt.Errorf("elydora: params must not be nil")
	}

	operationID, err := generateUUIDv7()
	if err != nil {
		return nil, fmt.Errorf("elydora: generate operation id: %w", err)
	}

	nonce, err := generateNonce()
	if err != nil {
		return nil, fmt.Errorf("elydora: generate nonce: %w", err)
	}

	issuedAt := nowUnixMs()

	payloadHash, err := computePayloadHash(params.Payload)
	if err != nil {
		return nil, fmt.Errorf("elydora: compute payload hash: %w", err)
	}

	prevChainHash := params.PrevChainHash
	if prevChainHash == "" {
		prevChainHash = GenesisChainHash
	}

	kid := params.KID
	if kid == "" && c.agentID != "" {
		kid = c.agentID + "-key-1"
	}

	eor := &EOR{
		OpVersion:      "1.0",
		OperationID:    operationID,
		OrgID:          c.orgID,
		AgentID:        c.agentID,
		IssuedAt:       issuedAt,
		TTLMs:          int64(c.ttlMs),
		Nonce:          nonce,
		OperationType:  params.OperationType,
		Subject:        params.Subject,
		Action:         params.Action,
		Payload:        params.Payload,
		PayloadHash:    payloadHash,
		PrevChainHash:  prevChainHash,
		AgentPubkeyKID: kid,
	}

	signature, err := signEOR(eor, c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("elydora: sign eor: %w", err)
	}
	eor.Signature = signature

	return eor, nil
}

// SubmitOperation submits a signed EOR to the Elydora API.
func (c *Client) SubmitOperation(eor *EOR) (*SubmitOperationResponse, error) {
	var result SubmitOperationResponse
	if err := c.doPost("/v1/operations", eor, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetOperation retrieves an operation by ID.
func (c *Client) GetOperation(operationID string) (*GetOperationResponse, error) {
	var result GetOperationResponse
	if err := c.doGet(fmt.Sprintf("/v1/operations/%s", operationID), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// VerifyOperation verifies the integrity of an operation.
func (c *Client) VerifyOperation(operationID string) (*VerifyOperationResponse, error) {
	var result VerifyOperationResponse
	if err := c.doPost(
		fmt.Sprintf("/v1/operations/%s/verify", operationID),
		map[string]interface{}{},
		&result,
	); err != nil {
		return nil, err
	}
	return &result, nil
}
