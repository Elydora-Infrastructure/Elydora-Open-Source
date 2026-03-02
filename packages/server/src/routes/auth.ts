/**
 * Auth routes — registration, login, profile retrieval, and token refresh.
 *
 * /register and /login are public (no auth required).
 * /me and /refresh require a valid JWT.
 */

import { Hono } from 'hono';
import type { Env, AppVariables } from '../types.js';
import { authMiddleware } from '../middleware/auth.js';
import { registerUser, loginUser, getMe, issueJWT, issueApiToken } from '../services/auth-service.js';
import type { RbacRole } from '../shared/index.js';
import { DEFAULT_JWT_TTL_SECONDS } from '../shared/index.js';
import { getMessage } from '../i18n/messages.js';

const auth = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// ---------------------------------------------------------------------------
// POST /v1/auth/register — Create a new user and organization
// ---------------------------------------------------------------------------
auth.post('/register', async (c) => {
  const body = await c.req.json();
  const { email, password, display_name, org_name } = body;

  const result = await registerUser(
    c.env.ELYDORA_DB,
    c.env.JWT_SECRET,
    email,
    password,
    display_name,
    org_name,
  );

  return c.json(result, 201);
});

// ---------------------------------------------------------------------------
// POST /v1/auth/login — Authenticate and receive a JWT
// ---------------------------------------------------------------------------
auth.post('/login', async (c) => {
  const body = await c.req.json();
  const { email, password } = body;

  const result = await loginUser(c.env.ELYDORA_DB, c.env.JWT_SECRET, email, password);

  return c.json(result, 200);
});

// ---------------------------------------------------------------------------
// GET /v1/auth/me — Get current user profile (auth required)
// ---------------------------------------------------------------------------
auth.get('/me', authMiddleware, async (c) => {
  const userId = c.get('actor');
  const user = await getMe(c.env.ELYDORA_DB, userId);

  return c.json({ user }, 200);
});

// ---------------------------------------------------------------------------
// POST /v1/auth/refresh — Issue a new JWT (auth required)
// ---------------------------------------------------------------------------
auth.post('/refresh', authMiddleware, async (c) => {
  const now = Date.now();
  const token = await issueJWT(
    {
      sub: c.get('actor'),
      org_id: c.get('org_id'),
      role: c.get('role') as RbacRole,
      iat: Math.floor(now / 1000),
      exp: Math.floor(now / 1000) + DEFAULT_JWT_TTL_SECONDS,
    },
    c.env.JWT_SECRET,
  );

  return c.json({ token }, 200);
});

// ---------------------------------------------------------------------------
// POST /v1/auth/token — Issue an API token with custom TTL (auth required)
// ---------------------------------------------------------------------------
auth.post('/token', authMiddleware, async (c) => {
  const body = await c.req.json();
  const { ttl_seconds } = body;

  if (ttl_seconds !== null && ttl_seconds !== undefined) {
    if (typeof ttl_seconds !== 'number' || ttl_seconds <= 0) {
      const lang = c.get('lang') ?? 'en';
      return c.json({ error: { code: 'VALIDATION_ERROR', message: getMessage('auth.ttlMustBeNumber', lang), request_id: '' } }, 400);
    }
  }

  const result = await issueApiToken(
    c.get('actor'),
    c.get('org_id'),
    c.get('role') as RbacRole,
    c.env.JWT_SECRET,
    ttl_seconds ?? null,
  );

  return c.json(result, 200);
});

export { auth };
