/**
 * Auth service — password hashing, JWT issuance, user registration, and login.
 *
 * Uses PBKDF2-SHA256 via Web Crypto API for password hashing and
 * HMAC-SHA256 for JWT issuance (matching the existing auth middleware).
 */

import { base64urlEncode } from '../utils/crypto.js';
import { generateUUIDv7 } from '../utils/uuid.js';
import { AppError } from '../middleware/error-handler.js';
import { DEFAULT_JWT_TTL_SECONDS } from '../shared/index.js';
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
// JWT payload
// ---------------------------------------------------------------------------

interface JWTPayload {
  sub: string;
  org_id: string;
  role: RbacRole;
  iat: number;
  exp: number;
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
// JWT issuance (HMAC-SHA256, matching existing auth middleware verification)
// ---------------------------------------------------------------------------

export async function issueJWT(payload: JWTPayload, secret: string): Promise<string> {
  const encoder = new TextEncoder();
  const header = { alg: 'HS256', typ: 'JWT' };
  const headerB64 = base64urlEncode(encoder.encode(JSON.stringify(header)));
  const payloadB64 = base64urlEncode(encoder.encode(JSON.stringify(payload)));

  const signingInput = `${headerB64}.${payloadB64}`;
  const key = await crypto.subtle.importKey(
    'raw',
    encoder.encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  const signature = await crypto.subtle.sign('HMAC', key, encoder.encode(signingInput));
  const signatureB64 = base64urlEncode(new Uint8Array(signature));

  return `${headerB64}.${payloadB64}.${signatureB64}`;
}

// ---------------------------------------------------------------------------
// Issue API token with custom TTL
// ---------------------------------------------------------------------------

/** Maximum API token lifetime: 365 days. */
const MAX_API_TOKEN_TTL = 365 * 24 * 60 * 60;

export async function issueApiToken(
  sub: string,
  orgId: string,
  role: RbacRole,
  jwtSecret: string,
  ttlSeconds: number | null,
): Promise<{ token: string; expires_at: number }> {
  const now = Math.floor(Date.now() / 1000);
  // Default to 90 days if no TTL specified; cap at 365 days
  const effectiveTtl = ttlSeconds === null ? 90 * 24 * 60 * 60 : Math.min(ttlSeconds, MAX_API_TOKEN_TTL);
  if (effectiveTtl <= 0) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'auth.ttlMustBePositive' });
  }
  const exp = now + effectiveTtl;
  const expiresAt = exp;

  const token = await issueJWT(
    { sub, org_id: orgId, role, iat: now, exp },
    jwtSecret,
  );

  return { token, expires_at: expiresAt };
}

// ---------------------------------------------------------------------------
// Register user + org
// ---------------------------------------------------------------------------

export async function registerUser(
  db: Database,
  jwtSecret: string,
  email: string,
  password: string,
  displayName: string,
  orgName: string,
): Promise<{ user: UserRow; organization: OrgRow; token: string }> {
  // Validate
  if (!email || !password || password.length < 8 || password.length > 128) {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'auth.emailPasswordRequired' });
  }

  // Check email uniqueness
  const existing = await db
    .prepare('SELECT user_id FROM users WHERE email = ?')
    .bind(email)
    .first();

  if (existing) {
    throw new AppError(409, 'VALIDATION_ERROR', { key: 'auth.emailAlreadyRegistered' });
  }

  const now = Date.now();
  const orgId = generateUUIDv7();
  const userId = generateUUIDv7();
  const hash = await hashPassword(password);

  const resolvedDisplayName = displayName || email.split('@')[0]!;
  const resolvedOrgName = orgName || `${resolvedDisplayName}'s Organization`;

  // Create org + user atomically in a single transaction
  await db.batch([
    db
      .prepare(
        'INSERT INTO organizations (org_id, name, created_at, updated_at) VALUES (?, ?, ?, ?)',
      )
      .bind(orgId, resolvedOrgName, now, now),
    db
      .prepare(
        'INSERT INTO users (user_id, org_id, email, password_hash, display_name, role, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
      )
      .bind(userId, orgId, email, hash, resolvedDisplayName, 'org_owner', 'active', now, now),
  ]);

  const token = await issueJWT(
    {
      sub: userId,
      org_id: orgId,
      role: 'org_owner',
      iat: Math.floor(now / 1000),
      exp: Math.floor(now / 1000) + DEFAULT_JWT_TTL_SECONDS,
    },
    jwtSecret,
  );

  return {
    user: {
      user_id: userId,
      org_id: orgId,
      email,
      display_name: resolvedDisplayName,
      role: 'org_owner' as RbacRole,
      status: 'active',
      created_at: now,
      updated_at: now,
    },
    organization: {
      org_id: orgId,
      name: resolvedOrgName,
      created_at: now,
      updated_at: now,
    },
    token,
  };
}

// ---------------------------------------------------------------------------
// Login user
// ---------------------------------------------------------------------------

export async function loginUser(
  db: Database,
  jwtSecret: string,
  email: string,
  password: string,
): Promise<{ user: UserRow; token: string }> {
  const row = await db
    .prepare(
      'SELECT user_id, org_id, email, password_hash, display_name, role, status, created_at, updated_at FROM users WHERE email = ?',
    )
    .bind(email)
    .first<UserRow & { password_hash: string }>();

  if (!row) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.invalidCredentials' });
  }

  if (row.status !== 'active') {
    throw new AppError(403, 'FORBIDDEN', { key: 'auth.accountSuspended' });
  }

  const valid = await verifyPassword(password, row.password_hash);
  if (!valid) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.invalidCredentials' });
  }

  const now = Date.now();
  const token = await issueJWT(
    {
      sub: row.user_id,
      org_id: row.org_id,
      role: row.role as RbacRole,
      iat: Math.floor(now / 1000),
      exp: Math.floor(now / 1000) + DEFAULT_JWT_TTL_SECONDS,
    },
    jwtSecret,
  );

  return {
    user: {
      user_id: row.user_id,
      org_id: row.org_id,
      email: row.email,
      display_name: row.display_name,
      role: row.role as RbacRole,
      status: row.status,
      created_at: row.created_at,
      updated_at: row.updated_at,
    },
    token,
  };
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
