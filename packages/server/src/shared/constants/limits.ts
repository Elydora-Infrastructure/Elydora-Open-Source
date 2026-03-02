/** Maximum payload size in bytes (256 KB) */
export const MAX_PAYLOAD_SIZE = 256 * 1024;

/** Maximum TTL in milliseconds (5 minutes) */
export const MAX_TTL_MS = 300_000;

/** Minimum TTL in milliseconds (1 second) */
export const MIN_TTL_MS = 1_000;

/** Maximum nonce length in characters */
export const MAX_NONCE_LENGTH = 64;

/** Maximum number of agents on the Starter plan */
export const STARTER_MAX_AGENTS = 10;

/** Default data retention period in days */
export const DEFAULT_RETENTION_DAYS = 30;

/** Default epoch interval in milliseconds (5 minutes) */
export const DEFAULT_EPOCH_INTERVAL_MS = 300_000;

/** Maximum number of results per query */
export const MAX_QUERY_LIMIT = 1000;

/** Default number of results per query */
export const DEFAULT_QUERY_LIMIT = 100;

/** Default JWT token lifetime in seconds (24 hours) */
export const DEFAULT_JWT_TTL_SECONDS = 86400;
