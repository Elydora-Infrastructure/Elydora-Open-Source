import type { ErrorCode } from '../types/enums.js';

/** Map of all Elydora error codes to default human-readable messages */
export const ERROR_CODES: Readonly<Record<ErrorCode, string>> = {
  INVALID_SIGNATURE: 'The operation signature is invalid.',
  UNKNOWN_AGENT: 'The specified agent does not exist.',
  KEY_REVOKED: 'The signing key has been revoked.',
  AGENT_FROZEN: 'The agent is frozen and cannot submit operations.',
  TTL_EXPIRED: 'The operation TTL has expired.',
  REPLAY_DETECTED: 'A duplicate nonce was detected (replay attack).',
  PREV_HASH_MISMATCH: 'The previous chain hash does not match.',
  PAYLOAD_TOO_LARGE: 'The operation payload exceeds the maximum allowed size.',
  RATE_LIMITED: 'Too many requests. Please retry later.',
  INTERNAL_ERROR: 'An internal server error occurred.',
  UNAUTHORIZED: 'Authentication is required.',
  FORBIDDEN: 'You do not have permission to perform this action.',
  NOT_FOUND: 'The requested resource was not found.',
  VALIDATION_ERROR: 'The request failed validation.',
} as const;
