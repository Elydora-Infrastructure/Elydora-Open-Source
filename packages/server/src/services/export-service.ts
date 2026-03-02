/**
 * Export service — business logic for compliance export job management.
 *
 * Exports are asynchronous jobs that package operations matching specified
 * criteria into a downloadable bundle (JSON or PDF). The job is enqueued
 * via a message queue and processed by the queue consumer. Once complete,
 * the result is stored in the object store and the database record is
 * updated with a reference to the object key.
 */

import type { Export, CreateExportRequest, CreateExportResponse, GetExportResponse, ListExportsResponse } from '../shared/index.js';
import { generateUUIDv7 } from '../utils/uuid.js';
import { AppError } from '../middleware/error-handler.js';
import type { Database, MessageQueue, ObjectStore } from '../adapters/interfaces.js';

// ---------------------------------------------------------------------------
// Create export
// ---------------------------------------------------------------------------

export async function createExport(
  db: Database,
  queue: MessageQueue,
  body: CreateExportRequest,
  orgId: string,
  actor: string,
): Promise<CreateExportResponse> {
  const now = Date.now();

  // Validate time range
  if (typeof body.start_time !== 'number' || body.start_time <= 0) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'export.invalidStartTime' });
  }
  if (typeof body.end_time !== 'number' || body.end_time <= 0) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'export.invalidEndTime' });
  }
  if (body.start_time >= body.end_time) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'export.startBeforeEnd' });
  }

  // Validate format
  if (body.format !== 'json' && body.format !== 'pdf') {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'export.invalidFormat' });
  }

  const exportId = generateUUIDv7();

  const queryParams = JSON.stringify({
    start_time: body.start_time,
    end_time: body.end_time,
    agent_id: body.agent_id ?? null,
    operation_type: body.operation_type ?? null,
    format: body.format,
  });

  const exportRecord: Export = {
    export_id: exportId,
    org_id: orgId,
    status: 'queued',
    query_params: queryParams,
    r2_export_key: null,
    created_at: now,
    completed_at: null,
  };

  // Insert export record
  await db
    .prepare(
      `INSERT INTO exports (export_id, org_id, status, query_params, r2_export_key, created_at, completed_at)
       VALUES (?, ?, ?, ?, ?, ?, ?)`,
    )
    .bind(
      exportRecord.export_id,
      exportRecord.org_id,
      exportRecord.status,
      exportRecord.query_params,
      exportRecord.r2_export_key,
      exportRecord.created_at,
      exportRecord.completed_at,
    )
    .run();

  // Log admin event
  const eventId = generateUUIDv7();
  await db
    .prepare(
      `INSERT INTO admin_events (event_id, org_id, actor, action, target_type, target_id, details, created_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
    )
    .bind(
      eventId,
      orgId,
      actor,
      'export.create',
      'export',
      exportId,
      queryParams,
      now,
    )
    .run();

  // Enqueue the export job
  await queue.send({
    type: 'export',
    export_id: exportId,
    org_id: orgId,
    query_params: queryParams,
  });

  return { export: exportRecord };
}

// ---------------------------------------------------------------------------
// List exports
// ---------------------------------------------------------------------------

export async function listExports(
  db: Database,
  orgId: string,
): Promise<ListExportsResponse> {
  const { results } = await db
    .prepare('SELECT * FROM exports WHERE org_id = ? ORDER BY created_at DESC')
    .bind(orgId)
    .all<Export>();
  return { exports: results ?? [] };
}

// ---------------------------------------------------------------------------
// Get export
// ---------------------------------------------------------------------------

export async function getExport(
  db: Database,
  r2: ObjectStore,
  exportId: string,
  orgId: string,
): Promise<GetExportResponse> {
  const exportRecord = await db
    .prepare('SELECT * FROM exports WHERE export_id = ? AND org_id = ?')
    .bind(exportId, orgId)
    .first<Export>();

  if (!exportRecord) {
    throw new AppError(404, 'NOT_FOUND', { key: 'export.notFound', params: { id: exportId } });
  }

  let downloadUrl: string | undefined;

  // If the export is complete and has an object store key, provide a download path.
  // The client can use a dedicated download endpoint to stream the file.
  if (exportRecord.status === 'done' && exportRecord.r2_export_key) {
    // Verify the object actually exists in R2
    const head = await r2.head(exportRecord.r2_export_key);
    if (head) {
      // Construct a download path that the client can use
      downloadUrl = `/v1/exports/${exportId}/download`;
    }
  }

  return {
    export: exportRecord,
    download_url: downloadUrl,
  };
}
