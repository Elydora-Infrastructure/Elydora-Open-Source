package elydora

type RegisterAgentKeyInput struct {
	KID       string `json:"kid"`
	PublicKey string `json:"public_key"`
	Algorithm string `json:"algorithm"`
}

type RegisterAgentRequest struct {
	AgentID           string                  `json:"agent_id"`
	DisplayName       string                  `json:"display_name,omitempty"`
	ResponsibleEntity string                  `json:"responsible_entity,omitempty"`
	IntegrationType   IntegrationType         `json:"integration_type"`
	Keys              []RegisterAgentKeyInput `json:"keys"`
}

type RegisterAgentResponse struct {
	Agent Agent      `json:"agent"`
	Keys  []AgentKey `json:"keys"`
}

type GetAgentResponse struct {
	Agent Agent      `json:"agent"`
	Keys  []AgentKey `json:"keys"`
}

type FreezeAgentRequest struct {
	Reason string `json:"reason"`
}

type FreezeAgentResponse struct {
	Agent          Agent       `json:"agent"`
	PreviousStatus AgentStatus `json:"previous_status"`
}

type RevokeAgentRequest struct {
	KID    string `json:"kid"`
	Reason string `json:"reason"`
}

type UnfreezeAgentRequest struct {
	Reason string `json:"reason"`
}

type UnfreezeAgentResponse struct {
	Agent          Agent       `json:"agent"`
	PreviousStatus AgentStatus `json:"previous_status"`
}

type UpdateAgentRequest struct {
	IntegrationType IntegrationType `json:"integration_type"`
}

type UpdateAgentResponse struct {
	Agent Agent `json:"agent"`
}

type ListAgentsResponse struct {
	Agents []Agent `json:"agents"`
}

type DeleteAgentResponse struct {
	Deleted bool `json:"deleted"`
}

type GetMeResponse struct {
	User                AuthMeUser          `json:"user"`
	CurrentOrganization CurrentOrganization `json:"current_organization"`
}

type IssueApiTokenRequest struct {
	TTLSeconds *int `json:"ttl_seconds"`
}

type IssueApiTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt *int64 `json:"expires_at"`
	TokenID   string `json:"token_id"`
}

type RotateApiTokenResponse struct {
	Token                   string `json:"token"`
	ExpiresAt               *int64 `json:"expires_at"`
	TokenID                 string `json:"token_id"`
	PreviousTokenGraceUntil int64  `json:"previous_token_grace_until"`
}

type HealthResponse struct {
	Status          string            `json:"status"`
	Version         string            `json:"version"`
	ProtocolVersion string            `json:"protocol_version"`
	Capabilities    map[string]string `json:"capabilities"`
	Timestamp       int64             `json:"timestamp"`
}

type SubmitOperationResponse struct {
	Receipt EAR `json:"receipt"`
}

type GetOperationResponse struct {
	Operation Operation   `json:"operation"`
	Receipt   *Receipt    `json:"receipt,omitempty"`
	Payload   interface{} `json:"payload,omitempty"`
}

type VerifyOperationChecks struct {
	Signature bool  `json:"signature"`
	Chain     bool  `json:"chain"`
	Receipt   bool  `json:"receipt"`
	Merkle    *bool `json:"merkle,omitempty"`
}

type VerifyOperationResponse struct {
	Valid  bool                  `json:"valid"`
	Checks VerifyOperationChecks `json:"checks"`
	Errors []string              `json:"errors,omitempty"`
}

type AuditQueryRequest struct {
	OrgID         string `json:"org_id,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	OperationType string `json:"operation_type,omitempty"`
	StartTime     *int64 `json:"start_time,omitempty"`
	EndTime       *int64 `json:"end_time,omitempty"`
	Cursor        string `json:"cursor,omitempty"`
	Limit         *int   `json:"limit,omitempty"`
}

type AuditQueryResponse struct {
	Operations []Operation `json:"operations"`
	Cursor     string      `json:"cursor,omitempty"`
	TotalCount int64       `json:"total_count"`
}

type GetEpochResponse struct {
	Epoch  Epoch        `json:"epoch"`
	Anchor *EpochAnchor `json:"anchor,omitempty"`
}

type EpochAnchor struct {
	TSAToken   string `json:"tsa_token,omitempty"`
	TSAUrl     string `json:"tsa_url,omitempty"`
	AnchoredAt *int64 `json:"anchored_at,omitempty"`
}

type ListEpochsResponse struct {
	Epochs []Epoch `json:"epochs"`
}

type CreateExportRequest struct {
	StartTime     int64        `json:"start_time"`
	EndTime       int64        `json:"end_time"`
	AgentID       string       `json:"agent_id,omitempty"`
	OperationType string       `json:"operation_type,omitempty"`
	Format        ExportFormat `json:"format"`
}

type CreateExportResponse struct {
	Export Export `json:"export"`
}

type GetExportResponse struct {
	Export      Export `json:"export"`
	DownloadURL string `json:"download_url,omitempty"`
}

type ListExportsResponse struct {
	Exports []Export `json:"exports"`
}

type JWK struct {
	KTY string `json:"kty"`
	CRV string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	KID string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
}

type JWKSResponse struct {
	Keys []JWK `json:"keys"`
}

type AuthRegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	OrgName     string `json:"org_name,omitempty"`
}

type AuthRegisterResponse struct {
	User         User         `json:"user"`
	Organization Organization `json:"organization"`
	Token        string       `json:"token"`
}

type AuthLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthLoginResponse struct {
	User  User   `json:"user"`
	Token string `json:"token"`
}

type ListWebhooksResponse struct {
	Webhooks []Webhook `json:"webhooks"`
}

type RegisterWebhookRequest struct {
	EndpointURL string   `json:"endpoint_url"`
	Events      []string `json:"events"`
	Secret      string   `json:"secret"`
}

type RegisterWebhookResponse struct {
	Webhook Webhook `json:"webhook"`
}

type ListMembersResponse struct {
	Members []Member `json:"members"`
}

type ListAdminEventsResponse struct {
	Events []AdminEvent `json:"events"`
}

type DeepHealthResponse struct {
	Status          string                 `json:"status"`
	Version         string                 `json:"version"`
	ProtocolVersion string                 `json:"protocol_version"`
	Capabilities    map[string]string      `json:"capabilities"`
	Timestamp       int64                  `json:"timestamp"`
	Dependencies    DeepHealthDependencies `json:"dependencies"`
}
