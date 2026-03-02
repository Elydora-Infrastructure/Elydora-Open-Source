/**
 * Audit routes — query the tamper-evident audit log.
 *
 * Per the OpenAPI spec, audit queries use POST /v1/audit/query with
 * filter parameters in the request body. This avoids URL length limits
 * and supports complex filter expressions.
 */

import { Hono } from 'hono';
import type { Env, AppVariables } from '../types.js';
import type { AuditQueryRequest } from '../shared/index.js';
import { authMiddleware } from '../middleware/auth.js';
import { requireRole } from '../middleware/rbac.js';
import * as auditService from '../services/audit-service.js';

const audit = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// All audit routes require authentication and at least compliance_auditor role
audit.use('/*', authMiddleware);

// ---------------------------------------------------------------------------
// POST /v1/audit/query — Query the audit log
// ---------------------------------------------------------------------------
audit.post(
  '/query',
  requireRole('compliance_auditor'),
  async (c) => {
    const body = await c.req.json<AuditQueryRequest>();
    const orgId = c.get('org_id');

    const result = await auditService.queryAudit(c.env.ELYDORA_DB, body ?? {}, orgId);

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

export { audit };
