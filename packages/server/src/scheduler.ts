/**
 * Epoch scheduler — runs every 5 minutes to create Merkle tree rollups.
 *
 * Run as a separate Node.js process: node dist/scheduler.js
 */

import 'dotenv/config';
import cron from 'node-cron';
import { PostgresAdapter } from './adapters/postgres.js';
import { MinioAdapter } from './adapters/minio.js';
import { RedisCacheAdapter } from './adapters/redis-cache.js';
import { BullMQAdapter } from './adapters/bullmq.js';
import { createEpoch } from './services/epoch-service.js';
import { DEFAULT_EPOCH_INTERVAL_MS } from './shared/index.js';

// ---------------------------------------------------------------------------
// Initialize adapters
// ---------------------------------------------------------------------------

const db = new PostgresAdapter(process.env.DATABASE_URL!);
const evidence = new MinioAdapter(
  process.env.MINIO_ENDPOINT!,
  process.env.MINIO_ACCESS_KEY!,
  process.env.MINIO_SECRET_KEY!,
  process.env.MINIO_BUCKET ?? 'elydora-evidence',
);
const cache = new RedisCacheAdapter(process.env.REDIS_URL!);
const redisUrl = new URL(process.env.REDIS_URL!);
const queue = new BullMQAdapter({
  host: redisUrl.hostname,
  port: Number(redisUrl.port) || 6379,
});

await cache.connect();

// ---------------------------------------------------------------------------
// Epoch creation logic (mirrors the Cloudflare scheduled handler)
// ---------------------------------------------------------------------------

async function runEpochCreation(): Promise<void> {
  const now = Date.now();
  const interval = DEFAULT_EPOCH_INTERVAL_MS;
  const epochEnd = Math.floor(now / interval) * interval;
  const epochStart = epochEnd - interval;

  const { results: orgs } = await db
    .prepare(
      'SELECT DISTINCT org_id FROM operations WHERE created_at >= ? AND created_at < ?',
    )
    .bind(epochStart, epochEnd)
    .all<{ org_id: string }>();

  if (!orgs || orgs.length === 0) return;

  for (const { org_id } of orgs) {
    try {
      const epoch = await createEpoch(
        db,
        evidence,
        process.env.ELYDORA_SIGNING_KEY!,
        org_id,
        epochStart,
        epochEnd,
      );

      if (epoch) {
        try {
          await queue.send({
            type: 'tsa',
            epoch_id: epoch.epoch_id,
            org_id: epoch.org_id,
            root_hash: epoch.root_hash,
            r2_epoch_key: epoch.r2_epoch_key,
          });
        } catch (tsaError) {
          console.error(
            `Failed to enqueue TSA anchor for epoch ${epoch.epoch_id}:`,
            tsaError instanceof Error ? tsaError.message : String(tsaError),
          );
        }
      }
    } catch (error) {
      console.error(
        `Failed to create epoch for org ${org_id}:`,
        error instanceof Error ? error.message : String(error),
      );
    }
  }
}

// ---------------------------------------------------------------------------
// Schedule: every 5 minutes
// ---------------------------------------------------------------------------

cron.schedule('*/5 * * * *', async () => {
  console.log(`[${new Date().toISOString()}] Running epoch creation...`);
  try {
    await runEpochCreation();
  } catch (error) {
    console.error('Epoch creation failed:', error);
  }
});

console.log('Elydora epoch scheduler started (every 5 minutes)');

// Graceful shutdown
async function shutdown() {
  console.log('Shutting down scheduler...');
  await cache.close();
  await queue.close();
  await db.close();
  process.exit(0);
}

process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);
