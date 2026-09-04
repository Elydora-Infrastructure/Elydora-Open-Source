import type { Operation, Receipt, GetOperationResponse } from '../shared/index.js';
import { AppError } from '../middleware/error-handler.js';
import type { Database, ObjectStore } from '../adapters/interfaces.js';

export { submitOperation } from './operation-submit.js';
export { verifyOperation } from './operation-verify.js';

export async function getOperation(
  db: Database,
  r2: ObjectStore,
  operationId: string,
  orgId: string,
): Promise<GetOperationResponse> {
  const operation = await db
    .prepare('SELECT * FROM operations WHERE operation_id = ? AND org_id = ?')
    .bind(operationId, orgId)
    .first<Operation>();

  if (!operation) {
    throw new AppError(404, 'NOT_FOUND', { key: 'operation.notFound', params: { id: operationId } });
  }

  const receipt = await db
    .prepare('SELECT * FROM receipts WHERE operation_id = ?')
    .bind(operationId)
    .first<Receipt>();

  // Fetch payload from R2
  let payload: Record<string, unknown> | undefined;
  if (operation.r2_payload_key) {
    try {
      const r2Object = await r2.get(operation.r2_payload_key);
      if (r2Object) {
        const eor = await r2Object.json<Record<string, unknown>>();
        payload = (eor.payload as Record<string, unknown>) ?? undefined;
      }
    } catch {
      // Payload fetch is best-effort: don't fail the request
    }
  }

  return {
    operation,
    receipt: receipt ?? undefined,
    payload,
  };
}

// ---------------------------------------------------------------------------
// Verify operation
// ---------------------------------------------------------------------------
