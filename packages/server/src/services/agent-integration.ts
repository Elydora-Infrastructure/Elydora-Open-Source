import {
  INTEGRATION_TYPES,
  type AdminAction,
  type Agent,
  type IntegrationType,
} from '../shared/index.js';
import type { Database, PreparedStatement } from '../adapters/interfaces.js';
import { AppError } from '../middleware/error-handler.js';
import { generateUUIDv7 } from '../utils/uuid.js';

export function requireIntegrationType(value: unknown): IntegrationType {
  if (typeof value !== 'string' || value.length === 0) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.missingIntegrationType' });
  }
  if (!(INTEGRATION_TYPES as readonly string[]).includes(value)) {
    throw new AppError(
      400,
      'VALIDATION_ERROR',
      {
        key: 'agent.invalidIntegrationType',
        params: { value, valid: INTEGRATION_TYPES.join(', ') },
      },
    );
  }
  return value as IntegrationType;
}

export async function updateAgentIntegrationType(
  db: Database,
  agentId: string,
  integrationValue: unknown,
  orgId: string,
  actor: string,
): Promise<{ agent: Agent }> {
  const integrationType = requireIntegrationType(integrationValue);
  const now = Date.now();

  const agentRow = await db
    .prepare('SELECT * FROM agents WHERE agent_id = ? AND org_id = ?')
    .bind(agentId, orgId)
    .first<Agent>();

  if (!agentRow) {
    throw new AppError(404, 'NOT_FOUND', { key: 'agent.notFound', params: { id: agentId } });
  }

  const statements: PreparedStatement[] = [];
  statements.push(
    db
      .prepare('UPDATE agents SET integration_type = ?, updated_at = ? WHERE agent_id = ? AND org_id = ?')
      .bind(integrationType, now, agentId, orgId),
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
        JSON.stringify({
          integration_type: integrationType,
          previous_integration_type: agentRow.integration_type,
        }),
        now,
      ),
  );

  await db.batch(statements);

  return {
    agent: {
      ...agentRow,
      integration_type: integrationType,
      updated_at: now,
    },
  };
}
