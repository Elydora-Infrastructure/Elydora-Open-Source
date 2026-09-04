export type AgentStatus = 'active' | 'frozen' | 'revoked';
export type KeyStatus = 'active' | 'retired' | 'revoked';
export type ExportStatus = 'queued' | 'running' | 'done' | 'failed';
export type RbacRole =
  | 'org_owner'
  | 'security_admin'
  | 'compliance_auditor'
  | 'readonly_investigator'
  | 'integration_engineer';
export const INTEGRATION_TYPES = [
  'augment',
  'claudecode',
  'cline',
  'codex',
  'copilot',
  'cursor',
  'droid',
  'gemini',
  'grok',
  'kimi',
  'kirocli',
  'kiroide',
  'letta',
  'opencode',
  'qwen',
  'enterprise',
  'gui',
  'sdk',
  'other',
] as const;
export type IntegrationType = (typeof INTEGRATION_TYPES)[number];
export type AdminAction =
  | 'agent.register' | 'agent.update' | 'agent.freeze' | 'agent.unfreeze'
  | 'agent.revoke' | 'agent.delete' | 'key.revoke' | 'export.create'
  | 'org.create' | 'org.update'
  | 'member.invite' | 'invitation.cancel' | 'member.remove' | 'member.role_change'
  | 'agent.assign' | 'agent.unassign'
  | 'webhook.register' | 'webhook.delete';
export type ErrorCode =
  | 'INVALID_SIGNATURE'
  | 'UNKNOWN_AGENT'
  | 'KEY_REVOKED'
  | 'KEY_RETIRED'
  | 'AGENT_FROZEN'
  | 'AGENT_REVOKED'
  | 'TTL_EXPIRED'
  | 'REPLAY_DETECTED'
  | 'PREV_HASH_MISMATCH'
  | 'PAYLOAD_TOO_LARGE'
  | 'RATE_LIMITED'
  | 'INTERNAL_ERROR'
  | 'UNAUTHORIZED'
  | 'FORBIDDEN'
  | 'NOT_FOUND'
  | 'VALIDATION_ERROR';

export interface Agent {
  readonly agent_id: string;
  readonly org_id: string;
  readonly display_name: string;
  readonly responsible_entity: string;
  readonly integration_type: IntegrationType;
  readonly status: AgentStatus;
  readonly created_at: number;
  readonly updated_at: number;
}

export interface AgentKey {
  readonly kid: string;
  readonly agent_id: string;
  readonly public_key: string;
  readonly algorithm: 'ed25519';
  readonly status: KeyStatus;
  readonly created_at: number;
  readonly retired_at: number | null;
}

export interface Operation {
  readonly operation_id: string;
  readonly org_id: string;
  readonly agent_id: string;
  readonly seq_no: number;
  readonly operation_type: string;
  readonly issued_at: number;
  readonly ttl_ms: number;
  readonly nonce: string;
  readonly subject: string;
  readonly action: string;
  readonly payload_hash: string;
  readonly prev_chain_hash: string;
  readonly chain_hash: string;
  readonly agent_pubkey_kid: string;
  readonly signature: string;
  readonly r2_payload_key: string | null;
  readonly created_at: number;
}

export interface Receipt {
  readonly receipt_id: string;
  readonly operation_id: string;
  readonly r2_receipt_key: string;
  readonly created_at: number;
}

export interface Epoch {
  readonly epoch_id: string;
  readonly org_id: string;
  readonly start_time: number;
  readonly end_time: number;
  readonly root_hash: string;
  readonly leaf_count: number;
  readonly r2_epoch_key: string;
  readonly created_at: number;
}

export interface Organization {
  readonly org_id: string;
  readonly name: string;
  readonly description: string;
  readonly ba_org_id: string | null;
  readonly created_at: number;
  readonly updated_at: number;
}

export interface AdminEvent {
  readonly event_id: string;
  readonly org_id: string;
  readonly actor: string;
  readonly action: string;
  readonly target_type: string;
  readonly target_id: string;
  readonly details: string | null;
  readonly created_at: number;
}

export interface AgentAssignment {
  readonly id: string;
  readonly agent_id: string;
  readonly user_id: string;
  readonly org_id: string;
  readonly assigned_by: string;
  readonly created_at: number;
}

export interface User {
  readonly user_id: string;
  readonly org_id: string;
  readonly email: string;
  readonly display_name: string;
  readonly role: RbacRole;
  readonly status: 'active' | 'suspended';
  readonly created_at: number;
  readonly updated_at: number;
}

export interface Export {
  readonly export_id: string;
  readonly org_id: string;
  readonly status: ExportStatus;
  readonly query_params: string;
  readonly r2_export_key: string | null;
  readonly created_at: number;
  readonly completed_at: number | null;
}

export interface EOR {
  readonly op_version: '1.0';
  readonly operation_id: string;
  readonly org_id: string;
  readonly agent_id: string;
  readonly issued_at: number;
  readonly ttl_ms: number;
  readonly nonce: string;
  readonly operation_type: string;
  readonly subject: Record<string, unknown>;
  readonly action: Record<string, unknown>;
  readonly payload: Record<string, unknown> | string | null;
  readonly payload_hash: string;
  readonly prev_chain_hash: string;
  readonly agent_pubkey_kid: string;
  readonly signature: string;
}

export interface EAR {
  readonly receipt_version: string;
  readonly receipt_id: string;
  readonly operation_id: string;
  readonly org_id: string;
  readonly agent_id: string;
  readonly server_received_at: number;
  readonly seq_no: number;
  readonly chain_hash: string;
  readonly queue_message_id: string;
  readonly receipt_hash: string;
  readonly elydora_kid: string;
  readonly elydora_signature: string;
}

export interface RegisterAgentRequest {
  readonly agent_id: string;
  readonly integration_type: IntegrationType;
  readonly display_name?: string;
  readonly responsible_entity?: string;
  readonly keys: ReadonlyArray<{
    readonly kid: string;
    readonly public_key: string;
    readonly algorithm: 'ed25519';
  }>;
}

export interface RegisterAgentResponse {
  readonly agent: Agent;
  readonly keys: AgentKey[];
}

export interface GetAgentResponse {
  readonly agent: Agent;
  readonly keys: AgentKey[];
}

export interface SubmitOperationResponse {
  readonly receipt: EAR;
}

export interface GetOperationResponse {
  readonly operation: Operation;
  readonly receipt?: Receipt;
  readonly payload?: Record<string, unknown> | string;
}

export interface VerifyOperationResponse {
  readonly valid: boolean;
  readonly checks: {
    readonly signature: boolean;
    readonly chain: boolean;
    readonly receipt: boolean;
    readonly merkle?: boolean;
  };
  readonly errors?: string[];
}

export interface AuditQueryRequest {
  readonly org_id?: string;
  readonly agent_id?: string;
  readonly operation_type?: string;
  readonly start_time?: number;
  readonly end_time?: number;
  readonly cursor?: string;
  readonly limit?: number;
}

export interface AuditQueryResponse {
  readonly operations: Operation[];
  readonly cursor?: string;
  readonly total_count: number;
}

export interface GetEpochResponse {
  readonly epoch: Epoch;
  readonly anchor?: {
    readonly tsa_token?: string;
    readonly tsa_url?: string;
    readonly anchored_at?: number;
  };
}

export interface ListEpochsResponse {
  readonly epochs: Epoch[];
}

export interface CreateExportRequest {
  readonly start_time: number;
  readonly end_time: number;
  readonly agent_id?: string;
  readonly operation_type?: string;
  readonly format: 'json' | 'pdf';
}

export interface CreateExportResponse {
  readonly export: Export;
}

export interface GetExportResponse {
  readonly export: Export;
  readonly download_url?: string;
}

export interface ListExportsResponse {
  readonly exports: Export[];
}

export interface ListAgentsResponse {
  readonly agents: Agent[];
}

export interface UpdateAgentRequest {
  readonly integration_type: IntegrationType;
}

export interface UpdateAgentResponse {
  readonly agent: Agent;
}

export interface FreezeAgentResponse {
  readonly agent: Agent;
  readonly previous_status: AgentStatus;
}

export interface UnfreezeAgentResponse {
  readonly agent: Agent;
  readonly previous_status: AgentStatus;
}

export interface DeleteAgentResponse {
  readonly deleted: boolean;
}

export interface Webhook {
  readonly webhook_id: string;
  readonly org_id: string;
  readonly endpoint_url: string;
  readonly events: string[];
  readonly status: 'active' | 'disabled';
  readonly created_at: number;
  readonly updated_at: number;
}

export interface ListWebhooksResponse {
  readonly webhooks: Webhook[];
}

export interface RegisterWebhookResponse {
  readonly webhook: Webhook;
}

export interface ListMembersResponse {
  readonly members: {
    readonly member_id: string;
    readonly user_id: string;
    readonly role: string;
    readonly joined_at: string;
    readonly email: string;
    readonly display_name: string;
  }[];
}

export interface ListAdminEventsResponse {
  readonly events: AdminEvent[];
}

export interface DeepHealthResponse {
  readonly status: 'healthy' | 'degraded';
  readonly version: string;
  readonly protocol_version: string;
  readonly capabilities: Record<string, string>;
  readonly timestamp: number;
  readonly dependencies: {
    readonly d1: { status: 'ok' | 'failed'; latency_ms: number };
    readonly auth_storage: { status: 'ok' | 'failed'; latency_ms: number; contract_phase?: 'expand' | 'contract' };
    readonly r2: { status: 'ok' | 'failed'; latency_ms: number };
    readonly kv: { status: 'ok' | 'failed'; latency_ms: number };
  };
}

export interface GetMeResponse {
  readonly user: Omit<User, 'org_id'> & {
    readonly org_id: string | null;
    readonly onboarding_completed: boolean;
  };
  readonly current_organization: {
    readonly org_id: string | null;
    readonly ba_org_id: string | null;
    readonly role: RbacRole;
  };
}

export interface IssueApiTokenResponse {
  readonly token: string;
  readonly expires_at: number | null;
  readonly token_id: string;
}

export interface RotateApiTokenResponse {
  readonly token: string;
  readonly expires_at: number | null;
  readonly token_id: string;
  readonly previous_token_grace_until: number;
}

export interface HealthResponse {
  readonly status: string;
  readonly version: string;
  readonly protocol_version: string;
  readonly capabilities: Record<string, string>;
  readonly timestamp: number;
}

export interface JWK {
  readonly kty: string;
  readonly crv?: string;
  readonly x?: string;
  readonly kid: string;
  readonly use: string;
  readonly alg: string;
}

export interface JWKSResponse {
  readonly keys: JWK[];
}

export interface AuthRegisterResponse {
  readonly user: User;
  readonly organization: Organization;
  readonly token: string;
}

export interface AuthLoginResponse {
  readonly user: User;
  readonly token: string;
}

export interface ErrorResponse {
  readonly error: {
    readonly code: ErrorCode;
    readonly message: string;
    readonly request_id: string;
    readonly details?: Record<string, unknown>;
  };
}

export interface ElydoraClientConfig {
  readonly orgId: string;
  readonly agentId: string;
  readonly privateKey: string;
  readonly baseUrl?: string;
  readonly ttlMs?: number;
  readonly maxRetries?: number;
  readonly kid?: string;
}

export interface CreateOperationParams {
  readonly operationType: string;
  readonly subject: Record<string, unknown>;
  readonly action: Record<string, unknown>;
  readonly payload?: Record<string, unknown> | string | null;
}
