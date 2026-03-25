/**
 * Internationalization (i18n) translation map.
 *
 * Provides bilingual (English / Chinese) translations for all user-facing
 * error and validation messages returned by the Elydora API.
 *
 * Message keys follow a dot-separated convention:
 *   <domain>.<specificMessage>
 *
 * Some messages accept parameters via simple {param} placeholders that
 * callers must replace before returning the message to the client.
 */

export type Lang = 'en' | 'zh';

const messages: Record<string, Record<Lang, string>> = {
  // ---------------------------------------------------------------------------
  // Error code default messages  (src/shared/constants/errors.ts)
  // ---------------------------------------------------------------------------
  'error.invalidSignature': {
    en: 'The operation signature is invalid.',
    zh: '\u64cd\u4f5c\u7b7e\u540d\u65e0\u6548\u3002',
  },
  'error.unknownAgent': {
    en: 'The specified agent does not exist.',
    zh: '\u6307\u5b9a\u7684\u4ee3\u7406\u4e0d\u5b58\u5728\u3002',
  },
  'error.keyRevoked': {
    en: 'The signing key has been revoked.',
    zh: '\u7b7e\u540d\u5bc6\u94a5\u5df2\u88ab\u64a4\u9500\u3002',
  },
  'error.agentFrozen': {
    en: 'The agent is frozen and cannot submit operations.',
    zh: '\u8be5\u4ee3\u7406\u5df2\u88ab\u51bb\u7ed3\uff0c\u65e0\u6cd5\u63d0\u4ea4\u64cd\u4f5c\u3002',
  },
  'error.ttlExpired': {
    en: 'The operation TTL has expired.',
    zh: '\u64cd\u4f5c\u7684 TTL \u5df2\u8fc7\u671f\u3002',
  },
  'error.replayDetected': {
    en: 'A duplicate nonce was detected (replay attack).',
    zh: '\u68c0\u6d4b\u5230\u91cd\u590d\u7684\u968f\u673a\u6570\uff08\u91cd\u653e\u653b\u51fb\uff09\u3002',
  },
  'error.prevHashMismatch': {
    en: 'The previous chain hash does not match.',
    zh: '\u524d\u4e00\u4e2a\u94fe\u54c8\u5e0c\u4e0d\u5339\u914d\u3002',
  },
  'error.payloadTooLarge': {
    en: 'The operation payload exceeds the maximum allowed size.',
    zh: '\u64cd\u4f5c\u8d1f\u8f7d\u8d85\u8fc7\u4e86\u5141\u8bb8\u7684\u6700\u5927\u5927\u5c0f\u3002',
  },
  'error.rateLimited': {
    en: 'Too many requests. Please retry later.',
    zh: '\u8bf7\u6c42\u8fc7\u591a\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\u3002',
  },
  'error.internalError': {
    en: 'An internal server error occurred.',
    zh: '\u53d1\u751f\u4e86\u5185\u90e8\u670d\u52a1\u5668\u9519\u8bef\u3002',
  },
  'error.unauthorized': {
    en: 'Authentication is required.',
    zh: '\u9700\u8981\u8eab\u4efd\u9a8c\u8bc1\u3002',
  },
  'error.forbidden': {
    en: 'You do not have permission to perform this action.',
    zh: '\u60a8\u6ca1\u6709\u6267\u884c\u6b64\u64cd\u4f5c\u7684\u6743\u9650\u3002',
  },
  'error.notFound': {
    en: 'The requested resource was not found.',
    zh: '\u672a\u627e\u5230\u8bf7\u6c42\u7684\u8d44\u6e90\u3002',
  },
  'error.validationError': {
    en: 'The request failed validation.',
    zh: '\u8bf7\u6c42\u672a\u901a\u8fc7\u9a8c\u8bc1\u3002',
  },

  // ---------------------------------------------------------------------------
  // Auth middleware  (src/middleware/auth.ts)
  // ---------------------------------------------------------------------------
  'auth.missingHeader': {
    en: 'Missing Authorization header',
    zh: '\u7f3a\u5c11 Authorization \u8bf7\u6c42\u5934',
  },
  'auth.bearerRequired': {
    en: 'Authorization header must use Bearer scheme',
    zh: 'Authorization \u8bf7\u6c42\u5934\u5fc5\u987b\u4f7f\u7528 Bearer \u65b9\u6848',
  },
  'auth.emptyToken': {
    en: 'Empty Bearer token',
    zh: 'Bearer \u4ee4\u724c\u4e3a\u7a7a',
  },
  'auth.ttlMustBePositive': {
    en: 'ttl_seconds must be a positive number',
    zh: 'ttl_seconds \u5fc5\u987b\u4e3a\u6b63\u6570',
  },

  // ---------------------------------------------------------------------------
  // RBAC middleware  (src/middleware/rbac.ts)
  // ---------------------------------------------------------------------------
  'rbac.noRole': {
    en: 'No role found in request context',
    zh: '\u8bf7\u6c42\u4e0a\u4e0b\u6587\u4e2d\u672a\u627e\u5230\u89d2\u8272',
  },
  'rbac.insufficientPermissions': {
    en: 'Insufficient permissions for this action.',
    zh: '\u6743\u9650\u4e0d\u8db3\uff0c\u65e0\u6cd5\u6267\u884c\u6b64\u64cd\u4f5c\u3002',
  },

  // ---------------------------------------------------------------------------
  // 404 handler  (src/index.ts)
  // ---------------------------------------------------------------------------
  'notFound.resource': {
    en: 'The requested resource was not found.',
    zh: '\u672a\u627e\u5230\u8bf7\u6c42\u7684\u8d44\u6e90\u3002',
  },

  // ---------------------------------------------------------------------------
  // Auth routes  (src/routes/auth.ts)
  // ---------------------------------------------------------------------------
  'auth.ttlMustBeNumber': {
    en: 'ttl_seconds must be a number or null',
    zh: 'ttl_seconds \u5fc5\u987b\u4e3a\u6570\u5b57\u6216 null',
  },

  // ---------------------------------------------------------------------------
  // Agent routes  (src/routes/agents.ts)
  // ---------------------------------------------------------------------------
  'agent.missingAgentId': {
    en: 'Missing required field "agent_id".',
    zh: '\u7f3a\u5c11\u5fc5\u586b\u5b57\u6bb5 "agent_id"\u3002',
  },
  'agent.missingIntegrationType': {
    en: 'Missing required field "integration_type".',
    zh: '\u7f3a\u5c11\u5fc5\u586b\u5b57\u6bb5 "integration_type"\u3002',
  },
  'agent.missingReason': {
    en: 'Missing required field "reason".',
    zh: '\u7f3a\u5c11\u5fc5\u586b\u5b57\u6bb5 "reason"\u3002',
  },
  'agent.missingKid': {
    en: 'Missing required field "kid".',
    zh: '\u7f3a\u5c11\u5fc5\u586b\u5b57\u6bb5 "kid"\u3002',
  },

  // ---------------------------------------------------------------------------
  // Operation routes  (src/routes/operations.ts)
  // ---------------------------------------------------------------------------
  'operation.invalidBody': {
    en: 'Invalid or empty request body.',
    zh: '\u8bf7\u6c42\u4f53\u65e0\u6548\u6216\u4e3a\u7a7a\u3002',
  },

  // ---------------------------------------------------------------------------
  // Export routes  (src/routes/exports.ts)
  // ---------------------------------------------------------------------------
  'export.bodyRequired': {
    en: 'Request body is required.',
    zh: '\u8bf7\u6c42\u4f53\u4e3a\u5fc5\u586b\u9879\u3002',
  },
  'export.notFoundById': {
    en: 'Export "{id}" not found.',
    zh: '\u672a\u627e\u5230\u5bfc\u51fa\u8bb0\u5f55 "{id}"\u3002',
  },
  'export.notYetComplete': {
    en: 'Export is not yet complete.',
    zh: '\u5bfc\u51fa\u5c1a\u672a\u5b8c\u6210\u3002',
  },
  'export.fileNotFound': {
    en: 'Export file not found in storage.',
    zh: '\u5728\u5b58\u50a8\u4e2d\u672a\u627e\u5230\u5bfc\u51fa\u6587\u4ef6\u3002',
  },

  // ---------------------------------------------------------------------------
  // Auth service  (src/services/auth-service.ts)
  // ---------------------------------------------------------------------------
  'auth.emailPasswordRequired': {
    en: 'Email and password (8-128 chars) are required',
    zh: '\u7535\u5b50\u90ae\u4ef6\u548c\u5bc6\u7801\uff088-128 \u4e2a\u5b57\u7b26\uff09\u4e3a\u5fc5\u586b\u9879',
  },
  'auth.emailAlreadyRegistered': {
    en: 'Email already registered',
    zh: '\u7535\u5b50\u90ae\u4ef6\u5df2\u88ab\u6ce8\u518c',
  },
  'auth.invalidCredentials': {
    en: 'Invalid email or password',
    zh: '\u7535\u5b50\u90ae\u4ef6\u6216\u5bc6\u7801\u65e0\u6548',
  },
  'auth.accountSuspended': {
    en: 'Account is suspended',
    zh: '\u8d26\u6237\u5df2\u88ab\u505c\u7528',
  },
  'auth.userNotFound': {
    en: 'User not found',
    zh: '\u672a\u627e\u5230\u7528\u6237',
  },

  // ---------------------------------------------------------------------------
  // Agent service  (src/services/agent-service.ts)
  // ---------------------------------------------------------------------------
  'agent.alreadyExists': {
    en: 'An agent with id "{id}" already exists.',
    zh: 'ID \u4e3a "{id}" \u7684\u4ee3\u7406\u5df2\u5b58\u5728\u3002',
  },
  'agent.atLeastOneKey': {
    en: 'At least one signing key must be provided.',
    zh: '\u5fc5\u987b\u63d0\u4f9b\u81f3\u5c11\u4e00\u4e2a\u7b7e\u540d\u5bc6\u94a5\u3002',
  },
  'agent.notFound': {
    en: 'Agent "{id}" not found.',
    zh: '\u672a\u627e\u5230\u4ee3\u7406 "{id}"\u3002',
  },
  'agent.invalidIntegrationType': {
    en: 'Invalid integration_type "{value}". Must be one of: {valid}.',
    zh: '\u65e0\u6548\u7684 integration_type "{value}"\u3002\u5fc5\u987b\u4e3a\u4ee5\u4e0b\u4e4b\u4e00\uff1a{valid}\u3002',
  },
  'agent.alreadyFrozen': {
    en: 'Agent "{id}" is already frozen.',
    zh: '\u4ee3\u7406 "{id}" \u5df2\u88ab\u51bb\u7ed3\u3002',
  },
  'agent.permanentlyRevoked': {
    en: 'Agent "{id}" has been permanently revoked.',
    zh: '\u4ee3\u7406 "{id}" \u5df2\u88ab\u6c38\u4e45\u64a4\u9500\u3002',
  },
  'agent.notFrozen': {
    en: 'Agent "{id}" is not frozen.',
    zh: '\u4ee3\u7406 "{id}" \u672a\u88ab\u51bb\u7ed3\u3002',
  },
  'agent.permanentlyRevokedCannotUnfreeze': {
    en: 'Agent "{id}" has been permanently revoked and cannot be unfrozen.',
    zh: '\u4ee3\u7406 "{id}" \u5df2\u88ab\u6c38\u4e45\u64a4\u9500\uff0c\u65e0\u6cd5\u89e3\u51bb\u3002',
  },
  'agent.keyNotFound': {
    en: 'Key "{kid}" not found for agent "{id}".',
    zh: '\u672a\u627e\u5230\u4ee3\u7406 "{id}" \u7684\u5bc6\u94a5 "{kid}"\u3002',
  },
  'agent.keyAlreadyRevoked': {
    en: 'Key "{kid}" is already revoked.',
    zh: '\u5bc6\u94a5 "{kid}" \u5df2\u88ab\u64a4\u9500\u3002',
  },
  'agent.unsupportedAlgorithm': {
    en: 'Unsupported key algorithm "{algorithm}". Only "ed25519" is supported.',
    zh: '\u4e0d\u652f\u6301\u7684\u5bc6\u94a5\u7b97\u6cd5 "{algorithm}"\u3002\u4ec5\u652f\u6301 "ed25519"\u3002',
  },
  'agent.invalidPublicKeyLength': {
    en: 'Public key "{kid}" must be 32 bytes (got {actual}).',
    zh: '\u516c\u94a5 "{kid}" \u5fc5\u987b\u4e3a 32 \u5b57\u8282\uff08\u5b9e\u9645 {actual}\uff09\u3002',
  },
  'agent.invalidPublicKeyEncoding': {
    en: 'Public key "{kid}" has invalid base64url encoding.',
    zh: '\u516c\u94a5 "{kid}" \u7684 base64url \u7f16\u7801\u65e0\u6548\u3002',
  },

  // ---------------------------------------------------------------------------
  // Operation service  (src/services/operation-service.ts)
  // ---------------------------------------------------------------------------
  'operation.agentNotRegistered': {
    en: 'Agent "{id}" is not registered.',
    zh: '\u4ee3\u7406 "{id}" \u672a\u6ce8\u518c\u3002',
  },
  'operation.agentRevoked': {
    en: 'The agent has been permanently revoked.',
    zh: '\u8be5\u4ee3\u7406\u5df2\u88ab\u6c38\u4e45\u64a4\u9500\u3002',
  },
  'operation.keyNotFoundForAgent': {
    en: 'Key "{kid}" not found for agent "{id}".',
    zh: '\u672a\u627e\u5230\u4ee3\u7406 "{id}" \u7684\u5bc6\u94a5 "{kid}"\u3002',
  },
  'operation.keyRetired': {
    en: 'The signing key has been retired.',
    zh: '\u7b7e\u540d\u5bc6\u94a5\u5df2\u88ab\u505c\u7528\u3002',
  },
  'operation.prevHashMismatch': {
    en: 'Expected prev_chain_hash "{expected}", got "{actual}".',
    zh: '\u9884\u671f\u7684 prev_chain_hash \u4e3a "{expected}"\uff0c\u5b9e\u9645\u4e3a "{actual}"\u3002',
  },
  'operation.unsupportedVersion': {
    en: 'Unsupported op_version "{version}". Only "1.0" is supported.',
    zh: '\u4e0d\u652f\u6301\u7684 op_version "{version}"\u3002\u4ec5\u652f\u6301 "1.0"\u3002',
  },
  'operation.missingField': {
    en: 'Missing or empty required field "{field}".',
    zh: '\u7f3a\u5c11\u6216\u4e3a\u7a7a\u7684\u5fc5\u586b\u5b57\u6bb5 "{field}"\u3002',
  },
  'operation.nonceTooLong': {
    en: 'Nonce exceeds maximum length of {max} characters.',
    zh: '\u968f\u673a\u6570\u8d85\u8fc7\u4e86\u6700\u5927\u957f\u5ea6 {max} \u4e2a\u5b57\u7b26\u3002',
  },
  'operation.invalidIssuedAt': {
    en: 'Invalid "issued_at" timestamp.',
    zh: '\u65e0\u6548\u7684 "issued_at" \u65f6\u95f4\u6233\u3002',
  },
  'operation.missingTtl': {
    en: 'Missing or invalid "ttl_ms".',
    zh: '\u7f3a\u5c11\u6216\u65e0\u6548\u7684 "ttl_ms"\u3002',
  },
  'operation.ttlTooLow': {
    en: 'ttl_ms must be at least {min}ms.',
    zh: 'ttl_ms \u81f3\u5c11\u4e3a {min} \u6beb\u79d2\u3002',
  },
  'operation.ttlTooHigh': {
    en: 'ttl_ms must not exceed {max}ms.',
    zh: 'ttl_ms \u4e0d\u80fd\u8d85\u8fc7 {max} \u6beb\u79d2\u3002',
  },
  'operation.notFound': {
    en: 'Operation "{id}" not found.',
    zh: '\u672a\u627e\u5230\u64cd\u4f5c "{id}"\u3002',
  },

  // ---------------------------------------------------------------------------
  // Export service  (src/services/export-service.ts)
  // ---------------------------------------------------------------------------
  'export.invalidStartTime': {
    en: 'Invalid "start_time" parameter.',
    zh: '\u65e0\u6548\u7684 "start_time" \u53c2\u6570\u3002',
  },
  'export.invalidEndTime': {
    en: 'Invalid "end_time" parameter.',
    zh: '\u65e0\u6548\u7684 "end_time" \u53c2\u6570\u3002',
  },
  'export.startBeforeEnd': {
    en: '"start_time" must be before "end_time".',
    zh: '"start_time" \u5fc5\u987b\u65e9\u4e8e "end_time"\u3002',
  },
  'export.invalidFormat': {
    en: 'Format must be "json" or "pdf".',
    zh: '\u683c\u5f0f\u5fc5\u987b\u4e3a "json" \u6216 "pdf"\u3002',
  },
  'export.notFound': {
    en: 'Export "{id}" not found.',
    zh: '\u672a\u627e\u5230\u5bfc\u51fa\u8bb0\u5f55 "{id}"\u3002',
  },

  // ---------------------------------------------------------------------------
  // Audit service  (src/services/audit-service.ts)
  // ---------------------------------------------------------------------------
  'audit.invalidStartTime': {
    en: 'Invalid "start_time" parameter.',
    zh: '\u65e0\u6548\u7684 "start_time" \u53c2\u6570\u3002',
  },
  'audit.invalidEndTime': {
    en: 'Invalid "end_time" parameter.',
    zh: '\u65e0\u6548\u7684 "end_time" \u53c2\u6570\u3002',
  },
  'audit.invalidCursor': {
    en: 'Invalid cursor value.',
    zh: '\u65e0\u6548\u7684\u6e38\u6807\u503c\u3002',
  },

  // ---------------------------------------------------------------------------
  // Epoch service  (src/services/epoch-service.ts)
  // ---------------------------------------------------------------------------
  'epoch.notFound': {
    en: 'Epoch "{id}" not found.',
    zh: '\u672a\u627e\u5230\u65f6\u671f "{id}"\u3002',
  },
};

/**
 * Retrieve a translated message by key.
 *
 * Supports simple {param} placeholder interpolation via the optional
 * `params` argument.
 *
 * Falls back to English if the requested language is missing, and
 * ultimately falls back to the raw key if no translation exists at all.
 */
export function getMessage(
  key: string,
  lang: Lang = 'en',
  params?: Record<string, string | number>,
): string {
  let msg = messages[key]?.[lang] ?? messages[key]?.en ?? key;
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      msg = msg.replaceAll(`{${k}}`, String(v));
    }
  }
  return msg;
}

/**
 * Detect the preferred language from an Accept-Language header value.
 *
 * Returns 'zh' if the header contains any Chinese locale indicator,
 * otherwise defaults to 'en'.
 */
export function detectLanguage(acceptLanguage: string | null | undefined): Lang {
  if (!acceptLanguage) return 'en';
  if (acceptLanguage.includes('zh')) return 'zh';
  return 'en';
}
