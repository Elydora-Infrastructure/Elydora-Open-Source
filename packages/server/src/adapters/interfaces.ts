/**
 * Adapter interfaces for ElydoraOpenSource infrastructure services.
 *
 * These interfaces mirror the shape of Cloudflare's D1, R2, KV, and Queue
 * APIs so that all existing service code can run unchanged against
 * PostgreSQL, MinIO, Redis, and BullMQ implementations.
 */

// ---------------------------------------------------------------------------
// Database (replaces D1Database)
// ---------------------------------------------------------------------------

/**
 * A prepared (and optionally bound) SQL statement.
 * Mirrors D1PreparedStatement's interface exactly.
 */
export interface PreparedStatement {
  /**
   * Bind positional parameters (? placeholders) to the statement.
   * Returns a new statement with the values set.
   */
  bind(...values: unknown[]): PreparedStatement;

  /** Execute and return the first row, or null if no rows match. */
  first<T = unknown>(): Promise<T | null>;

  /** Execute and return all matching rows. */
  all<T = unknown>(): Promise<{ results: T[] }>;

  /** Execute a write statement (INSERT/UPDATE/DELETE). */
  run(): Promise<{ success: boolean }>;
}

/**
 * Relational database adapter.
 * Mirrors the subset of D1Database methods used by Elydora services.
 */
export interface Database {
  /** Prepare a SQL statement for execution. */
  prepare(sql: string): PreparedStatement;

  /**
   * Execute multiple prepared statements atomically (within a transaction).
   * Mirrors D1Database.batch().
   */
  batch(statements: PreparedStatement[]): Promise<{ results: unknown[]; success: boolean }[]>;
}

// ---------------------------------------------------------------------------
// Object store (replaces R2Bucket)
// ---------------------------------------------------------------------------

export interface ObjectStorePutOptions {
  httpMetadata?: { contentType?: string };
  customMetadata?: Record<string, string>;
}

export interface ObjectStoreObject {
  /** Readable body stream of the stored object. */
  readonly body: ReadableStream;
  /** Object size in bytes, if known. */
  readonly size?: number;
  /** HTTP metadata associated with the object. */
  readonly httpMetadata?: { readonly contentType?: string };
  /** Parse the body as JSON. */
  json<T = unknown>(): Promise<T>;
  /** Return the body as a UTF-8 string. */
  text(): Promise<string>;
}

export interface ObjectStoreHead {
  readonly size?: number;
  readonly httpMetadata?: { readonly contentType?: string };
}

/**
 * Object/blob storage adapter.
 * Mirrors the subset of R2Bucket methods used by Elydora services.
 */
export interface ObjectStore {
  /** Upload an object to the store. */
  put(
    key: string,
    body: string | Uint8Array | ReadableStream,
    options?: ObjectStorePutOptions,
  ): Promise<void>;

  /** Retrieve an object, or null if not found. */
  get(key: string): Promise<ObjectStoreObject | null>;

  /** Check if an object exists (metadata only). Returns null if not found. */
  head(key: string): Promise<ObjectStoreHead | null>;
}

// ---------------------------------------------------------------------------
// Cache (replaces KVNamespace)
// ---------------------------------------------------------------------------

/**
 * Key/value cache adapter.
 * Mirrors the subset of KVNamespace methods used by Elydora services.
 */
export interface Cache {
  /** Retrieve a cached value, or null if not found / expired. */
  get(key: string): Promise<string | null>;

  /** Store a value in the cache with an optional TTL in seconds. */
  put(key: string, value: string, options?: { expirationTtl?: number }): Promise<void>;
}

// ---------------------------------------------------------------------------
// Message queue (replaces Queue)
// ---------------------------------------------------------------------------

/**
 * Message queue adapter.
 * Mirrors the subset of Cloudflare Queue methods used by Elydora services.
 */
export interface MessageQueue {
  /** Enqueue a message. Returns the assigned message ID. */
  send(body: unknown): Promise<{ messageId: string }>;
}
