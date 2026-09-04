/** Global error handler middleware. */

import type { ErrorHandler } from 'hono';
import type { ErrorCode, ErrorResponse } from '../shared/index.js';
import { ERROR_CODES } from '../shared/index.js';
import type { Env, AppVariables } from '../types.js';
import { getMessage } from '../i18n/messages.js';
import type { Lang } from '../i18n/messages.js';

/** Maps each ErrorCode to its corresponding i18n message key. */
const ERROR_CODE_TO_I18N_KEY: Record<ErrorCode, string> = {
  INVALID_SIGNATURE: 'error.invalidSignature',
  UNKNOWN_AGENT: 'error.unknownAgent',
  KEY_REVOKED: 'error.keyRevoked',
  KEY_RETIRED: 'error.keyRetired',
  AGENT_FROZEN: 'error.agentFrozen',
  AGENT_REVOKED: 'error.agentRevoked',
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

/** Custom error class that carries an HTTP status and Elydora error code. */
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
      // i18n mode: store the key; resolve later in the error handler
      super(getMessage(messageOrOpts.key, 'en', messageOrOpts.params));
      this.messageKey = messageOrOpts.key;
      this.messageParams = messageOrOpts.params;
      this.usesDefaultMessage = false;
    } else if (messageOrOpts === undefined) {
      // Code-only mode: will be resolved via ERROR_CODE_TO_I18N_KEY
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

  /** Resolve the user-facing message for a given language. */
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

/** Build a standardised error response body. */
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

/** Hono error handler that converts all errors into the ErrorResponse format. */
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
