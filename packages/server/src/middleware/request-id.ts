/**
 * Request ID middleware.
 *
 * Generates a UUIDv7 for every incoming request and stores it in the
 * Hono context variables. The ID is also set as the X-Request-Id
 * response header so clients can reference it when reporting issues.
 */

import type { MiddlewareHandler } from 'hono';
import type { Env, AppVariables } from '../types.js';
import { generateUUIDv7 } from '../utils/uuid.js';

export const requestIdMiddleware: MiddlewareHandler<{
  Bindings: Env;
  Variables: AppVariables;
}> = async (c, next) => {
  const requestId = generateUUIDv7();
  c.set('request_id', requestId);
  c.header('X-Request-Id', requestId);

  await next();
};
