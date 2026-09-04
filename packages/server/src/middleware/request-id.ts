/** Request ID middleware. */

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
