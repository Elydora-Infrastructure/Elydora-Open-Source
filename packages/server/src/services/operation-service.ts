/**
 * Operation service — business logic for EOR submission, retrieval, and verification.
 *
 * This is the core of the Elydora protocol implementation. It performs:
 * 1. EOR field validation
 * 2. Agent and key status checks
 * 3. Ed25519 signature verification
 * 4. Hash chain validation and computation
 * 5. Persistence to D1 and R2
 * 6. Queue enqueue for async processing
 * 7. EAR (receipt) generation with server signature
 */

import type {
  EOR,
  EAR,
  Agent,
  AgentKey,
  Operation,
  Receipt,
  GetOperationResponse,
  VerifyOperationResponse,
} from '../shared/index.js';
import {
  MAX_PAYLOAD_SIZE,
  MAX_TTL_MS,
  MIN_TTL_MS,
  MAX_NONCE_LENGTH,
} from '../shared/index.js';
import {
  base64urlDecode,
  verifyEd25519Signature,
  jcsCanonicalise,
  computeChainHash,
  computeReceiptHash,
  signEd25519,
} from '../utils/crypto.js';
import { generateUUIDv7 } from '../utils/uuid.js';
import { AppError } from '../middleware/error-handler.js';
import type { Database, ObjectStore, Cache, MessageQueue, PreparedStatement } from '../adapters/interfaces.js';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** The genesis chain hash used for an agent's first operation. */
const GENESIS_CHAIN_HASH = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA';

/** Elydora server key ID used in receipts. */
const ELYDORA_KID = 'elydora-server-key-v1';

// ---------------------------------------------------------------------------
// Submit operation
// ---------------------------------------------------------------------------

export async function submitOperation(
  db: Database,
  r2: ObjectStore,
  kv: Cache,
  queue: MessageQueue,
  eor: EOR,
  signingKey: string,
): Promise<{ receipt: EAR }> {
  const receivedAt = Date.now();

  // ------------------------------------------------------------------
  // Step 1: Validate EOR fields
  // ------------------------------------------------------------------
  validateEORFields(eor, receivedAt);

  // ------------------------------------------------------------------
  // Step 2: Replay detection via nonce in KV
  // ------------------------------------------------------------------
  const nonceKey = `nonce:${eor.org_id}:${eor.nonce}`;
  const existingNonce = await kv.get(nonceKey);
  if (existingNonce) {
    throw new AppError(400, 'REPLAY_DETECTED');
  }
  // Store nonce with TTL equal to the operation's TTL (in seconds)
  const nonceTtlSeconds = Math.ceil(eor.ttl_ms / 1000);
  await kv.put(nonceKey, '1', { expirationTtl: nonceTtlSeconds });

  // ------------------------------------------------------------------
  // Step 3: Look up agent and check status
  // ------------------------------------------------------------------
  const agent = await db
    .prepare('SELECT * FROM agents WHERE agent_id = ? AND org_id = ?')
    .bind(eor.agent_id, eor.org_id)
    .first<Agent>();

  if (!agent) {
    throw new AppError(404, 'UNKNOWN_AGENT', { key: 'operation.agentNotRegistered', params: { id: eor.agent_id } });
  }

  if (agent.status === 'frozen') {
    throw new AppError(403, 'AGENT_FROZEN');
  }

  if (agent.status === 'revoked') {
    throw new AppError(403, 'AGENT_FROZEN', { key: 'operation.agentRevoked' });
  }

  // ------------------------------------------------------------------
  // Step 4: Look up key and check status
  // ------------------------------------------------------------------
  const agentKey = await db
    .prepare('SELECT * FROM agent_keys WHERE kid = ? AND agent_id = ?')
    .bind(eor.agent_pubkey_kid, eor.agent_id)
    .first<AgentKey>();

  if (!agentKey) {
    throw new AppError(400, 'INVALID_SIGNATURE', { key: 'operation.keyNotFoundForAgent', params: { kid: eor.agent_pubkey_kid, id: eor.agent_id } });
  }

  if (agentKey.status === 'revoked') {
    throw new AppError(403, 'KEY_REVOKED');
  }

  if (agentKey.status === 'retired') {
    throw new AppError(403, 'KEY_REVOKED', { key: 'operation.keyRetired' });
  }

  // ------------------------------------------------------------------
  // Step 5: Verify Ed25519 signature
  // ------------------------------------------------------------------
  const signableEOR = buildSignableEOR(eor);
  const canonicalData = jcsCanonicalise(signableEOR);
  const dataBytes = new TextEncoder().encode(canonicalData);
  const signatureBytes = base64urlDecode(eor.signature);
  const publicKeyBytes = base64urlDecode(agentKey.public_key);

  const signatureValid = await verifyEd25519Signature(publicKeyBytes, signatureBytes, dataBytes);

  if (!signatureValid) {
    throw new AppError(400, 'INVALID_SIGNATURE');
  }

  // ------------------------------------------------------------------
  // Step 6: Verify prev_chain_hash against the latest chain_hash
  // ------------------------------------------------------------------
  const latestOp = await db
    .prepare(
      'SELECT chain_hash, seq_no FROM operations WHERE agent_id = ? ORDER BY seq_no DESC LIMIT 1',
    )
    .bind(eor.agent_id)
    .first<{ chain_hash: string; seq_no: number }>();

  const expectedPrevHash = latestOp ? latestOp.chain_hash : GENESIS_CHAIN_HASH;
  const nextSeqNo = latestOp ? latestOp.seq_no + 1 : 1;

  if (eor.prev_chain_hash !== expectedPrevHash) {
    throw new AppError(400, 'PREV_HASH_MISMATCH', { key: 'operation.prevHashMismatch', params: { expected: expectedPrevHash, actual: eor.prev_chain_hash } });
  }

  // ------------------------------------------------------------------
  // Step 7: Compute new chain_hash
  // ------------------------------------------------------------------
  const chainHash = await computeChainHash(
    eor.prev_chain_hash,
    eor.payload_hash,
    eor.operation_id,
    eor.issued_at,
  );

  // ------------------------------------------------------------------
  // Step 8: Store operation in D1
  // ------------------------------------------------------------------
  const r2PayloadKey = `${eor.org_id}/${eor.agent_id}/${eor.operation_id}`;

  const operation: Operation = {
    operation_id: eor.operation_id,
    org_id: eor.org_id,
    agent_id: eor.agent_id,
    seq_no: nextSeqNo,
    operation_type: eor.operation_type,
    issued_at: eor.issued_at,
    ttl_ms: eor.ttl_ms,
    nonce: eor.nonce,
    subject: JSON.stringify(eor.subject),
    action: JSON.stringify(eor.action),
    payload_hash: eor.payload_hash,
    prev_chain_hash: eor.prev_chain_hash,
    chain_hash: chainHash,
    agent_pubkey_kid: eor.agent_pubkey_kid,
    signature: eor.signature,
    r2_payload_key: r2PayloadKey,
    created_at: receivedAt,
  };

  // ------------------------------------------------------------------
  // Step 9: Store the full EOR envelope in R2
  // ------------------------------------------------------------------
  const eorJson = JSON.stringify(eor);
  await r2.put(r2PayloadKey, eorJson, {
    httpMetadata: { contentType: 'application/json' },
    customMetadata: {
      org_id: eor.org_id,
      agent_id: eor.agent_id,
      operation_type: eor.operation_type,
    },
  });

  // ------------------------------------------------------------------
  // Step 10: Enqueue for async processing
  // ------------------------------------------------------------------
  const queueMessageResult = await queue.send({
    type: 'operation',
    operation_id: eor.operation_id,
    org_id: eor.org_id,
    agent_id: eor.agent_id,
  });

  // D1 batch insert: operation + receipt
  const receiptId = generateUUIDv7();
  const queueMessageId = typeof queueMessageResult === 'object' && queueMessageResult !== null
    ? String((queueMessageResult as Record<string, unknown>).messageId ?? receiptId)
    : receiptId;

  // ------------------------------------------------------------------
  // Step 11: Generate EAR receipt
  // ------------------------------------------------------------------
  const receiptFields = {
    receipt_version: '1.0',
    receipt_id: receiptId,
    operation_id: eor.operation_id,
    org_id: eor.org_id,
    agent_id: eor.agent_id,
    server_received_at: receivedAt,
    seq_no: nextSeqNo,
    chain_hash: chainHash,
    queue_message_id: queueMessageId,
  };

  const receiptHash = await computeReceiptHash(receiptFields);

  // Sign the receipt hash with the server key
  const receiptDataToSign = new TextEncoder().encode(receiptHash);
  const elydoraSignature = await signEd25519(signingKey, receiptDataToSign);

  const ear: EAR = {
    ...receiptFields,
    receipt_hash: receiptHash,
    elydora_kid: ELYDORA_KID,
    elydora_signature: elydoraSignature,
  };

  // Store receipt in R2
  const r2ReceiptKey = `${eor.org_id}/${eor.agent_id}/receipts/${eor.operation_id}`;
  await r2.put(r2ReceiptKey, JSON.stringify(ear), {
    httpMetadata: { contentType: 'application/json' },
  });

  // ------------------------------------------------------------------
  // Step 12: Persist operation and receipt to D1
  // ------------------------------------------------------------------
  const statements: PreparedStatement[] = [];

  statements.push(
    db
      .prepare(
        `INSERT INTO operations (operation_id, org_id, agent_id, seq_no, operation_type, issued_at, ttl_ms, nonce, subject, action, payload_hash, prev_chain_hash, chain_hash, agent_pubkey_kid, signature, r2_payload_key, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .bind(
        operation.operation_id,
        operation.org_id,
        operation.agent_id,
        operation.seq_no,
        operation.operation_type,
        operation.issued_at,
        operation.ttl_ms,
        operation.nonce,
        operation.subject,
        operation.action,
        operation.payload_hash,
        operation.prev_chain_hash,
        operation.chain_hash,
        operation.agent_pubkey_kid,
        operation.signature,
        operation.r2_payload_key,
        operation.created_at,
      ),
  );

  statements.push(
    db
      .prepare(
        `INSERT INTO receipts (receipt_id, operation_id, r2_receipt_key, created_at)
         VALUES (?, ?, ?, ?)`,
      )
      .bind(receiptId, eor.operation_id, r2ReceiptKey, receivedAt),
  );

  await db.batch(statements);

  return { receipt: ear };
}

// ---------------------------------------------------------------------------
// Get operation
// ---------------------------------------------------------------------------

export async function getOperation(
  db: Database,
  r2: ObjectStore,
  operationId: string,
  orgId: string,
): Promise<GetOperationResponse> {
  const operation = await db
    .prepare('SELECT * FROM operations WHERE operation_id = ? AND org_id = ?')
    .bind(operationId, orgId)
    .first<Operation>();

  if (!operation) {
    throw new AppError(404, 'NOT_FOUND', { key: 'operation.notFound', params: { id: operationId } });
  }

  const receipt = await db
    .prepare('SELECT * FROM receipts WHERE operation_id = ?')
    .bind(operationId)
    .first<Receipt>();

  // Fetch payload from R2
  let payload: Record<string, unknown> | undefined;
  if (operation.r2_payload_key) {
    try {
      const r2Object = await r2.get(operation.r2_payload_key);
      if (r2Object) {
        const eor = await r2Object.json<Record<string, unknown>>();
        payload = (eor.payload as Record<string, unknown>) ?? undefined;
      }
    } catch {
      // Payload fetch is best-effort — don't fail the request
    }
  }

  return {
    operation,
    receipt: receipt ?? undefined,
    payload,
  };
}

// ---------------------------------------------------------------------------
// Verify operation
// ---------------------------------------------------------------------------

export async function verifyOperation(
  db: Database,
  r2: ObjectStore,
  operationId: string,
  orgId: string,
): Promise<VerifyOperationResponse> {
  const errors: string[] = [];
  let signatureCheck = false;
  let chainCheck = false;
  let receiptCheck = false;

  // Load the operation
  const operation = await db
    .prepare('SELECT * FROM operations WHERE operation_id = ? AND org_id = ?')
    .bind(operationId, orgId)
    .first<Operation>();

  if (!operation) {
    throw new AppError(404, 'NOT_FOUND', { key: 'operation.notFound', params: { id: operationId } });
  }

  // Load the EOR from R2
  if (!operation.r2_payload_key) {
    errors.push('Operation has no R2 payload key.');
    return { valid: false, checks: { signature: false, chain: false, receipt: false }, errors };
  }
  const eorObject = await r2.get(operation.r2_payload_key);
  if (!eorObject) {
    errors.push('EOR evidence not found in R2.');
    return { valid: false, checks: { signature: false, chain: false, receipt: false }, errors };
  }

  let eor: EOR;
  try {
    eor = JSON.parse(await eorObject.text()) as EOR;
  } catch {
    errors.push('Failed to parse EOR evidence from R2.');
    return { valid: false, checks: { signature: false, chain: false, receipt: false }, errors };
  }

  // ---- Signature verification ----
  try {
    const agentKey = await db
      .prepare('SELECT * FROM agent_keys WHERE kid = ? AND agent_id = ?')
      .bind(eor.agent_pubkey_kid, eor.agent_id)
      .first<AgentKey>();

    if (agentKey) {
      const signableEOR = buildSignableEOR(eor);
      const canonicalData = jcsCanonicalise(signableEOR);
      const dataBytes = new TextEncoder().encode(canonicalData);
      const signatureBytes = base64urlDecode(eor.signature);
      const publicKeyBytes = base64urlDecode(agentKey.public_key);

      signatureCheck = await verifyEd25519Signature(publicKeyBytes, signatureBytes, dataBytes);
      if (!signatureCheck) {
        errors.push('Ed25519 signature verification failed.');
      }
    } else {
      errors.push(`Signing key "${eor.agent_pubkey_kid}" not found.`);
    }
  } catch (e) {
    errors.push(`Signature check error: ${e instanceof Error ? e.message : String(e)}`);
  }

  // ---- Chain hash verification ----
  try {
    const expectedChainHash = await computeChainHash(
      operation.prev_chain_hash,
      operation.payload_hash,
      operation.operation_id,
      operation.issued_at,
    );

    chainCheck = expectedChainHash === operation.chain_hash;
    if (!chainCheck) {
      errors.push(`Chain hash mismatch: expected "${expectedChainHash}", stored "${operation.chain_hash}".`);
    }

    // Also verify the prev_chain_hash links correctly
    if (operation.seq_no > 1) {
      const prevOp = await db
        .prepare('SELECT chain_hash FROM operations WHERE agent_id = ? AND seq_no = ?')
        .bind(operation.agent_id, operation.seq_no - 1)
        .first<{ chain_hash: string }>();

      if (prevOp && prevOp.chain_hash !== operation.prev_chain_hash) {
        chainCheck = false;
        errors.push(`prev_chain_hash does not match previous operation's chain_hash.`);
      }
    }
  } catch (e) {
    errors.push(`Chain check error: ${e instanceof Error ? e.message : String(e)}`);
  }

  // ---- Receipt verification ----
  try {
    const receiptRow = await db
      .prepare('SELECT * FROM receipts WHERE operation_id = ?')
      .bind(operationId)
      .first<Receipt>();

    if (receiptRow) {
      const receiptObject = await r2.get(receiptRow.r2_receipt_key);
      if (receiptObject) {
        const ear = JSON.parse(await receiptObject.text()) as EAR;

        // Recompute receipt hash
        const receiptFields = {
          receipt_version: ear.receipt_version,
          receipt_id: ear.receipt_id,
          operation_id: ear.operation_id,
          org_id: ear.org_id,
          agent_id: ear.agent_id,
          server_received_at: ear.server_received_at,
          seq_no: ear.seq_no,
          chain_hash: ear.chain_hash,
          queue_message_id: ear.queue_message_id,
        };

        const expectedHash = await computeReceiptHash(receiptFields);
        if (expectedHash === ear.receipt_hash) {
          receiptCheck = true;
        } else {
          errors.push('Receipt hash does not match recomputed value.');
        }
      } else {
        errors.push('Receipt evidence not found in R2.');
      }
    } else {
      errors.push('No receipt found for this operation.');
    }
  } catch (e) {
    errors.push(`Receipt check error: ${e instanceof Error ? e.message : String(e)}`);
  }

  // ---- Merkle inclusion verification ----
  let merkleCheck: boolean | undefined;

  // Find epoch by operation's created_at timestamp
  const epochRecord = await db
    .prepare('SELECT * FROM epochs WHERE org_id = ? AND start_time <= ? AND end_time > ?')
    .bind(orgId, operation.created_at, operation.created_at)
    .first<{ epoch_id: string; org_id: string; start_time: number; end_time: number; root_hash: string; leaf_count: number; r2_epoch_key: string; created_at: number }>();

  if (epochRecord) {
    try {
      const epochObj = await r2.get(epochRecord.r2_epoch_key);
      if (epochObj) {
        const epochData = JSON.parse(await epochObj.text()) as {
          merkle: { leafOps: string[]; layers: string[][] };
        };
        // Check if operation's chain_hash is in the leaf layer
        const leaves = epochData.merkle.layers[0];
        if (leaves && leaves.includes(operation.chain_hash)) {
          // Verify the tree root matches the epoch's root_hash
          const { getMerkleProof, verifyMerkleProof } = await import('../utils/merkle.js');
          const tree = {
            root: epochRecord.root_hash,
            leaves,
            leafOps: epochData.merkle.leafOps,
            layers: epochData.merkle.layers,
          };
          const proof = getMerkleProof(tree, operation.chain_hash);
          if (proof) {
            merkleCheck = await verifyMerkleProof(proof);
            if (!merkleCheck) {
              errors.push('Merkle inclusion proof verification failed.');
            }
          } else {
            merkleCheck = false;
            errors.push('Operation chain_hash found in leaves but proof generation failed.');
          }
        } else {
          merkleCheck = false;
          errors.push('Operation chain_hash not found in epoch Merkle tree leaves.');
        }
      }
    } catch {
      // If R2 read fails, leave merkle as undefined (pending)
    }
  }
  // If no epoch found, merkleCheck remains undefined (pending)

  const valid = signatureCheck && chainCheck && receiptCheck;

  return {
    valid,
    checks: {
      signature: signatureCheck,
      chain: chainCheck,
      receipt: receiptCheck,
      ...(merkleCheck !== undefined ? { merkle: merkleCheck } : {}),
    },
    errors: errors.length > 0 ? errors : undefined,
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Build the signable portion of an EOR (excludes the signature field itself).
 */
function buildSignableEOR(eor: EOR): Record<string, unknown> {
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

/**
 * Validate all required EOR fields before processing.
 */
function validateEORFields(eor: EOR, receivedAt: number): void {
  // Protocol version
  if (eor.op_version !== '1.0') {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.unsupportedVersion', params: { version: eor.op_version } });
  }

  // Required string fields
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

  // Nonce length
  if (eor.nonce.length > MAX_NONCE_LENGTH) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.nonceTooLong', params: { max: MAX_NONCE_LENGTH } });
  }

  // issued_at must be a number
  if (typeof eor.issued_at !== 'number' || eor.issued_at <= 0) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.invalidIssuedAt' });
  }

  // TTL bounds
  if (typeof eor.ttl_ms !== 'number') {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.missingTtl' });
  }
  if (eor.ttl_ms < MIN_TTL_MS) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.ttlTooLow', params: { min: MIN_TTL_MS } });
  }
  if (eor.ttl_ms > MAX_TTL_MS) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.ttlTooHigh', params: { max: MAX_TTL_MS } });
  }

  // TTL expiration check
  if (eor.issued_at + eor.ttl_ms < receivedAt) {
    throw new AppError(400, 'TTL_EXPIRED');
  }

  // Payload size check (approximate via JSON serialization)
  if (eor.payload !== null && eor.payload !== undefined) {
    const payloadStr = typeof eor.payload === 'string' ? eor.payload : JSON.stringify(eor.payload);
    if (new TextEncoder().encode(payloadStr).byteLength > MAX_PAYLOAD_SIZE) {
      throw new AppError(400, 'PAYLOAD_TOO_LARGE');
    }
  }
}
