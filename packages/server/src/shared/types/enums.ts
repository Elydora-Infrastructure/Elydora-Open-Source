/** Agent lifecycle status */
export type AgentStatus = 'active' | 'frozen' | 'revoked';

/** Agent key lifecycle status */
export type KeyStatus = 'active' | 'retired' | 'revoked';

/** Export job status */
export type ExportStatus = 'queued' | 'running' | 'done' | 'failed';

/** Administrative action types for audit logging */
export type AdminAction =
  | 'agent.register'
  | 'agent.update'
  | 'agent.freeze'
  | 'agent.unfreeze'
  | 'agent.revoke'
  | 'agent.delete'
  | 'key.revoke'
  | 'export.create';

/** Supported agent integration types */
export type IntegrationType =
  | 'claudecode' | 'cursor' | 'gemini' | 'kirocli' | 'kiroide'
  | 'opencode' | 'copilot' | 'letta' | 'codex' | 'kimi'
  | 'enterprise' | 'gui' | 'sdk' | 'other';

/** RBAC roles for console access */
export type RbacRole =
  | 'org_owner'
  | 'security_admin'
  | 'compliance_auditor'
  | 'readonly_investigator'
  | 'integration_engineer';

/** Error codes returned by the API */
export type ErrorCode =
  | 'INVALID_SIGNATURE'
  | 'UNKNOWN_AGENT'
  | 'KEY_REVOKED'
  | 'AGENT_FROZEN'
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
