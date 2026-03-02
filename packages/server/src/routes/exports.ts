/**
 * Export routes — create and retrieve compliance export jobs.
 */

import { Hono } from 'hono';
import type { Env, AppVariables } from '../types.js';
import type { CreateExportRequest } from '../shared/index.js';
import { authMiddleware } from '../middleware/auth.js';
import { requireRole } from '../middleware/rbac.js';
import { AppError } from '../middleware/error-handler.js';
import * as exportService from '../services/export-service.js';

const exports_ = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// All export routes require authentication
exports_.use('/*', authMiddleware);

// ---------------------------------------------------------------------------
// GET /v1/exports — List all exports for the organization
// ---------------------------------------------------------------------------
exports_.get('/', requireRole('compliance_auditor'), async (c) => {
  const orgId = c.get('org_id');
  const result = await exportService.listExports(c.env.ELYDORA_DB, orgId);
  c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
  return c.json(result, 200);
});

// ---------------------------------------------------------------------------
// POST /v1/exports — Create a compliance export job
// ---------------------------------------------------------------------------
exports_.post(
  '/',
  requireRole('compliance_auditor'),
  async (c) => {
    const body = await c.req.json<CreateExportRequest>();

    if (!body) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'export.bodyRequired' });
    }

    const orgId = c.get('org_id');
    const actor = c.get('actor');

    const result = await exportService.createExport(
      c.env.ELYDORA_DB,
      c.env.ELYDORA_QUEUE,
      body,
      orgId,
      actor,
    );

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 201);
  },
);

// ---------------------------------------------------------------------------
// GET /v1/exports/:export_id — Retrieve export status + download URL
// ---------------------------------------------------------------------------
exports_.get(
  '/:export_id',
  requireRole('compliance_auditor'),
  async (c) => {
    const exportId = c.req.param('export_id');
    const orgId = c.get('org_id');

    const result = await exportService.getExport(
      c.env.ELYDORA_DB,
      c.env.ELYDORA_EVIDENCE,
      exportId,
      orgId,
    );

    c.header('X-Elydora-Protocol-Version', c.env.PROTOCOL_VERSION);
    return c.json(result, 200);
  },
);

// ---------------------------------------------------------------------------
// GET /v1/exports/:export_id/download — Stream the export file from R2
// ---------------------------------------------------------------------------
exports_.get(
  '/:export_id/download',
  requireRole('compliance_auditor'),
  async (c) => {
    const exportId = c.req.param('export_id');
    const orgId = c.get('org_id');

    // Fetch the export record
    const exportRecord = await c.env.ELYDORA_DB
      .prepare('SELECT * FROM exports WHERE export_id = ? AND org_id = ?')
      .bind(exportId, orgId)
      .first<{ status: string; r2_export_key: string | null; query_params: string }>();

    if (!exportRecord) {
      throw new AppError(404, 'NOT_FOUND', { key: 'export.notFoundById', params: { id: exportId } });
    }

    if (exportRecord.status !== 'done' || !exportRecord.r2_export_key) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'export.notYetComplete' });
    }

    const object = await c.env.ELYDORA_EVIDENCE.get(exportRecord.r2_export_key);
    if (!object) {
      throw new AppError(404, 'NOT_FOUND', { key: 'export.fileNotFound' });
    }

    // Determine file extension from the stored query_params
    let ext = '';
    const qp = exportRecord as { query_params?: string };
    if (qp.query_params) {
      try {
        const params = JSON.parse(qp.query_params);
        ext = params.format === 'pdf' ? '.pdf' : '.json';
      } catch { /* ignore */ }
    }

    const headers = new Headers();
    headers.set('Content-Type', object.httpMetadata?.contentType ?? 'application/octet-stream');
    headers.set('Content-Disposition', `attachment; filename="export-${exportId}${ext}"`);

    if (object.size !== undefined) {
      headers.set('Content-Length', String(object.size));
    }

    return new Response(object.body, { headers });
  },
);

export { exports_ };
