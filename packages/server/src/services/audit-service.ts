/**
 * Audit service — business logic for querying the tamper-evident audit log.
 *
 * Builds dynamic SQL queries with optional filters (org_id, agent_id,
 * operation_type, time range) and supports cursor-based pagination for
 * efficient traversal of large result sets.
 */

import type { Operation, AuditQueryRequest, AuditQueryResponse } from '../shared/index.js';
import { DEFAULT_QUERY_LIMIT, MAX_QUERY_LIMIT } from '../shared/index.js';
import { decodeCursor, encodeCursor } from '../utils/pagination.js';
import { AppError } from '../middleware/error-handler.js';
import type { Database } from '../adapters/interfaces.js';

// ---------------------------------------------------------------------------
// Query audit log
// ---------------------------------------------------------------------------

export async function queryAudit(
  db: Database,
  params: AuditQueryRequest,
  orgId: string,
): Promise<AuditQueryResponse> {
  // Validate and clamp limit
  const limit = Math.min(
    Math.max(params.limit ?? DEFAULT_QUERY_LIMIT, 1),
    MAX_QUERY_LIMIT,
  );

  // Build WHERE clauses and bind values
  const conditions: string[] = [];
  const bindings: (string | number)[] = [];

  // Always scope to the caller's org
  conditions.push('org_id = ?');
  bindings.push(orgId);

  // Optional agent filter (further restrict if the request specifies an org_id matching theirs)
  if (params.agent_id) {
    conditions.push('agent_id = ?');
    bindings.push(params.agent_id);
  }

  if (params.operation_type) {
    conditions.push('operation_type = ?');
    bindings.push(params.operation_type);
  }

  if (params.start_time !== undefined && params.start_time !== null) {
    if (typeof params.start_time !== 'number' || params.start_time < 0) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'audit.invalidStartTime' });
    }
    conditions.push('created_at >= ?');
    bindings.push(params.start_time);
  }

  if (params.end_time !== undefined && params.end_time !== null) {
    if (typeof params.end_time !== 'number' || params.end_time < 0) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'audit.invalidEndTime' });
    }
    conditions.push('created_at <= ?');
    bindings.push(params.end_time);
  }

  // Cursor-based pagination (keyset pagination)
  let cursorPayload = params.cursor ? decodeCursor(params.cursor) : null;
  if (params.cursor && !cursorPayload) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'audit.invalidCursor' });
  }

  if (cursorPayload) {
    // For descending order: fetch rows where (created_at, operation_id) < cursor
    conditions.push('(created_at < ? OR (created_at = ? AND operation_id < ?))');
    bindings.push(cursorPayload.created_at, cursorPayload.created_at, cursorPayload.id);
  }

  const whereClause = conditions.length > 0 ? 'WHERE ' + conditions.join(' AND ') : '';

  // Get total count (without cursor filter for accurate total)
  const countConditions = conditions.filter(
    (_, i) => !cursorPayload || i < conditions.length - 1,
  );
  const countBindings = cursorPayload ? bindings.slice(0, -3) : [...bindings];

  const countWhereClause =
    countConditions.length > 0 ? 'WHERE ' + countConditions.join(' AND ') : '';

  const countQuery = `SELECT COUNT(*) as total FROM operations ${countWhereClause}`;
  const countStmt = db.prepare(countQuery);
  const boundCountStmt =
    countBindings.length > 0 ? countStmt.bind(...countBindings) : countStmt;
  const countResult = await boundCountStmt.first<{ total: number }>();
  const totalCount = countResult?.total ?? 0;

  // Fetch the page of results (reverse chronological)
  // Request limit+1 to detect if there are more pages
  const dataQuery = `SELECT * FROM operations ${whereClause} ORDER BY created_at DESC, operation_id DESC LIMIT ?`;
  const dataBindings = [...bindings, limit + 1];

  const dataStmt = db.prepare(dataQuery);
  const boundDataStmt = dataStmt.bind(...dataBindings);
  const dataResult = await boundDataStmt.all<Operation>();
  const rows = dataResult.results ?? [];

  // Determine next cursor
  const hasMore = rows.length > limit;
  const operations = hasMore ? rows.slice(0, limit) : rows;

  let nextCursor: string | undefined;
  if (hasMore && operations.length > 0) {
    const lastItem = operations[operations.length - 1]!;
    nextCursor = encodeCursor({
      created_at: lastItem.created_at,
      id: lastItem.operation_id,
    });
  }

  return {
    operations,
    cursor: nextCursor,
    total_count: totalCount,
  };
}
