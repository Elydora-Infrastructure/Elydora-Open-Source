/**
 * Operation routes — submit, retrieve, and verify signed operation records.
 */

import { Hono } from 'hono';
import type { Env, AppVariables } from '../types.js';
import type { EOR } from '../shared/index.js';
import { authMiddleware } from '../middleware/auth.js';
import { requireRole } from '../middleware/rbac.js';
import { AppError } from '../middleware/error-handler.js';
import * as operationService from '../services/operation-service.js';

const operations = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// ---------------------------------------------------------------------------
// POST /v1/operations — Submit a signed EOR
// ---------------------------------------------------------------------------
// Operation submission uses the EOR's embedded signature for authentication
// rather than a Bearer JWT. The agent authenticates via its Ed25519 signature.
// We still apply auth middleware to get the org context.
operations.post(
  '/',
  authMiddleware,
  requireRole('integration_engineer'),
  async (c) => {
    const eorBody = await c.req.json<EOR>();

    const orgId = c.get('org_id');
    const eor = { ...eorBody, org_id: orgId };

    if (!eor || !eor.operation_id) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'operation.invalidBody' });
    }

    const result = await operationService.submitOperation(
      c.env.ELYDORA_DB,
      c.env.ELYDORA_EVIDENCE,
      c.env.ELYDORA_CACHE,
      c.env.ELYDORA_QUEUE,
      eor,
      c.env.ELYDORA_SIGNING_KEY,
    );

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 202);
  },
);

// ---------------------------------------------------------------------------
// GET /v1/operations/:operation_id — Retrieve an operation
// ---------------------------------------------------------------------------
operations.get(
  '/:operation_id',
  authMiddleware,
  requireRole('readonly_investigator'),
  async (c) => {
    const operationId = c.req.param('operation_id');
    const orgId = c.get('org_id');

    const result = await operationService.getOperation(c.env.ELYDORA_DB, c.env.ELYDORA_EVIDENCE, operationId, orgId);

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

// ---------------------------------------------------------------------------
// POST /v1/operations/:operation_id/verify — Verify operation integrity
// ---------------------------------------------------------------------------
operations.post(
  '/:operation_id/verify',
  authMiddleware,
  requireRole('readonly_investigator'),
  async (c) => {
    const operationId = c.req.param('operation_id');
    const orgId = c.get('org_id');

    const result = await operationService.verifyOperation(
      c.env.ELYDORA_DB,
      c.env.ELYDORA_EVIDENCE,
      operationId,
      orgId,
    );

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

export { operations };
