/**
 * Agent service — business logic for agent lifecycle management.
 *
 * Handles registration, lookup, freezing, and key revocation. All
 * administrative mutations are recorded as admin_events for audit.
 */

import type {
  Agent,
  AgentKey,
  RegisterAgentRequest,
  RegisterAgentResponse,
  GetAgentResponse,
  ListAgentsResponse,
  AdminAction,
} from '../shared/index.js';
import { generateUUIDv7 } from '../utils/uuid.js';
import { base64urlDecode } from '../utils/crypto.js';
import { AppError } from '../middleware/error-handler.js';
import type { Database, PreparedStatement } from '../adapters/interfaces.js';

// ---------------------------------------------------------------------------
// Register agent
// ---------------------------------------------------------------------------

export async function registerAgent(
  db: Database,
  body: RegisterAgentRequest,
  orgId: string,
  actor: string,
): Promise<RegisterAgentResponse> {
  const now = Date.now();

  // Check if agent already exists
  const existing = await db
    .prepare('SELECT agent_id FROM agents WHERE agent_id = ?')
    .bind(body.agent_id)
    .first<{ agent_id: string }>();

  if (existing) {
    throw new AppError(409, 'VALIDATION_ERROR', { key: 'agent.alreadyExists', params: { id: body.agent_id } });
  }

  // Validate at least one key is provided
  if (!body.keys || body.keys.length === 0) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.atLeastOneKey' });
  }

  // Validate each public key is a valid 32-byte Ed25519 key
  for (const k of body.keys) {
    if (k.algorithm !== 'ed25519') {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.unsupportedAlgorithm', params: { algorithm: k.algorithm } });
    }
    try {
      const keyBytes = base64urlDecode(k.public_key);
      if (keyBytes.length !== 32) {
        throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.invalidPublicKeyLength', params: { kid: k.kid, expected: 32, actual: keyBytes.length } });
      }
    } catch (e) {
      if (e instanceof AppError) throw e;
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.invalidPublicKeyEncoding', params: { kid: k.kid } });
    }
  }

  // Build the agent record
  const agent: Agent = {
    agent_id: body.agent_id,
    org_id: orgId,
    display_name: body.display_name ?? body.agent_id,
    responsible_entity: body.responsible_entity ?? '',
    integration_type: body.integration_type ?? 'sdk',
    status: 'active',
    created_at: now,
    updated_at: now,
  };

  // Build key records
  const keys: AgentKey[] = body.keys.map((k) => ({
    kid: k.kid,
    agent_id: body.agent_id,
    public_key: k.public_key,
    algorithm: k.algorithm,
    status: 'active' as const,
    created_at: now,
    retired_at: null,
  }));

  // Use a batch to insert atomically
  const statements: PreparedStatement[] = [];

  // Insert agent
  statements.push(
    db
      .prepare(
        `INSERT INTO agents (agent_id, org_id, display_name, responsible_entity, integration_type, status, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .bind(
        agent.agent_id,
        agent.org_id,
        agent.display_name,
        agent.responsible_entity,
        agent.integration_type,
        agent.status,
        agent.created_at,
        agent.updated_at,
      ),
  );

  // Insert keys
  for (const key of keys) {
    statements.push(
      db
        .prepare(
          `INSERT INTO agent_keys (kid, agent_id, public_key, algorithm, status, created_at, retired_at)
           VALUES (?, ?, ?, ?, ?, ?, ?)`,
        )
        .bind(
          key.kid,
          key.agent_id,
          key.public_key,
          key.algorithm,
          key.status,
          key.created_at,
          key.retired_at,
        ),
    );
  }

  // Insert admin event
  const eventId = generateUUIDv7();
  const action: AdminAction = 'agent.register';
  statements.push(
    db
      .prepare(
        `INSERT INTO admin_events (event_id, org_id, actor, action, target_type, target_id, details, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .bind(
        eventId,
        orgId,
        actor,
        action,
        'agent',
        body.agent_id,
        JSON.stringify({ display_name: agent.display_name, integration_type: agent.integration_type, key_count: keys.length }),
        now,
      ),
  );

  await db.batch(statements);

  return { agent, keys };
}

// ---------------------------------------------------------------------------
// List agents
// ---------------------------------------------------------------------------

export async function listAgents(
  db: Database,
  orgId: string,
): Promise<ListAgentsResponse> {
  const result = await db
    .prepare('SELECT * FROM agents WHERE org_id = ? ORDER BY created_at DESC')
    .bind(orgId)
    .all<Agent>();

  return { agents: result.results ?? [] };
}

// ---------------------------------------------------------------------------
// Get agent
// ---------------------------------------------------------------------------

export async function getAgent(
  db: Database,
  agentId: string,
  orgId: string,
): Promise<GetAgentResponse> {
  const agentRow = await db
    .prepare('SELECT * FROM agents WHERE agent_id = ? AND org_id = ?')
    .bind(agentId, orgId)
    .first<Agent>();

  if (!agentRow) {
    throw new AppError(404, 'NOT_FOUND', { key: 'agent.notFound', params: { id: agentId } });
  }

  const keysResult = await db
    .prepare('SELECT * FROM agent_keys WHERE agent_id = ?')
    .bind(agentId)
    .all<AgentKey>();

  const keys = keysResult.results ?? [];

  return { agent: agentRow, keys };
}

// ---------------------------------------------------------------------------
// Update agent integration type
// ---------------------------------------------------------------------------

const VALID_INTEGRATION_TYPES = [
  'sdk', 'claudecode', 'cursor', 'gemini', 'kirocli', 'kiroide',
  'opencode', 'copilot', 'letta', 'codex', 'kimi',
  'enterprise', 'gui', 'other',
] as const;

export async function updateAgentIntegrationType(
  db: Database,
  agentId: string,
  integrationValue: string,
  orgId: string,
  actor: string,
): Promise<{ agent: Agent }> {
  const now = Date.now();

  const agentRow = await db
    .prepare('SELECT * FROM agents WHERE agent_id = ? AND org_id = ?')
    .bind(agentId, orgId)
    .first<Agent>();

  if (!agentRow) {
    throw new AppError(404, 'NOT_FOUND', { key: 'agent.notFound', params: { id: agentId } });
  }

  if (!(VALID_INTEGRATION_TYPES as readonly string[]).includes(integrationValue)) {
    throw new AppError(
      400,
      'VALIDATION_ERROR',
      { key: 'agent.invalidIntegrationType', params: { value: integrationValue, valid: VALID_INTEGRATION_TYPES.join(', ') } },
    );
  }

  const statements: PreparedStatement[] = [];

  statements.push(
    db
      .prepare('UPDATE agents SET integration_type = ?, updated_at = ? WHERE agent_id = ?')
      .bind(integrationValue, now, agentId),
  );

  const eventId = generateUUIDv7();
  const action: AdminAction = 'agent.update';
  statements.push(
    db
      .prepare(
        `INSERT INTO admin_events (event_id, org_id, actor, action, target_type, target_id, details, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .bind(
        eventId,
        orgId,
        actor,
        action,
        'agent',
        agentId,
        JSON.stringify({ integration_type: integrationValue, previous_integration_type: agentRow.integration_type }),
        now,
      ),
  );

  await db.batch(statements);

  const updatedAgent: Agent = {
    ...agentRow,
    integration_type: integrationValue,
    updated_at: now,
  };

  return { agent: updatedAgent };
}

// ---------------------------------------------------------------------------
// Freeze agent
// ---------------------------------------------------------------------------

export async function freezeAgent(
  db: Database,
  agentId: string,
  reason: string,
  orgId: string,
  actor: string,
): Promise<{ agent: Agent }> {
  const now = Date.now();

  // Look up the agent
  const agentRow = await db
    .prepare('SELECT * FROM agents WHERE agent_id = ? AND org_id = ?')
    .bind(agentId, orgId)
    .first<Agent>();

  if (!agentRow) {
    throw new AppError(404, 'NOT_FOUND', { key: 'agent.notFound', params: { id: agentId } });
  }

  if (agentRow.status === 'frozen') {
    throw new AppError(409, 'VALIDATION_ERROR', { key: 'agent.alreadyFrozen', params: { id: agentId } });
  }

  if (agentRow.status === 'revoked') {
    throw new AppError(409, 'VALIDATION_ERROR', { key: 'agent.permanentlyRevoked', params: { id: agentId } });
  }

  const statements: PreparedStatement[] = [];

  // Update agent status
  statements.push(
    db
      .prepare('UPDATE agents SET status = ?, updated_at = ? WHERE agent_id = ?')
      .bind('frozen', now, agentId),
  );

  // Log admin event
  const eventId = generateUUIDv7();
  const action: AdminAction = 'agent.freeze';
  statements.push(
    db
      .prepare(
        `INSERT INTO admin_events (event_id, org_id, actor, action, target_type, target_id, details, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .bind(eventId, orgId, actor, action, 'agent', agentId, JSON.stringify({ reason }), now),
  );

  await db.batch(statements);

  const updatedAgent: Agent = {
    ...agentRow,
    status: 'frozen',
    updated_at: now,
  };

  return { agent: updatedAgent };
}

// ---------------------------------------------------------------------------
// Unfreeze agent
// ---------------------------------------------------------------------------

export async function unfreezeAgent(
  db: Database,
  agentId: string,
  reason: string,
  orgId: string,
  actor: string,
): Promise<{ agent: Agent }> {
  const now = Date.now();

  const agentRow = await db
    .prepare('SELECT * FROM agents WHERE agent_id = ? AND org_id = ?')
    .bind(agentId, orgId)
    .first<Agent>();

  if (!agentRow) {
    throw new AppError(404, 'NOT_FOUND', { key: 'agent.notFound', params: { id: agentId } });
  }

  if (agentRow.status === 'active') {
    throw new AppError(409, 'VALIDATION_ERROR', { key: 'agent.notFrozen', params: { id: agentId } });
  }

  if (agentRow.status === 'revoked') {
    throw new AppError(409, 'VALIDATION_ERROR', { key: 'agent.permanentlyRevokedCannotUnfreeze', params: { id: agentId } });
  }

  const statements: PreparedStatement[] = [];

  statements.push(
    db
      .prepare('UPDATE agents SET status = ?, updated_at = ? WHERE agent_id = ?')
      .bind('active', now, agentId),
  );

  const eventId = generateUUIDv7();
  const action: AdminAction = 'agent.unfreeze';
  statements.push(
    db
      .prepare(
        `INSERT INTO admin_events (event_id, org_id, actor, action, target_type, target_id, details, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .bind(eventId, orgId, actor, action, 'agent', agentId, JSON.stringify({ reason }), now),
  );

  await db.batch(statements);

  const updatedAgent: Agent = {
    ...agentRow,
    status: 'active',
    updated_at: now,
  };

  return { agent: updatedAgent };
}

// ---------------------------------------------------------------------------
// Delete agent
// ---------------------------------------------------------------------------

export async function deleteAgent(
  db: Database,
  agentId: string,
  orgId: string,
  actor: string,
): Promise<void> {
  const now = Date.now();

  // Verify the agent belongs to this org
  const agentRow = await db
    .prepare('SELECT * FROM agents WHERE agent_id = ? AND org_id = ?')
    .bind(agentId, orgId)
    .first<Agent>();

  if (!agentRow) {
    throw new AppError(404, 'NOT_FOUND', { key: 'agent.notFound', params: { id: agentId } });
  }

  const statements: PreparedStatement[] = [];

  // Delete receipts for this agent's operations (receipts FK → operations)
  statements.push(
    db.prepare(
      'DELETE FROM receipts WHERE operation_id IN (SELECT operation_id FROM operations WHERE agent_id = ?)',
    ).bind(agentId),
  );

  // Delete operations (operations FK → agents)
  statements.push(
    db.prepare('DELETE FROM operations WHERE agent_id = ?').bind(agentId),
  );

  // Delete associated keys (agent_keys FK → agents)
  statements.push(
    db.prepare('DELETE FROM agent_keys WHERE agent_id = ?').bind(agentId),
  );

  // Delete the agent
  statements.push(
    db.prepare('DELETE FROM agents WHERE agent_id = ? AND org_id = ?').bind(agentId, orgId),
  );

  // Log admin event
  const eventId = generateUUIDv7();
  const action: AdminAction = 'agent.delete';
  statements.push(
    db
      .prepare(
        `INSERT INTO admin_events (event_id, org_id, actor, action, target_type, target_id, details, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .bind(
        eventId,
        orgId,
        actor,
        action,
        'agent',
        agentId,
        JSON.stringify({ display_name: agentRow.display_name }),
        now,
      ),
  );

  await db.batch(statements);
}

// ---------------------------------------------------------------------------
// Revoke key
// ---------------------------------------------------------------------------

export async function revokeKey(
  db: Database,
  agentId: string,
  kid: string,
  reason: string,
  orgId: string,
  actor: string,
): Promise<{ key: AgentKey }> {
  const now = Date.now();

  // Verify the agent belongs to this org
  const agentRow = await db
    .prepare('SELECT * FROM agents WHERE agent_id = ? AND org_id = ?')
    .bind(agentId, orgId)
    .first<Agent>();

  if (!agentRow) {
    throw new AppError(404, 'NOT_FOUND', { key: 'agent.notFound', params: { id: agentId } });
  }

  // Look up the key
  const keyRow = await db
    .prepare('SELECT * FROM agent_keys WHERE kid = ? AND agent_id = ?')
    .bind(kid, agentId)
    .first<AgentKey>();

  if (!keyRow) {
    throw new AppError(404, 'NOT_FOUND', { key: 'agent.keyNotFound', params: { kid, id: agentId } });
  }

  if (keyRow.status === 'revoked') {
    throw new AppError(409, 'VALIDATION_ERROR', { key: 'agent.keyAlreadyRevoked', params: { kid } });
  }

  const statements: PreparedStatement[] = [];

  // Revoke the key
  statements.push(
    db
      .prepare('UPDATE agent_keys SET status = ?, retired_at = ? WHERE kid = ?')
      .bind('revoked', now, kid),
  );

  // Log admin event
  const eventId = generateUUIDv7();
  const action: AdminAction = 'key.revoke';
  statements.push(
    db
      .prepare(
        `INSERT INTO admin_events (event_id, org_id, actor, action, target_type, target_id, details, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .bind(
        eventId,
        orgId,
        actor,
        action,
        'agent_key',
        kid,
        JSON.stringify({ agent_id: agentId, reason }),
        now,
      ),
  );

  await db.batch(statements);

  const updatedKey: AgentKey = {
    ...keyRow,
    status: 'revoked',
    retired_at: now,
  };

  return { key: updatedKey };
}
