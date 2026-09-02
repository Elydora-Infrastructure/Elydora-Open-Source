import assert from 'node:assert/strict';
import { generateKeyPairSync, randomUUID } from 'node:crypto';
import test from 'node:test';

import { AppError } from '../dist/middleware/error-handler.js';
import { submitOperation } from '../dist/services/operation-service.js';
import { jcsCanonicalise, sha256Base64url, signEd25519 } from '../dist/utils/crypto.js';

const GENESIS = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA';
const WINNER = 'Rxlf4j36C3KvIQ3hWuOkX698BR5iDypUFuB70JjEuvM';

function keyPair() {
  const pair = generateKeyPairSync('ed25519');
  return {
    seed: pair.privateKey.export({ format: 'jwk' }).d,
    publicKey: pair.publicKey.export({ format: 'jwk' }).x,
  };
}

async function signedEor(agentId, orgId, kid, seed) {
  const payload = { tool: 'Bash' };
  const unsigned = {
    op_version: '1.0',
    operation_id: randomUUID(),
    org_id: orgId,
    agent_id: agentId,
    issued_at: Date.now(),
    ttl_ms: 30_000,
    nonce: randomUUID(),
    operation_type: 'ai.tool_use',
    subject: { session_id: 'unit' },
    action: { tool: 'Bash' },
    payload,
    payload_hash: await sha256Base64url(jcsCanonicalise(payload)),
    prev_chain_hash: GENESIS,
    agent_pubkey_kid: kid,
  };
  const signature = await signEd25519(seed, new TextEncoder().encode(jcsCanonicalise(unsigned)));
  return { ...unsigned, signature };
}

function pgConflict() {
  return Object.assign(new Error('duplicate key value violates unique constraint "operations_agent_id_seq_no_key"'), {
    code: '23505',
    constraint: 'operations_agent_id_seq_no_key',
  });
}

function fakes(batchError, agentId, orgId, kid, publicKey) {
  const sent = [];
  const db = {
    prepare: (sql) => ({
      bind: () => ({
        first: async () => {
          if (sql.includes('FROM agents')) return { agent_id: agentId, org_id: orgId, status: 'active' };
          if (sql.includes('FROM agent_keys')) return { kid, agent_id: agentId, public_key: publicKey, status: 'active' };
          if (sql.includes('SELECT chain_hash, seq_no')) return null;
          if (sql.includes('SELECT chain_hash FROM operations')) return { chain_hash: WINNER };
          throw new Error(`unexpected query: ${sql}`);
        },
      }),
    }),
    batch: async () => {
      if (batchError) throw batchError;
      return [];
    },
  };
  const r2 = { put: async () => undefined };
  const kv = { get: async () => null, put: async () => undefined };
  const queue = { send: async (messageId, body) => { sent.push({ messageId, body }); } };
  return { db, r2, kv, queue, sent };
}

test('a seq_no conflict on insert reports the winning chain hash as PREV_HASH_MISMATCH', async () => {
  const agent = keyPair();
  const server = keyPair();
  const eor = await signedEor('agent-conflict', 'org-conflict', 'agent-conflict-key-1', agent.seed);
  const { db, r2, kv, queue, sent } = fakes(pgConflict(), 'agent-conflict', 'org-conflict', 'agent-conflict-key-1', agent.publicKey);

  await assert.rejects(
    submitOperation(db, r2, kv, queue, eor, server.seed),
    (error) => error instanceof AppError
      && error.statusCode === 400
      && error.errorCode === 'PREV_HASH_MISMATCH'
      && error.message.includes(`Expected prev_chain_hash "${WINNER}"`),
  );
  assert.equal(sent.length, 0);
});

test('other insert failures propagate unchanged and nothing is enqueued', async () => {
  const agent = keyPair();
  const server = keyPair();
  const eor = await signedEor('agent-conflict', 'org-conflict', 'agent-conflict-key-1', agent.seed);
  const { db, r2, kv, queue, sent } = fakes(new Error('connection reset'), 'agent-conflict', 'org-conflict', 'agent-conflict-key-1', agent.publicKey);

  await assert.rejects(
    submitOperation(db, r2, kv, queue, eor, server.seed),
    (error) => error instanceof Error && !(error instanceof AppError) && error.message === 'connection reset',
  );
  assert.equal(sent.length, 0);
});

test('a committed operation is enqueued with its reserved queue message id', async () => {
  const agent = keyPair();
  const server = keyPair();
  const eor = await signedEor('agent-conflict', 'org-conflict', 'agent-conflict-key-1', agent.seed);
  const { db, r2, kv, queue, sent } = fakes(null, 'agent-conflict', 'org-conflict', 'agent-conflict-key-1', agent.publicKey);

  const { receipt } = await submitOperation(db, r2, kv, queue, eor, server.seed);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].messageId, receipt.queue_message_id);
  assert.equal(sent[0].body.operation_id, eor.operation_id);
});
