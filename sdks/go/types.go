package elydora

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

type IntegrationType string

const (
	IntegrationTypeAugment    IntegrationType = "augment"
	IntegrationTypeClaudecode IntegrationType = "claudecode"
	IntegrationTypeCline      IntegrationType = "cline"
	IntegrationTypeCodex      IntegrationType = "codex"
	IntegrationTypeCopilot    IntegrationType = "copilot"
	IntegrationTypeCursor     IntegrationType = "cursor"
	IntegrationTypeDroid      IntegrationType = "droid"
	IntegrationTypeGemini     IntegrationType = "gemini"
	IntegrationTypeGrok       IntegrationType = "grok"
	IntegrationTypeKimi       IntegrationType = "kimi"
	IntegrationTypeKiroCLI    IntegrationType = "kirocli"
	IntegrationTypeKiroIDE    IntegrationType = "kiroide"
	IntegrationTypeLetta      IntegrationType = "letta"
	IntegrationTypeOpenCode   IntegrationType = "opencode"
	IntegrationTypeQwen       IntegrationType = "qwen"
	IntegrationTypeEnterprise IntegrationType = "enterprise"
	IntegrationTypeGUI        IntegrationType = "gui"
	IntegrationTypeSDK        IntegrationType = "sdk"
	IntegrationTypeOther      IntegrationType = "other"
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
	RbacRoleOrgOwner             RbacRole = "org_owner"
	RbacRoleSecurityAdmin        RbacRole = "security_admin"
	RbacRoleComplianceAuditor    RbacRole = "compliance_auditor"
	RbacRoleReadonlyInvestigator RbacRole = "readonly_investigator"
	RbacRoleIntegrationEngineer  RbacRole = "integration_engineer"
)

type ErrorCode string

const (
	ErrorCodeInvalidSignature ErrorCode = "INVALID_SIGNATURE"
	ErrorCodeUnknownAgent     ErrorCode = "UNKNOWN_AGENT"
	ErrorCodeKeyRevoked       ErrorCode = "KEY_REVOKED"
	ErrorCodeKeyRetired       ErrorCode = "KEY_RETIRED"
	ErrorCodeAgentFrozen      ErrorCode = "AGENT_FROZEN"
	ErrorCodeAgentRevoked     ErrorCode = "AGENT_REVOKED"
	ErrorCodeTTLExpired       ErrorCode = "TTL_EXPIRED"
	ErrorCodeReplayDetected   ErrorCode = "REPLAY_DETECTED"
	ErrorCodePrevHashMismatch ErrorCode = "PREV_HASH_MISMATCH"
	ErrorCodePayloadTooLarge  ErrorCode = "PAYLOAD_TOO_LARGE"
	ErrorCodeRateLimited      ErrorCode = "RATE_LIMITED"
	ErrorCodeInternalError    ErrorCode = "INTERNAL_ERROR"
	ErrorCodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	ErrorCodeForbidden        ErrorCode = "FORBIDDEN"
	ErrorCodeNotFound         ErrorCode = "NOT_FOUND"
	ErrorCodeValidationError  ErrorCode = "VALIDATION_ERROR"
)

type AdminAction string

const (
	AdminActionAgentRegister    AdminAction = "agent.register"
	AdminActionAgentUpdate      AdminAction = "agent.update"
	AdminActionAgentFreeze      AdminAction = "agent.freeze"
	AdminActionAgentUnfreeze    AdminAction = "agent.unfreeze"
	AdminActionAgentRevoke      AdminAction = "agent.revoke"
	AdminActionAgentDelete      AdminAction = "agent.delete"
	AdminActionKeyRevoke        AdminAction = "key.revoke"
	AdminActionExportCreate     AdminAction = "export.create"
	AdminActionOrgCreate        AdminAction = "org.create"
	AdminActionOrgUpdate        AdminAction = "org.update"
	AdminActionMemberInvite     AdminAction = "member.invite"
	AdminActionInvitationCancel AdminAction = "invitation.cancel"
	AdminActionMemberRemove     AdminAction = "member.remove"
	AdminActionMemberRoleChange AdminAction = "member.role_change"
	AdminActionAgentAssign      AdminAction = "agent.assign"
	AdminActionAgentUnassign    AdminAction = "agent.unassign"
	AdminActionWebhookRegister  AdminAction = "webhook.register"
	AdminActionWebhookDelete    AdminAction = "webhook.delete"
)

type WebhookStatus string

const (
	WebhookStatusActive   WebhookStatus = "active"
	WebhookStatusDisabled WebhookStatus = "disabled"
)

type ExportFormat string

const (
	ExportFormatJSON ExportFormat = "json"
	ExportFormatPDF  ExportFormat = "pdf"
)

type Agent struct {
	AgentID           string          `json:"agent_id"`
	OrgID             string          `json:"org_id"`
	DisplayName       string          `json:"display_name"`
	ResponsibleEntity string          `json:"responsible_entity"`
	IntegrationType   IntegrationType `json:"integration_type"`
	Status            AgentStatus     `json:"status"`
	CreatedAt         int64           `json:"created_at"`
	UpdatedAt         int64           `json:"updated_at"`
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
	OperationID    string  `json:"operation_id"`
	OrgID          string  `json:"org_id"`
	AgentID        string  `json:"agent_id"`
	SeqNo          int64   `json:"seq_no"`
	OperationType  string  `json:"operation_type"`
	IssuedAt       int64   `json:"issued_at"`
	TTLMs          int64   `json:"ttl_ms"`
	Nonce          string  `json:"nonce"`
	Subject        string  `json:"subject"`
	Action         string  `json:"action"`
	PayloadHash    string  `json:"payload_hash"`
	PrevChainHash  string  `json:"prev_chain_hash"`
	ChainHash      string  `json:"chain_hash"`
	AgentPubkeyKID string  `json:"agent_pubkey_kid"`
	Signature      string  `json:"signature"`
	R2PayloadKey   *string `json:"r2_payload_key"`
	CreatedAt      int64   `json:"created_at"`
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
	OrgID       string  `json:"org_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	BAOrgID     *string `json:"ba_org_id"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
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

// EOR is the Elydora Operation Record.
type EOR struct {
	OpVersion      string      `json:"op_version"`
	OperationID    string      `json:"operation_id"`
	OrgID          string      `json:"org_id"`
	AgentID        string      `json:"agent_id"`
	IssuedAt       int64       `json:"issued_at"`
	TTLMs          int64       `json:"ttl_ms"`
	Nonce          string      `json:"nonce"`
	OperationType  string      `json:"operation_type"`
	Subject        interface{} `json:"subject"`
	Action         interface{} `json:"action"`
	Payload        interface{} `json:"payload"`
	PayloadHash    string      `json:"payload_hash"`
	PrevChainHash  string      `json:"prev_chain_hash"`
	AgentPubkeyKID string      `json:"agent_pubkey_kid"`
	Signature      string      `json:"signature"`
}

// EAR is the Elydora Acknowledgment Receipt.
type EAR struct {
	ReceiptVersion   string `json:"receipt_version"`
	ReceiptID        string `json:"receipt_id"`
	OperationID      string `json:"operation_id"`
	OrgID            string `json:"org_id"`
	AgentID          string `json:"agent_id"`
	ServerReceivedAt int64  `json:"server_received_at"`
	SeqNo            int64  `json:"seq_no"`
	ChainHash        string `json:"chain_hash"`
	QueueMessageID   string `json:"queue_message_id"`
	ReceiptHash      string `json:"receipt_hash"`
	ElydoraKID       string `json:"elydora_kid"`
	ElydoraSignature string `json:"elydora_signature"`
}

// AdminEvent represents an administrative event log entry.
type AdminEvent struct {
	EventID    string  `json:"event_id"`
	OrgID      string  `json:"org_id"`
	Actor      string  `json:"actor"`
	Action     string  `json:"action"`
	TargetType string  `json:"target_type"`
	TargetID   string  `json:"target_id"`
	Details    *string `json:"details"`
	CreatedAt  int64   `json:"created_at"`
}

// AgentAssignment represents a many-to-many agent-to-user assignment.
type AgentAssignment struct {
	ID         string `json:"id"`
	AgentID    string `json:"agent_id"`
	UserID     string `json:"user_id"`
	OrgID      string `json:"org_id"`
	AssignedBy string `json:"assigned_by"`
	CreatedAt  int64  `json:"created_at"`
}

// Webhook represents a registered webhook endpoint.
type Webhook struct {
	WebhookID   string        `json:"webhook_id"`
	OrgID       string        `json:"org_id"`
	EndpointURL string        `json:"endpoint_url"`
	Events      []string      `json:"events"`
	Status      WebhookStatus `json:"status"`
	CreatedAt   int64         `json:"created_at"`
	UpdatedAt   int64         `json:"updated_at"`
}

// Member represents a console user returned from the members list endpoint.
type Member struct {
	MemberID    string `json:"member_id"`
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	JoinedAt    string `json:"joined_at"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// AuthMeUser is the profile returned by GET /v1/auth/me.
type AuthMeUser struct {
	UserID              string   `json:"user_id"`
	OrgID               *string  `json:"org_id"`
	Email               string   `json:"email"`
	DisplayName         string   `json:"display_name"`
	Role                RbacRole `json:"role"`
	Status              string   `json:"status"`
	CreatedAt           int64    `json:"created_at"`
	UpdatedAt           int64    `json:"updated_at"`
	OnboardingCompleted bool     `json:"onboarding_completed"`
}

// CurrentOrganization is the active organization of the authenticated user.
type CurrentOrganization struct {
	OrgID   *string  `json:"org_id"`
	BAOrgID *string  `json:"ba_org_id"`
	Role    RbacRole `json:"role"`
}

// DependencyHealth is the health of one backend dependency.
type DependencyHealth struct {
	Status        string `json:"status"`
	LatencyMs     int64  `json:"latency_ms"`
	ContractPhase string `json:"contract_phase,omitempty"`
}

// DeepHealthDependencies lists every backend dependency.
type DeepHealthDependencies struct {
	D1          DependencyHealth `json:"d1"`
	AuthStorage DependencyHealth `json:"auth_storage"`
	R2          DependencyHealth `json:"r2"`
	KV          DependencyHealth `json:"kv"`
}

// CreateOperationParams are the inputs for CreateOperation.
type CreateOperationParams struct {
	OperationType string
	Subject       map[string]interface{}
	Action        map[string]interface{}
	Payload       interface{}
	PrevChainHash string
	KID           string
}
