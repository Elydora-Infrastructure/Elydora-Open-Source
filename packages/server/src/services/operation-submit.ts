import type { EOR, EAR, Agent, AgentKey, Operation } from '../shared/index.js';
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
import { ELYDORA_KID, GENESIS_CHAIN_HASH, buildSignableEOR, validateEORFields } from './operation-eor.js';

export async function submitOperation(
  db: Database,
  r2: ObjectStore,
  kv: Cache,
  queue: MessageQueue,
  eor: EOR,
  signingKey: string,
): Promise<{ receipt: EAR }> {
  const receivedAt = Date.now();

  // Step 1: Validate EOR fields
  validateEORFields(eor, receivedAt);

  // Step 2: Replay detection via nonce in KV
  const nonceKey = `nonce:${eor.org_id}:${eor.nonce}`;
  const existingNonce = await kv.get(nonceKey);
  if (existingNonce) {
    throw new AppError(400, 'REPLAY_DETECTED');
  }
  // Store nonce with TTL equal to the operation's TTL (in seconds)
  const nonceTtlSeconds = Math.ceil(eor.ttl_ms / 1000);
  await kv.put(nonceKey, '1', { expirationTtl: nonceTtlSeconds });

  // Step 3: Look up agent and check status
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
    throw new AppError(403, 'AGENT_REVOKED', { key: 'operation.agentRevoked' });
  }

  // Step 4: Look up key and check status
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
    throw new AppError(403, 'KEY_RETIRED', { key: 'operation.keyRetired' });
  }

  // Step 5: Verify Ed25519 signature
  const signableEOR = buildSignableEOR(eor);
  const canonicalData = jcsCanonicalise(signableEOR);
  const dataBytes = new TextEncoder().encode(canonicalData);
  const signatureBytes = base64urlDecode(eor.signature);
  const publicKeyBytes = base64urlDecode(agentKey.public_key);

  const signatureValid = await verifyEd25519Signature(publicKeyBytes, signatureBytes, dataBytes);

  if (!signatureValid) {
    throw new AppError(400, 'INVALID_SIGNATURE');
  }

  // Step 6: Verify prev_chain_hash against the latest chain_hash
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

  // Step 7: Compute new chain_hash
  const chainHash = await computeChainHash(
    eor.prev_chain_hash,
    eor.payload_hash,
    eor.operation_id,
    eor.issued_at,
  );

  // Step 8: Store operation in D1
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

  // Step 9: Store the full EOR envelope in R2
  const eorJson = JSON.stringify(eor);
  await r2.put(r2PayloadKey, eorJson, {
    httpMetadata: { contentType: 'application/json' },
    customMetadata: {
      org_id: eor.org_id,
      agent_id: eor.agent_id,
      operation_type: eor.operation_type,
    },
  });

  // Step 10: Reserve the queue message ID; the send follows the commit
  const queueMessageId = generateUUIDv7();
  const receiptId = generateUUIDv7();

  // Step 11: Generate EAR receipt
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

  // Step 12: Persist operation and receipt to D1
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

  try {
    await db.batch(statements);
  } catch (error) {
    if (!isSeqNoConflict(error)) throw error;
    const winner = await db
      .prepare('SELECT chain_hash FROM operations WHERE agent_id = ? ORDER BY seq_no DESC LIMIT 1')
      .bind(eor.agent_id)
      .first<{ chain_hash: string }>();
    if (!winner) throw new Error('seq_no conflict without a stored operation.');
    throw new AppError(400, 'PREV_HASH_MISMATCH', {
      key: 'operation.prevHashMismatch',
      params: { expected: winner.chain_hash, actual: eor.prev_chain_hash },
    });
  }

  try {
    await queue.send(queueMessageId, {
      type: 'operation',
      operation_id: eor.operation_id,
      org_id: eor.org_id,
      agent_id: eor.agent_id,
    });
  } catch (error) {
    console.error('operation.enqueue_failed', {
      operation_id: eor.operation_id,
      queue_message_id: queueMessageId,
      error: error instanceof Error ? error.message : String(error),
    });
  }

  return { receipt: ear };
}

function isSeqNoConflict(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false;
  const { code, constraint } = error as { code?: unknown; constraint?: unknown };
  return code === '23505' && constraint === 'operations_agent_id_seq_no_key';
}
