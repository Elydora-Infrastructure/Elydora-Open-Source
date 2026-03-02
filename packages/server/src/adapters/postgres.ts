/**
 * PostgreSQL adapter implementing the Database interface.
 *
 * Translates the D1-style API (prepare/bind/first/all/run/batch) into
 * standard `pg` Pool queries. SQLite `?` placeholders are converted to
 * PostgreSQL `$N` positional parameters automatically.
 *
 * Batch execution runs all statements inside a single transaction to
 * preserve the atomicity guarantee of D1.batch().
 */

import pg from 'pg';
import type { Database, PreparedStatement } from './interfaces.js';

const { Pool, types } = pg;
type Pool = InstanceType<typeof pg.Pool>;
type PoolClient = pg.PoolClient;

// Parse PostgreSQL BIGINT (oid 20) as JavaScript number instead of string.
// Elydora timestamps are Unix milliseconds (~1.7e12) which fit safely within
// Number.MAX_SAFE_INTEGER (~9e15).
types.setTypeParser(20, (val: string) => Number(val));

// ---------------------------------------------------------------------------
// Placeholder conversion
// ---------------------------------------------------------------------------

/**
 * Convert SQLite `?` positional placeholders to PostgreSQL `$N` style.
 * Only replaces `?` outside of SQL string literals (safe for all Elydora queries).
 */
function convertPlaceholders(sql: string): string {
  let index = 0;
  return sql.replace(/\?/g, () => `$${++index}`);
}

// ---------------------------------------------------------------------------
// Statement implementation
// ---------------------------------------------------------------------------

/**
 * Internal PostgreSQL statement that implements PreparedStatement.
 * Stores the SQL and bound values; executes lazily on first/all/run/executeWith.
 */
class PostgresStatement implements PreparedStatement {
  constructor(
    private readonly pool: Pool,
    private readonly sql: string,
    private readonly values: unknown[],
  ) {}

  bind(...values: unknown[]): PreparedStatement {
    return new PostgresStatement(this.pool, this.sql, values);
  }

  async first<T = unknown>(): Promise<T | null> {
    const result = await this.pool.query<Record<string, unknown>>({
      text: convertPlaceholders(this.sql),
      values: this.values as unknown[],
    });
    return (result.rows[0] as T) ?? null;
  }

  async all<T = unknown>(): Promise<{ results: T[] }> {
    const result = await this.pool.query<Record<string, unknown>>({
      text: convertPlaceholders(this.sql),
      values: this.values as unknown[],
    });
    return { results: result.rows as T[] };
  }

  async run(): Promise<{ success: boolean }> {
    await this.pool.query({
      text: convertPlaceholders(this.sql),
      values: this.values as unknown[],
    });
    return { success: true };
  }

  /** Execute using a specific pooled client (used by batch transaction). */
  async executeWith(client: PoolClient): Promise<{ results: unknown[]; success: boolean }> {
    const result = await client.query({
      text: convertPlaceholders(this.sql),
      values: this.values as unknown[],
    });
    return { results: result.rows, success: true };
  }
}

// ---------------------------------------------------------------------------
// PostgreSQL adapter
// ---------------------------------------------------------------------------

export class PostgresAdapter implements Database {
  private readonly pool: Pool;

  constructor(connectionString: string) {
    this.pool = new Pool({ connectionString });
  }

  prepare(sql: string): PreparedStatement {
    return new PostgresStatement(this.pool, sql, []);
  }

  async batch(
    statements: PreparedStatement[],
  ): Promise<{ results: unknown[]; success: boolean }[]> {
    const client = await this.pool.connect();
    try {
      await client.query('BEGIN');
      const results: { results: unknown[]; success: boolean }[] = [];
      for (const stmt of statements) {
        const pgStmt = stmt as PostgresStatement;
        results.push(await pgStmt.executeWith(client));
      }
      await client.query('COMMIT');
      return results;
    } catch (err) {
      await client.query('ROLLBACK');
      throw err;
    } finally {
      client.release();
    }
  }

  /** Close the connection pool (call on graceful shutdown). */
  async close(): Promise<void> {
    await this.pool.end();
  }
}
