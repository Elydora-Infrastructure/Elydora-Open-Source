/**
 * Auth service — password hashing and user profile retrieval.
 *
 * Authentication is handled by Better Auth (see lib/auth.ts).
 * This service provides password utilities for legacy PBKDF2 hash
 * verification and the getMe profile query.
 */

import { AppError } from '../middleware/error-handler.js';
import type { RbacRole } from '../shared/index.js';
import type { Database } from '../adapters/interfaces.js';

// ---------------------------------------------------------------------------
// Row types (query results)
// ---------------------------------------------------------------------------

export interface UserRow {
  readonly user_id: string;
  readonly org_id: string;
  readonly email: string;
  readonly display_name: string;
  readonly role: RbacRole;
  readonly status: 'active' | 'suspended';
  readonly created_at: number;
  readonly updated_at: number;
}

export interface OrgRow {
  readonly org_id: string;
  readonly name: string;
  readonly created_at: number;
  readonly updated_at: number;
}

// ---------------------------------------------------------------------------
// Password hashing (PBKDF2-SHA256 via Web Crypto)
// Format: pbkdf2:100000:salt_hex:hash_hex
// ---------------------------------------------------------------------------

export async function hashPassword(password: string): Promise<string> {
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const encoder = new TextEncoder();
  const keyMaterial = await crypto.subtle.importKey(
    'raw',
    encoder.encode(password),
    'PBKDF2',
    false,
    ['deriveBits'],
  );
  const bits = await crypto.subtle.deriveBits(
    { name: 'PBKDF2', salt, iterations: 100000, hash: 'SHA-256' },
    keyMaterial,
    256,
  );
  const saltHex = Array.from(salt)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
  const hashHex = Array.from(new Uint8Array(bits))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
  return `pbkdf2:100000:${saltHex}:${hashHex}`;
}

export async function verifyPassword(password: string, stored: string): Promise<boolean> {
  const parts = stored.split(':');
  if (parts.length !== 4 || parts[0] !== 'pbkdf2') return false;

  const iterations = parseInt(parts[1]!, 10);
  const saltHex = parts[2]!;
  const storedHashHex = parts[3]!;

  const salt = new Uint8Array(saltHex.match(/.{2}/g)!.map((b) => parseInt(b, 16)));
  const encoder = new TextEncoder();
  const keyMaterial = await crypto.subtle.importKey(
    'raw',
    encoder.encode(password),
    'PBKDF2',
    false,
    ['deriveBits'],
  );
  const bits = await crypto.subtle.deriveBits(
    { name: 'PBKDF2', salt, iterations, hash: 'SHA-256' },
    keyMaterial,
    256,
  );
  const hashHex = Array.from(new Uint8Array(bits))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');

  // Constant-time comparison
  if (hashHex.length !== storedHashHex.length) return false;
  let result = 0;
  for (let i = 0; i < hashHex.length; i++) {
    result |= hashHex.charCodeAt(i) ^ storedHashHex.charCodeAt(i);
  }
  return result === 0;
}

// ---------------------------------------------------------------------------
// Get current user profile
// ---------------------------------------------------------------------------

export async function getMe(db: Database, userId: string): Promise<UserRow> {
  const row = await db
    .prepare(
      'SELECT user_id, org_id, email, display_name, role, status, created_at, updated_at FROM users WHERE user_id = ?',
    )
    .bind(userId)
    .first<UserRow>();

  if (!row) {
    throw new AppError(404, 'NOT_FOUND', { key: 'auth.userNotFound' });
  }

  return row;
}
