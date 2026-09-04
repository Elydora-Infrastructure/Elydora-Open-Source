import type { EOR } from '../shared/index.js';
import { MAX_PAYLOAD_SIZE, MAX_TTL_MS, MIN_TTL_MS, MAX_NONCE_LENGTH } from '../shared/index.js';
import { AppError } from '../middleware/error-handler.js';

/** Genesis chain hash for an agent's first operation. */
export const GENESIS_CHAIN_HASH = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA';

/** Server key ID carried in receipts. */
export const ELYDORA_KID = 'elydora-server-key-v1';

/** The EOR fields covered by the agent signature. */
export function buildSignableEOR(eor: EOR): Record<string, unknown> {
  return {
    op_version: eor.op_version,
    operation_id: eor.operation_id,
    org_id: eor.org_id,
    agent_id: eor.agent_id,
    issued_at: eor.issued_at,
    ttl_ms: eor.ttl_ms,
    nonce: eor.nonce,
    operation_type: eor.operation_type,
    subject: eor.subject,
    action: eor.action,
    payload: eor.payload,
    payload_hash: eor.payload_hash,
    prev_chain_hash: eor.prev_chain_hash,
    agent_pubkey_kid: eor.agent_pubkey_kid,
  };
}

export function validateEORFields(eor: EOR, receivedAt: number): void {
  if (eor.op_version !== '1.0') {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.unsupportedVersion', params: { version: eor.op_version } });
  }

  const requiredStrings: Array<[string, unknown]> = [
    ['operation_id', eor.operation_id],
    ['org_id', eor.org_id],
    ['agent_id', eor.agent_id],
    ['nonce', eor.nonce],
    ['operation_type', eor.operation_type],
    ['payload_hash', eor.payload_hash],
    ['prev_chain_hash', eor.prev_chain_hash],
    ['agent_pubkey_kid', eor.agent_pubkey_kid],
    ['signature', eor.signature],
  ];

  for (const [field, value] of requiredStrings) {
    if (!value || typeof value !== 'string' || value.trim().length === 0) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.missingField', params: { field } });
    }
  }

  if (eor.nonce.length > MAX_NONCE_LENGTH) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.nonceTooLong', params: { max: MAX_NONCE_LENGTH } });
  }

  if (typeof eor.issued_at !== 'number' || eor.issued_at <= 0) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.invalidIssuedAt' });
  }

  if (typeof eor.ttl_ms !== 'number') {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.missingTtl' });
  }
  if (eor.ttl_ms < MIN_TTL_MS) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.ttlTooLow', params: { min: MIN_TTL_MS } });
  }
  if (eor.ttl_ms > MAX_TTL_MS) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.ttlTooHigh', params: { max: MAX_TTL_MS } });
  }

  if (eor.issued_at + eor.ttl_ms < receivedAt) {
    throw new AppError(400, 'TTL_EXPIRED');
  }

  if (eor.payload !== null && eor.payload !== undefined) {
    const payloadStr = typeof eor.payload === 'string' ? eor.payload : JSON.stringify(eor.payload);
    if (new TextEncoder().encode(payloadStr).byteLength > MAX_PAYLOAD_SIZE) {
      throw new AppError(400, 'PAYLOAD_TOO_LARGE');
    }
  }
}
