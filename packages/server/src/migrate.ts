import { readdir, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import pg from 'pg';

const MIGRATIONS_DIR = process.env.MIGRATIONS_DIR ?? join(process.cwd(), 'migrations');

/** Leading digits of a migration filename, e.g. 004_x.sql becomes 4. */
function versionOf(file: string): number {
  const match = /^(\d+)_/.exec(file);
  if (!match) throw new Error(`Migration ${file} does not start with a version number.`);
  return Number(match[1]);
}

async function appliedVersions(client: pg.Client): Promise<Set<number>> {
  const exists = await client.query("SELECT to_regclass('schema_versions') AS name");
  if (!exists.rows[0]?.name) return new Set();
  const rows = await client.query<{ version: string }>('SELECT version FROM schema_versions');
  return new Set(rows.rows.map((row) => Number(row.version)));
}

async function main(): Promise<void> {
  const connectionString = process.env.DATABASE_URL;
  if (!connectionString) throw new Error('DATABASE_URL is required.');

  const files = (await readdir(MIGRATIONS_DIR))
    .filter((file) => file.endsWith('.sql'))
    .sort((a, b) => versionOf(a) - versionOf(b));

  const client = new pg.Client({ connectionString });
  await client.connect();
  try {
    const applied = await appliedVersions(client);
    for (const file of files) {
      if (applied.has(versionOf(file))) {
        console.log(`Skipping ${file} (already applied)`);
        continue;
      }
      console.log(`Applying ${file}`);
      await client.query(await readFile(join(MIGRATIONS_DIR, file), 'utf8'));
    }
  } finally {
    await client.end();
  }
}

main().catch((error) => {
  console.error('migration failed:', error);
  process.exit(1);
});
