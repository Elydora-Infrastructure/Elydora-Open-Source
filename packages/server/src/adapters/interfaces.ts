/** Adapter interfaces for ElydoraOpenSource infrastructure services. */

// ---------------------------------------------------------------------------
// Database (replaces D1Database)
// ---------------------------------------------------------------------------

/** A prepared (and optionally bound) SQL statement. */
export interface PreparedStatement {
  /** Bind positional parameters (? placeholders) to the statement. */
  bind(...values: unknown[]): PreparedStatement;

  /** Execute and return the first row, or null if no rows match. */
  first<T = unknown>(): Promise<T | null>;

  /** Execute and return all matching rows. */
  all<T = unknown>(): Promise<{ results: T[] }>;

  /** Execute a write statement (INSERT/UPDATE/DELETE). */
  run(): Promise<{ success: boolean }>;
}

/** Relational database adapter. */
export interface Database {
  /** Prepare a SQL statement for execution. */
  prepare(sql: string): PreparedStatement;

  /** Execute multiple prepared statements atomically (within a transaction). */
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

/** Object/blob storage adapter. */
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

/** Key/value cache adapter. */
export interface Cache {
  /** Retrieve a cached value, or null if not found / expired. */
  get(key: string): Promise<string | null>;

  /** Store a value in the cache with an optional TTL in seconds. */
  put(key: string, value: string, options?: { expirationTtl?: number }): Promise<void>;
}

// ---------------------------------------------------------------------------
// Message queue (replaces Queue)
// ---------------------------------------------------------------------------

/** Message queue adapter. */
export interface MessageQueue {
  /** Enqueue a message under the caller's durable message ID. */
  send(messageId: string, body: unknown): Promise<void>;
}
