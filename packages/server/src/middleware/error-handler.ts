/**
 * Global error handler middleware.
 *
 * Catches all uncaught exceptions and formats them as a standardized
 * ErrorResponse with the appropriate HTTP status code, error code,
 * and the request's unique identifier for tracing.
 *
 * When an AppError carries a `messageKey`, the handler resolves it to
 * the correct language using the `lang` context variable set by the
 * i18n middleware. This allows services to throw errors with i18n keys
 * without needing direct access to the Hono context.
 */

import type { ErrorHandler } from 'hono';
import type { ErrorCode, ErrorResponse } from '../shared/index.js';
import { ERROR_CODES } from '../shared/index.js';
import type { Env, AppVariables } from '../types.js';
import { getMessage } from '../i18n/messages.js';
import type { Lang } from '../i18n/messages.js';

/**
 * Maps each ErrorCode to its corresponding i18n message key.
 *
 * This is used when an AppError is thrown with only an error code
 * (no explicit message or i18n key), so the global error handler
 * can still resolve a translated default message.
 */
const ERROR_CODE_TO_I18N_KEY: Record<ErrorCode, string> = {
  INVALID_SIGNATURE: 'error.invalidSignature',
  UNKNOWN_AGENT: 'error.unknownAgent',
  KEY_REVOKED: 'error.keyRevoked',
  AGENT_FROZEN: 'error.agentFrozen',
  TTL_EXPIRED: 'error.ttlExpired',
  REPLAY_DETECTED: 'error.replayDetected',
  PREV_HASH_MISMATCH: 'error.prevHashMismatch',
  PAYLOAD_TOO_LARGE: 'error.payloadTooLarge',
  RATE_LIMITED: 'error.rateLimited',
  INTERNAL_ERROR: 'error.internalError',
  UNAUTHORIZED: 'error.unauthorized',
  FORBIDDEN: 'error.forbidden',
  NOT_FOUND: 'error.notFound',
  VALIDATION_ERROR: 'error.validationError',
};

/**
 * Custom error class that carries an HTTP status and Elydora error code.
 *
 * Supports three modes:
 * 1. Code-only:  `new AppError(status, code)` — uses ErrorCode default message
 * 2. i18n key:   `new AppError(status, code, { key: 'msg.key', params: {...} })` —
 *                the message is resolved at response time from the translation map
 * 3. Raw string: `new AppError(status, code, 'raw message')` — message is used as-is
 *                (should be avoided in new code; prefer mode 2)
 */
export class AppError extends Error {
  public readonly statusCode: number;
  public readonly errorCode: ErrorCode;
  public readonly details?: Record<string, unknown>;
  public readonly messageKey?: string;
  public readonly messageParams?: Record<string, string | number>;
  /** True when the error was created with only an error code (no custom message). */
  public readonly usesDefaultMessage: boolean;

  constructor(
    statusCode: number,
    errorCode: ErrorCode,
    messageOrOpts?: string | { key: string; params?: Record<string, string | number> },
    details?: Record<string, unknown>,
  ) {
    if (typeof messageOrOpts === 'object' && messageOrOpts !== null) {
      // i18n mode — store the key; resolve later in the error handler
      super(getMessage(messageOrOpts.key, 'en', messageOrOpts.params));
      this.messageKey = messageOrOpts.key;
      this.messageParams = messageOrOpts.params;
      this.usesDefaultMessage = false;
    } else if (messageOrOpts === undefined) {
      // Code-only mode — will be resolved via ERROR_CODE_TO_I18N_KEY
      super(ERROR_CODES[errorCode]);
      this.usesDefaultMessage = true;
    } else {
      // Raw string mode (legacy)
      super(messageOrOpts);
      this.usesDefaultMessage = false;
    }
    this.name = 'AppError';
    this.statusCode = statusCode;
    this.errorCode = errorCode;
    this.details = details;
  }

  /**
   * Resolve the user-facing message for a given language.
   *
   * Resolution order:
   * 1. If a messageKey was provided, use that with the translation map.
   * 2. If the error used the default ErrorCode message, translate via
   *    the ERROR_CODE_TO_I18N_KEY mapping.
   * 3. Otherwise return the raw message string.
   */
  resolveMessage(lang: Lang): string {
    if (this.messageKey) {
      return getMessage(this.messageKey, lang, this.messageParams);
    }
    if (this.usesDefaultMessage) {
      const i18nKey = ERROR_CODE_TO_I18N_KEY[this.errorCode];
      if (i18nKey) {
        return getMessage(i18nKey, lang);
      }
    }
    return this.message;
  }
}

/**
 * Build a standardised error response body.
 *
 * When `lang` is provided and no explicit `message` is given, the
 * ErrorCode default message is resolved from the translation map.
 */
export function buildErrorResponse(
  code: ErrorCode,
  requestId: string,
  message?: string,
  details?: Record<string, unknown>,
  lang?: Lang,
): ErrorResponse {
  let resolvedMessage = message;
  if (!resolvedMessage) {
    const i18nKey = ERROR_CODE_TO_I18N_KEY[code];
    resolvedMessage = i18nKey ? getMessage(i18nKey, lang ?? 'en') : ERROR_CODES[code];
  }
  return {
    error: {
      code,
      message: resolvedMessage,
      request_id: requestId,
      ...(details ? { details } : {}),
    },
  };
}

/**
 * Hono error handler that converts all errors into the ErrorResponse format.
 *
 * Resolves i18n message keys using the `lang` context variable set by
 * the i18n middleware.
 */
export const globalErrorHandler: ErrorHandler<{
  Bindings: Env;
  Variables: AppVariables;
}> = (err, c) => {
  const requestId = c.get('request_id') ?? 'unknown';
  const lang: Lang = c.get('lang') ?? 'en';

  if (err instanceof AppError) {
    const resolvedMessage = err.resolveMessage(lang);
    const body = buildErrorResponse(err.errorCode, requestId, resolvedMessage, err.details);
    return c.json(body, err.statusCode as 400);
  }

  // Log unexpected errors for observability
  console.error(`[${requestId}] Unhandled error:`, err);

  const body = buildErrorResponse('INTERNAL_ERROR', requestId, undefined, undefined, lang);
  return c.json(body, 500);
};
