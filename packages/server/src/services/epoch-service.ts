/**
 * Epoch service — business logic for retrieving epoch root records.
 *
 * Epochs are periodic Merkle-tree rollups that anchor a set of operations
 * to a single root hash. Optionally, the epoch may carry a TSA
 * (Timestamping Authority) anchor that binds the root to an external
 * trusted timestamp for non-repudiation.
 */

import type { Epoch, EER, GetEpochResponse, ListEpochsResponse } from '../shared/index.js';
import { AppError } from '../middleware/error-handler.js';
import { buildMerkleTree } from '../utils/merkle.js';
import { generateUUIDv7 } from '../utils/uuid.js';
import { signEd25519, jcsCanonicalise } from '../utils/crypto.js';
import type { Database, ObjectStore } from '../adapters/interfaces.js';

// ---------------------------------------------------------------------------
// List epochs
// ---------------------------------------------------------------------------

export async function listEpochs(
  db: Database,
  orgId: string,
): Promise<ListEpochsResponse> {
  const { results } = await db
    .prepare('SELECT * FROM epochs WHERE org_id = ? ORDER BY created_at DESC')
    .bind(orgId)
    .all<Epoch>();
  return { epochs: results ?? [] };
}

// ---------------------------------------------------------------------------
// Get epoch
// ---------------------------------------------------------------------------

export async function getEpoch(
  db: Database,
  r2: ObjectStore,
  epochId: string,
  orgId: string,
): Promise<GetEpochResponse> {
  const epoch = await db
    .prepare('SELECT * FROM epochs WHERE epoch_id = ? AND org_id = ?')
    .bind(epochId, orgId)
    .first<Epoch>();

  if (!epoch) {
    throw new AppError(404, 'NOT_FOUND', { key: 'epoch.notFound', params: { id: epochId } });
  }

  // Attempt to load TSA anchor from R2 (stored alongside the epoch record)
  let anchor: { tsa_token?: string; tsa_url?: string; anchored_at?: number } | undefined;

  try {
    const tsaKey = `${epoch.r2_epoch_key}/tsa-anchor`;
    const tsaObject = await r2.get(tsaKey);
    if (tsaObject) {
      const tsaData = JSON.parse(await tsaObject.text()) as {
        tsa_token?: string;
        tsa_url?: string;
        anchored_at?: number;
      };
      if (tsaData.tsa_token) {
        anchor = {
          tsa_token: tsaData.tsa_token,
          tsa_url: tsaData.tsa_url,
          anchored_at: tsaData.anchored_at,
        };
      }
    }
  } catch {
    // TSA anchor is optional; if it doesn't exist or fails to parse, omit it
  }

  return {
    epoch,
    anchor,
  };
}

// ---------------------------------------------------------------------------
// Create epoch
// ---------------------------------------------------------------------------

/**
 * Create a new epoch by building a Merkle tree over operations in a time
 * window, signing the Epoch Record, and persisting to D1 + R2.
 *
 * Returns null if the epoch already exists, or if no operations fall within
 * the given time range.
 */
export async function createEpoch(
  db: Database,
  r2: ObjectStore,
  signingKey: string,
  orgId: string,
  startTime: number,
  endTime: number,
): Promise<Epoch | null> {
  // 1. Idempotency check — skip if epoch already exists for this window
  const existing = await db
    .prepare(
      'SELECT epoch_id FROM epochs WHERE org_id = ? AND start_time = ? AND end_time = ?',
    )
    .bind(orgId, startTime, endTime)
    .first<{ epoch_id: string }>();

  if (existing) return null;

  // 2. Query operations in the time window
  const { results: ops } = await db
    .prepare(
      'SELECT operation_id, chain_hash FROM operations WHERE org_id = ? AND created_at >= ? AND created_at < ? ORDER BY chain_hash ASC',
    )
    .bind(orgId, startTime, endTime)
    .all<{ operation_id: string; chain_hash: string }>();

  if (!ops || ops.length === 0) return null;

  // 3. Build Merkle tree
  const chainHashes = ops.map((op) => op.chain_hash);
  const operationIds = ops.map((op) => op.operation_id);
  const tree = await buildMerkleTree(chainHashes, operationIds);

  // 4. Generate epoch ID
  const epochId = generateUUIDv7();

  // 5. Create and sign the EER
  const eer: { -readonly [K in keyof EER]: EER[K] } = {
    epoch_id: epochId,
    org_id: orgId,
    start_time: startTime,
    end_time: endTime,
    leaf_count: tree.leaves.length,
    root_hash: tree.root,
    hash_alg: 'sha256',
    signature_by_elydora: '',
  };

  const canonical = jcsCanonicalise(eer);
  eer.signature_by_elydora = await signEd25519(
    signingKey,
    new TextEncoder().encode(canonical),
  );

  // 6. Store in R2
  const r2Key = `epochs/${orgId}/${epochId}`;
  const r2Data = {
    eer: eer as EER,
    merkle: { leafOps: tree.leafOps, layers: tree.layers },
  };
  await r2.put(r2Key, JSON.stringify(r2Data), {
    httpMetadata: { contentType: 'application/json' },
  });

  // 7. Insert epoch record in D1
  const now = Date.now();
  await db
    .prepare(
      'INSERT INTO epochs (epoch_id, org_id, start_time, end_time, root_hash, leaf_count, r2_epoch_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
    )
    .bind(epochId, orgId, startTime, endTime, tree.root, tree.leaves.length, r2Key, now)
    .run();

  return {
    epoch_id: epochId,
    org_id: orgId,
    start_time: startTime,
    end_time: endTime,
    root_hash: tree.root,
    leaf_count: tree.leaves.length,
    r2_epoch_key: r2Key,
    created_at: now,
  };
}
