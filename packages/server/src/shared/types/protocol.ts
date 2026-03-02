/**
 * Elydora Operation Record (EOR)
 *
 * The fundamental unit of auditable activity in the Elydora protocol.
 * Signed by the agent's Ed25519 key and chained via prev_chain_hash.
 */
export interface EOR {
  /** Protocol version, always "1.0" */
  readonly op_version: '1.0';
  /** Unique operation identifier (UUIDv7) */
  readonly operation_id: string;
  /** Organization identifier */
  readonly org_id: string;
  /** Agent that issued this operation */
  readonly agent_id: string;
  /** Timestamp when issued (Unix milliseconds) */
  readonly issued_at: number;
  /** Time-to-live in milliseconds */
  readonly ttl_ms: number;
  /** Replay-prevention nonce (base64url) */
  readonly nonce: string;
  /** Classification of the operation */
  readonly operation_type: string;
  /** Subject of the operation */
  readonly subject: Record<string, unknown>;
  /** Action performed */
  readonly action: Record<string, unknown>;
  /** Operation payload */
  readonly payload: Record<string, unknown> | string | null;
  /** SHA-256 hash of the payload (base64url) */
  readonly payload_hash: string;
  /** Hash of the previous operation in the agent's chain (base64url) */
  readonly prev_chain_hash: string;
  /** Key ID of the agent's signing key */
  readonly agent_pubkey_kid: string;
  /** Ed25519 signature over the canonical EOR (base64url) */
  readonly signature: string;
}

/**
 * Elydora Chain Hash (ECH)
 *
 * Represents the chain hash computation linking operations together.
 */
export interface ECH {
  /** Previous chain hash (base64url) */
  readonly prev_ech: string;
  /** SHA-256 hash of the operation payload (base64url) */
  readonly payload_hash: string;
  /** Operation identifier (UUIDv7) */
  readonly operation_id: string;
  /** Timestamp when issued (Unix milliseconds) */
  readonly issued_at: number;
  /** Resulting chain hash (base64url) */
  readonly chain_hash: string;
}

/**
 * Elydora Acknowledgment Receipt (EAR)
 *
 * Server-issued receipt confirming acceptance of an operation.
 */
export interface EAR {
  /** Receipt version */
  readonly receipt_version: string;
  /** Unique receipt identifier */
  readonly receipt_id: string;
  /** Operation this receipt acknowledges */
  readonly operation_id: string;
  /** Organization identifier */
  readonly org_id: string;
  /** Agent that submitted the operation */
  readonly agent_id: string;
  /** Timestamp when the server received the operation (Unix ms) */
  readonly server_received_at: number;
  /** Sequence number assigned to the operation */
  readonly seq_no: number;
  /** Chain hash at time of receipt */
  readonly chain_hash: string;
  /** Queue message ID for async processing */
  readonly queue_message_id: string;
  /** Hash of this receipt */
  readonly receipt_hash: string;
  /** Elydora server key ID used for signing */
  readonly elydora_kid: string;
  /** Elydora server signature over the receipt */
  readonly elydora_signature: string;
}

/**
 * Elydora Epoch Record (EER)
 *
 * Periodic Merkle-tree rollup anchoring operations to a root hash.
 */
export interface EER {
  /** Unique epoch identifier */
  readonly epoch_id: string;
  /** Organization identifier */
  readonly org_id: string;
  /** Epoch start time (Unix ms) */
  readonly start_time: number;
  /** Epoch end time (Unix ms) */
  readonly end_time: number;
  /** Number of leaf operations in this epoch */
  readonly leaf_count: number;
  /** Merkle root hash */
  readonly root_hash: string;
  /** Hash algorithm used (e.g. "sha256") */
  readonly hash_alg: string;
  /** Elydora server signature over the epoch record */
  readonly signature_by_elydora: string;
}
