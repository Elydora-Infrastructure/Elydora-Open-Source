/**
 * Better Auth authentication middleware.
 *
 * Validates requests via Better Auth sessions (cookie or bearer token).
 * On success, extracts `org_id`, `role`, and user `id` (actor) from the
 * session and stores them in the Hono context variables for downstream
 * handlers.
 *
 * Supports:
 *   - Session cookies (browser / console)
 *   - Bearer tokens via Better Auth's bearer plugin (SDK clients)
 */

import type { MiddlewareHandler } from 'hono';
import type { RbacRole } from '../shared/index.js';
import type { Env, AppVariables } from '../types.js';
import { AppError } from './error-handler.js';
import { createAuth } from '../lib/auth.js';

/**
 * Authentication middleware.
 *
 * Uses Better Auth's getSession to validate the request via session
 * cookie or bearer token. Sets org_id, role, and actor on the context.
 */
export const authMiddleware: MiddlewareHandler<{
  Bindings: Env;
  Variables: AppVariables;
}> = async (c, next) => {
  const authHeader = c.req.header('Authorization');
  if (!authHeader) {
    throw new AppError(401, 'UNAUTHORIZED', { key: 'auth.missingHeader' });
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

  await next();
};
