package elydora

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

type AgentStatus string

const (
	AgentStatusActive  AgentStatus = "active"
	AgentStatusFrozen  AgentStatus = "frozen"
	AgentStatusRevoked AgentStatus = "revoked"
)

type KeyStatus string

const (
	KeyStatusActive  KeyStatus = "active"
	KeyStatusRetired KeyStatus = "retired"
	KeyStatusRevoked KeyStatus = "revoked"
)

type ExportStatus string

const (
	ExportStatusQueued  ExportStatus = "queued"
	ExportStatusRunning ExportStatus = "running"
	ExportStatusDone    ExportStatus = "done"
	ExportStatusFailed  ExportStatus = "failed"
)

type RbacRole string

const (
	RbacRoleOrgOwner              RbacRole = "org_owner"
	RbacRoleSecurityAdmin         RbacRole = "security_admin"
	RbacRoleComplianceAuditor     RbacRole = "compliance_auditor"
	RbacRoleReadonlyInvestigator  RbacRole = "readonly_investigator"
	RbacRoleIntegrationEngineer   RbacRole = "integration_engineer"
)

type ErrorCode string

const (
	ErrorCodeInvalidSignature  ErrorCode = "INVALID_SIGNATURE"
	ErrorCodeUnknownAgent      ErrorCode = "UNKNOWN_AGENT"
	ErrorCodeKeyRevoked        ErrorCode = "KEY_REVOKED"
	ErrorCodeAgentFrozen       ErrorCode = "AGENT_FROZEN"
	ErrorCodeTTLExpired        ErrorCode = "TTL_EXPIRED"
	ErrorCodeReplayDetected    ErrorCode = "REPLAY_DETECTED"
	ErrorCodePrevHashMismatch  ErrorCode = "PREV_HASH_MISMATCH"
	ErrorCodePayloadTooLarge   ErrorCode = "PAYLOAD_TOO_LARGE"
	ErrorCodeRateLimited       ErrorCode = "RATE_LIMITED"
	ErrorCodeInternalError     ErrorCode = "INTERNAL_ERROR"
	ErrorCodeUnauthorized      ErrorCode = "UNAUTHORIZED"
	ErrorCodeForbidden         ErrorCode = "FORBIDDEN"
	ErrorCodeNotFound          ErrorCode = "NOT_FOUND"
	ErrorCodeValidationError   ErrorCode = "VALIDATION_ERROR"
)

type ExportFormat string

const (
	ExportFormatJSON ExportFormat = "json"
	ExportFormatPDF  ExportFormat = "pdf"
)

// ---------------------------------------------------------------------------
// Entity types
// ---------------------------------------------------------------------------

type Agent struct {
	AgentID           string      `json:"agent_id"`
	OrgID             string      `json:"org_id"`
	DisplayName       string      `json:"display_name"`
	ResponsibleEntity string      `json:"responsible_entity"`
	Status            AgentStatus `json:"status"`
	CreatedAt         int64       `json:"created_at"`
	UpdatedAt         int64       `json:"updated_at"`
}

type AgentKey struct {
	KID       string    `json:"kid"`
	AgentID   string    `json:"agent_id"`
	PublicKey string    `json:"public_key"`
	Algorithm string    `json:"algorithm"`
	Status    KeyStatus `json:"status"`
	CreatedAt int64     `json:"created_at"`
	RetiredAt *int64    `json:"retired_at"`
}

type Operation struct {
	OperationID   string `json:"operation_id"`
	OrgID         string `json:"org_id"`
	AgentID       string `json:"agent_id"`
	SeqNo         int64  `json:"seq_no"`
	OperationType string `json:"operation_type"`
	IssuedAt      int64  `json:"issued_at"`
	TTLMs         int64  `json:"ttl_ms"`
	Nonce         string `json:"nonce"`
	Subject       string `json:"subject"`
	Action        string `json:"action"`
	PayloadHash   string `json:"payload_hash"`
	PrevChainHash string `json:"prev_chain_hash"`
	ChainHash     string `json:"chain_hash"`
	AgentPubkeyKID string `json:"agent_pubkey_kid"`
	Signature     string `json:"signature"`
	R2PayloadKey  *string `json:"r2_payload_key"`
	CreatedAt     int64  `json:"created_at"`
}

type Receipt struct {
	ReceiptID    string `json:"receipt_id"`
	OperationID  string `json:"operation_id"`
	R2ReceiptKey string `json:"r2_receipt_key"`
	CreatedAt    int64  `json:"created_at"`
}

type Epoch struct {
	EpochID    string `json:"epoch_id"`
	OrgID      string `json:"org_id"`
	StartTime  int64  `json:"start_time"`
	EndTime    int64  `json:"end_time"`
	RootHash   string `json:"root_hash"`
	LeafCount  int64  `json:"leaf_count"`
	R2EpochKey string `json:"r2_epoch_key"`
	CreatedAt  int64  `json:"created_at"`
}

type Organization struct {
	OrgID     string `json:"org_id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type User struct {
	UserID      string   `json:"user_id"`
	OrgID       string   `json:"org_id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Role        RbacRole `json:"role"`
	Status      string   `json:"status"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

type Export struct {
	ExportID    string       `json:"export_id"`
	OrgID       string       `json:"org_id"`
	Status      ExportStatus `json:"status"`
	QueryParams string       `json:"query_params"`
	R2ExportKey *string      `json:"r2_export_key"`
	CreatedAt   int64        `json:"created_at"`
	CompletedAt *int64       `json:"completed_at"`
}

// ---------------------------------------------------------------------------
// Protocol types
// ---------------------------------------------------------------------------

// EOR is the Elydora Operation Record — the fundamental unit of auditable activity.
type EOR struct {
	OpVersion     string      `json:"op_version"`
	OperationID   string      `json:"operation_id"`
	OrgID         string      `json:"org_id"`
	AgentID       string      `json:"agent_id"`
	IssuedAt      int64       `json:"issued_at"`
	TTLMs         int64       `json:"ttl_ms"`
	Nonce         string      `json:"nonce"`
	OperationType string      `json:"operation_type"`
	Subject       interface{} `json:"subject"`
	Action        interface{} `json:"action"`
	Payload       interface{} `json:"payload"`
	PayloadHash   string      `json:"payload_hash"`
	PrevChainHash string      `json:"prev_chain_hash"`
	AgentPubkeyKID string    `json:"agent_pubkey_kid"`
	Signature     string      `json:"signature"`
}

// EAR is the Elydora Acknowledgment Receipt — server-issued receipt confirming acceptance.
type EAR struct {
	ReceiptVersion  string `json:"receipt_version"`
	ReceiptID       string `json:"receipt_id"`
	OperationID     string `json:"operation_id"`
	OrgID           string `json:"org_id"`
	AgentID         string `json:"agent_id"`
	ServerReceivedAt int64 `json:"server_received_at"`
	SeqNo           int64  `json:"seq_no"`
	ChainHash       string `json:"chain_hash"`
	QueueMessageID  string `json:"queue_message_id"`
	ReceiptHash     string `json:"receipt_hash"`
	ElydoraKID      string `json:"elydora_kid"`
	ElydoraSignature string `json:"elydora_signature"`
}

// ---------------------------------------------------------------------------
// API request/response types
// ---------------------------------------------------------------------------

type RegisterAgentKeyInput struct {
	KID       string `json:"kid"`
	PublicKey string `json:"public_key"`
	Algorithm string `json:"algorithm"`
}

type RegisterAgentRequest struct {
	AgentID           string                  `json:"agent_id"`
	DisplayName       string                  `json:"display_name,omitempty"`
	ResponsibleEntity string                  `json:"responsible_entity,omitempty"`
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

type RevokeAgentRequest struct {
	KID    string `json:"kid"`
	Reason string `json:"reason"`
}

type UnfreezeAgentRequest struct {
	Reason string `json:"reason"`
}

type ListAgentsResponse struct {
	Agents []Agent `json:"agents"`
}

type DeleteAgentResponse struct {
	Deleted bool `json:"deleted"`
}

type GetMeResponse struct {
	User User `json:"user"`
}

type IssueApiTokenRequest struct {
	TTLSeconds *int `json:"ttl_seconds"`
}

type IssueApiTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt *int64 `json:"expires_at"`
}

type HealthResponse struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Timestamp       int64  `json:"timestamp"`
}

type SubmitOperationResponse struct {
	Receipt EAR `json:"receipt"`
}

type GetOperationResponse struct {
	Operation Operation `json:"operation"`
	Receipt   *Receipt  `json:"receipt,omitempty"`
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
	TSAToken string `json:"tsa_token,omitempty"`
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
	Export      Export  `json:"export"`
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

// ---------------------------------------------------------------------------
// CreateOperation params (SDK-specific)
// ---------------------------------------------------------------------------

type CreateOperationParams struct {
	OperationType string
	Subject       map[string]interface{}
	Action        map[string]interface{}
	Payload       interface{}
	PrevChainHash string
	KID           string
}
