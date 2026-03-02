/**
 * i18n middleware — language detection from Accept-Language header.
 *
 * Reads the Accept-Language request header, detects the preferred
 * language (English or Chinese), and stores it in the Hono context
 * so that all downstream handlers and services can access it via
 * `c.get('lang')`.
 */

import type { MiddlewareHandler } from 'hono';
import type { Env, AppVariables } from '../types.js';
import { detectLanguage } from '../i18n/messages.js';

export const i18nMiddleware: MiddlewareHandler<{
  Bindings: Env;
  Variables: AppVariables;
}> = async (c, next) => {
  const acceptLanguage = c.req.header('Accept-Language');
  const lang = detectLanguage(acceptLanguage);
  c.set('lang', lang);
  await next();
};
