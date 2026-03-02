/**
 * JWT authentication middleware.
 *
 * Verifies Bearer tokens in the Authorization header using HMAC-SHA256.
 * On success, extracts `org_id`, `role`, and `sub` (actor) claims and
 * stores them in the Hono context variables for downstream handlers.
 *
 * Token structure (payload):
 * {
 *   "sub": "user_abc123",       // actor identifier
 *   "org_id": "org_acme",       // organization scope
 *   "role": "org_owner",        // RBAC role
 *   "iat": 1700000000,          // issued-at (seconds)
 *   "exp": 1700003600           // expiration (seconds)
 * }
 */

import type { MiddlewareHandler } from 'hono';
import type { RbacRole } from '../shared/index.js';
import type { Env, AppVariables } from '../types.js';
import { AppError } from './error-handler.js';
import { base64urlDecode } from '../utils/crypto.js';

/** Minimal JWT payload shape we require. */
interface JWTPayload {
  sub: string;
  org_id: string;
  role: RbacRole;
  iat: number;
  exp: number;
}

/**
 * Verify a JWT using HMAC-SHA256 and return its payload.
 */
async function verifyJWT(token: string, secret: string): Promise<JWTPayload> {
  const parts = token.split('.');
  if (parts.length !== 3) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.malformedJwt' });
  }

  const headerB64 = parts[0]!;
  const payloadB64 = parts[1]!;
  const signatureB64 = parts[2]!;

  // Validate JWT header — only HS256 is accepted
  try {
    const headerBytes = base64urlDecode(headerB64);
    const header = JSON.parse(new TextDecoder().decode(headerBytes)) as { alg?: string; typ?: string };
    if (header.alg !== 'HS256') {
      throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.unsupportedAlgorithm' });
    }
  } catch (e) {
    if (e instanceof AppError) throw e;
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.malformedJwt' });
  }

  // Import the HMAC key
  const encoder = new TextEncoder();
  const keyData = encoder.encode(secret);
  const cryptoKey = await crypto.subtle.importKey(
    'raw',
    keyData,
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['verify'],
  );

  // Verify signature over "header.payload"
  const signedInput = encoder.encode(`${headerB64}.${payloadB64}`);
  const signatureBytes = base64urlDecode(signatureB64);

  const valid = await crypto.subtle.verify('HMAC', cryptoKey, signatureBytes as unknown as Uint8Array<ArrayBuffer>, signedInput as unknown as Uint8Array<ArrayBuffer>);

  if (!valid) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.invalidSignature' });
  }

  // Decode and validate payload
  const payloadBytes = base64urlDecode(payloadB64);
  const payloadJson = new TextDecoder().decode(payloadBytes);
  let payload: JWTPayload;

  try {
    payload = JSON.parse(payloadJson);
  } catch {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.malformedPayload' });
  }

  // Validate required claims
  if (!payload.sub || typeof payload.sub !== 'string') {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.missingSub' });
  }
  if (!payload.org_id || typeof payload.org_id !== 'string') {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.missingOrgId' });
  }
  if (!payload.role || typeof payload.role !== 'string') {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.missingRole' });
  }

  // Check expiration
  const nowSeconds = Math.floor(Date.now() / 1000);
  if (typeof payload.exp !== 'number') {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.missingExp' });
  }
  if (payload.exp < nowSeconds) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.expired' });
  }

  return payload;
}

/**
 * Authentication middleware.
 *
 * Extracts the Bearer token, verifies it, and sets org_id, role, and
 * actor on the context. Should be applied to all routes that require
 * authentication.
 */
export const authMiddleware: MiddlewareHandler<{
  Bindings: Env;
  Variables: AppVariables;
}> = async (c, next) => {
  const authHeader = c.req.header('Authorization');

  if (!authHeader) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.missingHeader' });
  }

  if (!authHeader.startsWith('Bearer ')) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.bearerRequired' });
  }

  const token = authHeader.slice(7).trim();

  if (!token) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.emptyToken' });
  }

  const payload = await verifyJWT(token, c.env.JWT_SECRET);

  c.set('org_id', payload.org_id);
  c.set('role', payload.role);
  c.set('actor', payload.sub);

  await next();
};
