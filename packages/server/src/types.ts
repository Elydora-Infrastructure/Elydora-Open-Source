/**
 * Environment bindings for the ElydoraOpenSource Node.js server.
 *
 * Replaces Cloudflare-specific bindings (D1Database, R2Bucket, KVNamespace,
 * Queue) with adapter interfaces backed by PostgreSQL, MinIO, Redis, and
 * BullMQ respectively.
 */

import type { Database, ObjectStore, Cache, MessageQueue } from './adapters/interfaces.js';

export interface Env {
  /** PostgreSQL adapter for relational storage */
  ELYDORA_DB: Database;

  /** MinIO/S3 adapter for evidence payloads, receipts, and export bundles */
  ELYDORA_EVIDENCE: ObjectStore;

  /** Redis adapter for caching (nonces, rate-limit counters, etc.) */
  ELYDORA_CACHE: Cache;

  /** BullMQ adapter for async operation processing and export jobs */
  ELYDORA_QUEUE: MessageQueue;

  /** Deployment environment identifier (e.g. "production", "staging") */
  ENVIRONMENT: string;

  /** Current API version (e.g. "v1") */
  API_VERSION: string;

  /** Current Elydora protocol version (e.g. "1.0") */
  PROTOCOL_VERSION: string;

  /** HMAC secret used for JWT verification */
  JWT_SECRET: string;

  /** Base64url-encoded Ed25519 private key used by the server to sign receipts */
  ELYDORA_SIGNING_KEY: string;

  /** Comma-separated list of allowed CORS origins (used in production) */
  ALLOWED_ORIGINS: string;

  /** Optional TSA endpoint URL (defaults to Sectigo if unset) */
  TSA_URL?: string;
}

/**
 * Variables set by middleware and available via `c.get(...)` / `c.var`.
 */
export interface AppVariables {
  /** UUIDv7 request identifier injected by the request-id middleware */
  request_id: string;

  /** Organization identifier extracted from the verified JWT */
  org_id: string;

  /** RBAC role extracted from the verified JWT */
  role: import('./shared/index.js').RbacRole;

  /** Actor identifier (sub claim) extracted from the verified JWT */
  actor: string;

  /** Detected language from Accept-Language header (set by i18n middleware) */
  lang: import('./i18n/messages.js').Lang;
}
