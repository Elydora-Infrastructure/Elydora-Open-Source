/** Auth routes: registration, login, profile retrieval, and token refresh. */

import { Hono } from 'hono';
import type { Env, AppVariables } from '../types.js';
import { authMiddleware } from '../middleware/auth.js';
import { AppError } from '../middleware/error-handler.js';
import { issueApiToken, MAX_TTL_SECONDS } from '../services/api-token-service.js';
import { getMe } from '../services/auth-service.js';
import { createAuth } from '../lib/auth.js';
import { getMessage } from '../i18n/messages.js';

const auth = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// ---------------------------------------------------------------------------
// POST /v1/auth/register: Create a new user and organization
// ---------------------------------------------------------------------------
auth.post('/register', async (c) => {
  const body = await c.req.json();
  const { email, password, display_name, org_name } = body;

  const betterAuthInstance = createAuth(
    process.env.DATABASE_URL!,
    c.env.BETTER_AUTH_SECRET,
    c.env.BETTER_AUTH_URL,
    c.env.ALLOWED_ORIGINS,
  );

  // Use Better Auth to sign up the user
  const signUpResult = await betterAuthInstance.api.signUpEmail({
    body: {
      email,
      password,
      name: display_name || email.split('@')[0]!,
    },
  });

  if (!signUpResult) {
    const lang = c.get('lang') ?? 'en';
    return c.json({ error: { code: 'VALIDATION_ERROR', message: getMessage('auth.emailAlreadyRegistered', lang), request_id: '' } }, 409);
  }

  const user = signUpResult.user as { id: string; email: string; name: string; org_id?: string; role?: string; status?: string; createdAt?: unknown; updatedAt?: unknown };

  // Create the organization via the database adapter (preserving existing schema)
  const now = Date.now();
  const { generateUUIDv7 } = await import('../utils/uuid.js');
  const orgId = generateUUIDv7();
  const resolvedOrgName = org_name || `${user.name}'s Organization`;

  await c.env.ELYDORA_DB.batch([
    c.env.ELYDORA_DB
      .prepare(
        'INSERT INTO organizations (org_id, name, created_at, updated_at) VALUES (?, ?, ?, ?)',
      )
      .bind(orgId, resolvedOrgName, now, now),
    c.env.ELYDORA_DB
      .prepare(
        'UPDATE users SET org_id = ?, role = ?, status = ? WHERE user_id = ?',
      )
      .bind(orgId, 'org_owner', 'active', user.id),
  ]);

  // Return the session token
  const token = signUpResult.token ?? '';

  return c.json({
    user: {
      user_id: user.id,
      org_id: orgId,
      email: user.email,
      display_name: user.name,
      role: 'org_owner',
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
  }, 201);
});

// ---------------------------------------------------------------------------
// POST /v1/auth/login: Authenticate and receive a session token
// ---------------------------------------------------------------------------
auth.post('/login', async (c) => {
  const body = await c.req.json();
  const { email, password } = body;

  const betterAuthInstance = createAuth(
    process.env.DATABASE_URL!,
    c.env.BETTER_AUTH_SECRET,
    c.env.BETTER_AUTH_URL,
    c.env.ALLOWED_ORIGINS,
  );

  const signInResult = await betterAuthInstance.api.signInEmail({
    body: { email, password },
  });

  if (!signInResult) {
    const lang = c.get('lang') ?? 'en';
    return c.json({ error: { code: 'UNAUTHORIZED', message: getMessage('auth.invalidCredentials', lang), request_id: '' } }, 401);
  }

  const user = signInResult.user as { id: string; email: string; name: string; org_id?: string; role?: string; status?: string; created_at?: number; updated_at?: number };
  const token = signInResult.token ?? '';

  return c.json({
    user: {
      user_id: user.id,
      org_id: user.org_id ?? '',
      email: user.email,
      display_name: user.name,
      role: user.role ?? 'org_owner',
      status: user.status ?? 'active',
      created_at: user.created_at ?? 0,
      updated_at: user.updated_at ?? 0,
    },
    token,
  }, 200);
});

// ---------------------------------------------------------------------------
// GET /v1/auth/me: Get current user profile (auth required)
// ---------------------------------------------------------------------------
auth.get('/me', authMiddleware, async (c) => {
  const userId = c.get('actor');
  const user = await getMe(c.env.ELYDORA_DB, userId);

  return c.json({ user }, 200);
});

// ---------------------------------------------------------------------------
// POST /v1/auth/refresh: Issue a new session token (auth required)
// ---------------------------------------------------------------------------
auth.post('/refresh', authMiddleware, async (c) => {
  const betterAuthInstance = createAuth(
    process.env.DATABASE_URL!,
    c.env.BETTER_AUTH_SECRET,
    c.env.BETTER_AUTH_URL,
    c.env.ALLOWED_ORIGINS,
  );

  // Get the current session and return its token: Better Auth handles
  // session refresh automatically based on the updateAge setting
  const session = await betterAuthInstance.api.getSession({
    headers: c.req.raw.headers,
  });

  if (!session) {
    const lang = c.get('lang') ?? 'en';
    return c.json({ error: { code: 'UNAUTHORIZED', message: getMessage('auth.invalidSession', lang), request_id: '' } }, 401);
  }

  const token = session.session?.token ?? '';

  return c.json({ token }, 200);
});

// ---------------------------------------------------------------------------
// POST /v1/auth/token: Issue an opaque API token (session required)
// ---------------------------------------------------------------------------
auth.post('/token', authMiddleware, async (c) => {
  if (c.get('auth_token_type') !== 'session') {
    throw new AppError(400, 'VALIDATION_ERROR', { key: 'auth.issueRequiresSession' });
  }

  const body = await c.req.json();
  const { ttl_seconds } = body;

  if (ttl_seconds !== null && ttl_seconds !== undefined) {
    if (typeof ttl_seconds !== 'number') {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'auth.ttlMustBeNumber' });
    }
    if (!Number.isInteger(ttl_seconds) || ttl_seconds <= 0 || ttl_seconds > MAX_TTL_SECONDS) {
      throw new AppError(400, 'VALIDATION_ERROR', { key: 'auth.ttlMustBePositive' });
    }
  }

  const orgId = c.get('org_id');
  if (!orgId) {
    throw new AppError(409, 'VALIDATION_ERROR', { key: 'auth.organizationRequiredForApiToken' });
  }

  const issued = await issueApiToken(c.env.ELYDORA_DB, c.get('actor'), orgId, ttl_seconds ?? null);
  return c.json({ token: issued.token, expires_at: issued.expires_at }, 200);
});

export { auth };
