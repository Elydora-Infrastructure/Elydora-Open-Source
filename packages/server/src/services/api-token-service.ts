/**
 * Opaque API tokens: the raw value is returned once; only its SHA-256 hash is stored.
 */

import { createHash, randomBytes } from 'node:crypto';
import type { Database } from '../adapters/interfaces.js';
import type { RbacRole } from '../shared/index.js';
import { generateUUIDv7 } from '../utils/uuid.js';

export const MAX_TTL_SECONDS = 31_536_000;

interface ApiTokenAuthRow {
  readonly token_id: string;
  readonly user_id: string;
  readonly org_id: string;
  readonly expires_at: number | null;
  readonly user_org_id: string | null;
  readonly user_role: RbacRole;
  readonly user_status: string;
}

export interface AuthenticatedApiToken {
  readonly token_id: string;
  readonly user_id: string;
  readonly org_id: string;
  readonly role: RbacRole;
}

export interface IssuedApiToken {
  readonly token: string;
  readonly token_id: string;
  readonly expires_at: number | null;
}

function hashToken(token: string): string {
  return createHash('sha256').update(token).digest('hex');
}

export async function authenticateApiToken(
  db: Database,
  rawToken: string,
): Promise<AuthenticatedApiToken | null> {
  const nowSeconds = Math.floor(Date.now() / 1000);
  const row = await db
    .prepare(
      `SELECT t.token_id, t.user_id, t.org_id, t.expires_at,
              u.org_id AS user_org_id, u.role AS user_role, u.status AS user_status
       FROM api_tokens AS t
       JOIN users AS u ON u.user_id = t.user_id
       WHERE t.token_hash = ?`,
    )
    .bind(hashToken(rawToken))
    .first<ApiTokenAuthRow>();

  if (!row) return null;
  if (row.expires_at !== null && row.expires_at <= nowSeconds) return null;
  if (row.user_status !== 'active' || row.user_org_id !== row.org_id) return null;

  return {
    token_id: row.token_id,
    user_id: row.user_id,
    org_id: row.org_id,
    role: row.user_role,
  };
}

export async function issueApiToken(
  db: Database,
  userId: string,
  orgId: string,
  ttlSeconds: number | null,
): Promise<IssuedApiToken> {
  const nowSeconds = Math.floor(Date.now() / 1000);
  const expiresAt = ttlSeconds === null ? null : nowSeconds + ttlSeconds;
  const tokenId = generateUUIDv7();
  const token = randomBytes(32).toString('base64url');

  await db
    .prepare(
      `INSERT INTO api_tokens (token_id, user_id, org_id, token_hash, issued_at, expires_at)
       VALUES (?, ?, ?, ?, ?, ?)`,
    )
    .bind(tokenId, userId, orgId, hashToken(token), nowSeconds, expiresAt)
    .run();

  return { token, token_id: tokenId, expires_at: expiresAt };
}
