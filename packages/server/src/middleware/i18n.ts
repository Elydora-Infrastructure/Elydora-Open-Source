/** i18n middleware: language detection from Accept-Language header. */

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
