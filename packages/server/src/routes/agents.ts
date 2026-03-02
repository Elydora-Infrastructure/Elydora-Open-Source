/**
 * Agent routes — registration, lookup, freezing, and key revocation.
 */

import { Hono } from 'hono';
import type { Env, AppVariables } from '../types.js';
import type { RegisterAgentRequest, UpdateAgentRequest, FreezeAgentRequest, UnfreezeAgentRequest, RevokeAgentRequest } from '../shared/index.js';
import { authMiddleware } from '../middleware/auth.js';
import { requireRole } from '../middleware/rbac.js';
import { AppError } from '../middleware/error-handler.js';
import * as agentService from '../services/agent-service.js';

const agents = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// All agent routes require authentication
agents.use('/*', authMiddleware);

// ---------------------------------------------------------------------------
// POST /v1/agents/register
// ---------------------------------------------------------------------------
agents.post(
  '/register',
  requireRole('integration_engineer'),
  async (c) => {
    const body = await c.req.json<RegisterAgentRequest>();

    if (!body || !body.agent_id) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.missingAgentId' });
    }

    const orgId = c.get('org_id');
    const actor = c.get('actor');

    const result = await agentService.registerAgent(c.env.ELYDORA_DB, body, orgId, actor);

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 201);
  },
);

// ---------------------------------------------------------------------------
// GET /v1/agents
// ---------------------------------------------------------------------------
agents.get(
  '/',
  requireRole('readonly_investigator'),
  async (c) => {
    const orgId = c.get('org_id');
    const result = await agentService.listAgents(c.env.ELYDORA_DB, orgId);

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

// ---------------------------------------------------------------------------
// GET /v1/agents/:agent_id
// ---------------------------------------------------------------------------
agents.get(
  '/:agent_id',
  requireRole('readonly_investigator'),
  async (c) => {
    const agentId = c.req.param('agent_id');
    const orgId = c.get('org_id');

    const result = await agentService.getAgent(c.env.ELYDORA_DB, agentId, orgId);

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

// ---------------------------------------------------------------------------
// DELETE /v1/agents/:agent_id
// ---------------------------------------------------------------------------
agents.delete(
  '/:agent_id',
  requireRole('security_admin'),
  async (c) => {
    const agentId = c.req.param('agent_id');
    const orgId = c.get('org_id');
    const actor = c.get('actor');

    await agentService.deleteAgent(c.env.ELYDORA_DB, agentId, orgId, actor);

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json({ deleted: true }, 200);
  },
);

// ---------------------------------------------------------------------------
// PATCH /v1/agents/:agent_id
// ---------------------------------------------------------------------------
agents.patch(
  '/:agent_id',
  requireRole('integration_engineer'),
  async (c) => {
    const agentId = c.req.param('agent_id');
    const body = await c.req.json<UpdateAgentRequest>();

    if (!body || !body.integration_type || typeof body.integration_type !== 'string') {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.missingIntegrationType' });
    }

    const orgId = c.get('org_id');
    const actor = c.get('actor');

    const result = await agentService.updateAgentIntegrationType(
      c.env.ELYDORA_DB,
      agentId,
      body.integration_type,
      orgId,
      actor,
    );

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

// ---------------------------------------------------------------------------
// POST /v1/agents/:agent_id/freeze
// ---------------------------------------------------------------------------
agents.post(
  '/:agent_id/freeze',
  requireRole('security_admin'),
  async (c) => {
    const agentId = c.req.param('agent_id');
    const body = await c.req.json<FreezeAgentRequest>();

    if (!body || !body.reason || typeof body.reason !== 'string' || body.reason.trim().length === 0) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.missingReason' });
    }

    const orgId = c.get('org_id');
    const actor = c.get('actor');

    const result = await agentService.freezeAgent(
      c.env.ELYDORA_DB,
      agentId,
      body.reason,
      orgId,
      actor,
    );

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

// ---------------------------------------------------------------------------
// POST /v1/agents/:agent_id/unfreeze
// ---------------------------------------------------------------------------
agents.post(
  '/:agent_id/unfreeze',
  requireRole('security_admin'),
  async (c) => {
    const agentId = c.req.param('agent_id');
    const body = await c.req.json<UnfreezeAgentRequest>();

    if (!body || !body.reason || typeof body.reason !== 'string' || body.reason.trim().length === 0) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.missingReason' });
    }

    const orgId = c.get('org_id');
    const actor = c.get('actor');

    const result = await agentService.unfreezeAgent(
      c.env.ELYDORA_DB,
      agentId,
      body.reason,
      orgId,
      actor,
    );

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

// ---------------------------------------------------------------------------
// POST /v1/agents/:agent_id/revoke
// ---------------------------------------------------------------------------
agents.post(
  '/:agent_id/revoke',
  requireRole('security_admin'),
  async (c) => {
    const agentId = c.req.param('agent_id');
    const body = await c.req.json<RevokeAgentRequest>();

    if (!body || !body.kid || typeof body.kid !== 'string') {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.missingKid' });
    }

    if (!body.reason || typeof body.reason !== 'string' || body.reason.trim().length === 0) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'agent.missingReason' });
    }

    const orgId = c.get('org_id');
    const actor = c.get('actor');

    const result = await agentService.revokeKey(
      c.env.ELYDORA_DB,
      agentId,
      body.kid,
      body.reason,
      orgId,
      actor,
    );

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

export { agents };
