/**
 * Epoch routes — retrieve epoch root records with optional TSA anchors.
 */

import { Hono } from 'hono';
import type { Env, AppVariables } from '../types.js';
import { authMiddleware } from '../middleware/auth.js';
import { requireRole } from '../middleware/rbac.js';
import * as epochService from '../services/epoch-service.js';

const epochs = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// All epoch routes require authentication
epochs.use('/*', authMiddleware);

// ---------------------------------------------------------------------------
// GET /v1/epochs — List all epochs for the organization
// ---------------------------------------------------------------------------
epochs.get('/', requireRole('readonly_investigator'), async (c) => {
  const orgId = c.get('org_id');
  const result = await epochService.listEpochs(c.env.ELYDORA_DB, orgId);
  c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
  return c.json(result, 200);
});

// ---------------------------------------------------------------------------
// GET /v1/epochs/:epoch_id — Retrieve an epoch root
// ---------------------------------------------------------------------------
epochs.get(
  '/:epoch_id',
  requireRole('readonly_investigator'),
  async (c) => {
    const epochId = c.req.param('epoch_id');
    const orgId = c.get('org_id');

    const result = await epochService.getEpoch(
      c.env.ELYDORA_DB,
      c.env.ELYDORA_EVIDENCE,
      epochId,
      orgId,
    );

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

export { epochs };
