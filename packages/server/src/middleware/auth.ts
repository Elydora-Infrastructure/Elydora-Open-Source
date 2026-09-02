/**
 * Authentication middleware for opaque API tokens and Better Auth sessions.
 *
 * A bearer value is resolved as a hashed API token first; anything else is
 * validated as a Better Auth session (cookie or bearer session token).
 * On success the org_id, role, actor, and credential kind are stored in the
 * Hono context variables for downstream handlers.
 */

import type { MiddlewareHandler } from 'hono';
import type { RbacRole } from '../shared/index.js';
import type { Env, AppVariables } from '../types.js';
import { AppError } from './error-handler.js';
import { createAuth } from '../lib/auth.js';
import { authenticateApiToken } from '../services/api-token-service.js';

export const authMiddleware: MiddlewareHandler<{
  Bindings: Env;
  Variables: AppVariables;
}> = async (c, next) => {
  const authHeader = c.req.header('Authorization');
  if (!authHeader) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.missingHeader' });
  }

  const bearerToken = authHeader.startsWith('Bearer ') ? authHeader.slice(7).trim() : '';
  if (bearerToken) {
    const apiToken = await authenticateApiToken(c.env.ELYDORA_DB, bearerToken);
    if (apiToken) {
      c.set('org_id', apiToken.org_id);
      c.set('role', apiToken.role);
      c.set('actor', apiToken.user_id);
      c.set('auth_token_type', 'api');
      await next();
      return;
    }
  }

  let session: Awaited<ReturnType<ReturnType<typeof createAuth>['api']['getSession']>>;

  try {
    const betterAuthInstance = createAuth(
      process.env.DATABASE_URL!,
      c.env.BETTER_AUTH_SECRET,
      c.env.BETTER_AUTH_URL,
      c.env.ALLOWED_ORIGINS,
    );
    session = await betterAuthInstance.api.getSession({
      headers: c.req.raw.headers,
    });
  } catch {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.invalidSession' });
  }

  if (!session) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.invalidSession' });
  }

  const user = session.user as { id: string; org_id?: string; role?: string };

  c.set('org_id', user.org_id ?? '');
  c.set('role', ((user.role ?? 'readonly_investigator') as RbacRole));
  c.set('actor', user.id);
  c.set('auth_token_type', 'session');

  await next();
};
