import type { Agent, AgentKey, Epoch, Export, Operation, Organization, Receipt, User } from './entities.js';
import type { ErrorCode } from './enums.js';
import type { EAR, EOR } from './protocol.js';

// ---------------------------------------------------------------------------
// Agent endpoints
// ---------------------------------------------------------------------------

export interface RegisterAgentRequest {
  readonly agent_id: string;
  readonly display_name?: string;
  readonly responsible_entity?: string;
  readonly integration_type?: string;
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

export interface ListAgentsResponse {
  readonly agents: Agent[];
}

export interface FreezeAgentRequest {
  readonly reason: string;
}

export interface UnfreezeAgentRequest {
  readonly reason: string;
}

export interface RevokeAgentRequest {
  readonly kid: string;
  readonly reason: string;
}

export interface UpdateAgentRequest {
  readonly integration_type: string;
}

// ---------------------------------------------------------------------------
// Operation endpoints
// ---------------------------------------------------------------------------

export type SubmitOperationRequest = EOR;

export interface SubmitOperationResponse {
  readonly receipt: EAR;
}

export interface GetOperationResponse {
  readonly operation: Operation;
  readonly receipt?: Receipt;
  readonly payload?: Record<string, unknown>;
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

// ---------------------------------------------------------------------------
// Audit query endpoints
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Epoch endpoints
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Export endpoints
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// JWKS endpoint
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Auth endpoints
// ---------------------------------------------------------------------------

export interface AuthRegisterRequest {
  readonly email: string;
  readonly password: string;
  readonly display_name?: string;
  readonly org_name?: string;
}

export interface AuthRegisterResponse {
  readonly user: User;
  readonly organization: Organization;
  readonly token: string;
}

export interface AuthLoginRequest {
  readonly email: string;
  readonly password: string;
}

export interface AuthLoginResponse {
  readonly user: User;
  readonly token: string;
}

export interface AuthMeResponse {
  readonly user: User;
}

export interface AuthRefreshResponse {
  readonly token: string;
}

export interface IssueTokenRequest {
  readonly ttl_seconds?: number | null; // null = never expire
}

export interface IssueTokenResponse {
  readonly token: string;
  readonly expires_at: number | null; // null = never expire
}

// ---------------------------------------------------------------------------
// Error response
// ---------------------------------------------------------------------------

export interface ErrorResponse {
  readonly error: {
    readonly code: ErrorCode;
    readonly message: string;
    readonly request_id: string;
    readonly details?: Record<string, unknown>;
  };
}
