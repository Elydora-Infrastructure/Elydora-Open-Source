import type { AgentStatus, ExportStatus, KeyStatus, RbacRole } from './enums.js';

/** Registered AI agent within an organization */
export interface Agent {
  readonly agent_id: string;
  readonly org_id: string;
  readonly display_name: string;
  readonly responsible_entity: string;
  readonly integration_type: string;
  readonly status: AgentStatus;
  readonly created_at: number;
  readonly updated_at: number;
}

/** Public key associated with an agent for signing operations */
export interface AgentKey {
  readonly kid: string;
  readonly agent_id: string;
  /** Base64-encoded public key (stored as BLOB in D1) */
  readonly public_key: string;
  readonly algorithm: 'ed25519';
  readonly status: KeyStatus;
  readonly created_at: number;
  readonly retired_at: number | null;
}

/** Persisted operation record in D1 */
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

/** Receipt stored in R2, referenced from D1 */
export interface Receipt {
  readonly receipt_id: string;
  readonly operation_id: string;
  readonly r2_receipt_key: string;
  readonly created_at: number;
}

/** Periodic Merkle epoch rollup record */
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

/** Admin audit event log entry */
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

/** Organization */
export interface Organization {
  readonly org_id: string;
  readonly name: string;
  readonly created_at: number;
  readonly updated_at: number;
}

/** Console user within an organization */
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

/** Compliance export job */
export interface Export {
  readonly export_id: string;
  readonly org_id: string;
  readonly status: ExportStatus;
  readonly query_params: string;
  readonly r2_export_key: string | null;
  readonly created_at: number;
  readonly completed_at: number | null;
}
