/**
 * BullMQ queue worker — processes operation, export, and TSA jobs.
 *
 * Run as a separate Node.js process: node dist/worker.js
 */

import 'dotenv/config';
import { Worker, type Job } from 'bullmq';
import { QUEUE_NAME } from './adapters/bullmq.js';
import { PostgresAdapter } from './adapters/postgres.js';
import { MinioAdapter } from './adapters/minio.js';
import { RedisCacheAdapter } from './adapters/redis-cache.js';
import type { Env } from './types.js';
import type { Operation } from './shared/index.js';
import { buildBrandedPDF } from './utils/pdf.js';
import { requestTimestamp, DEFAULT_TSA_URL } from './utils/tsa.js';
import { base64urlDecode, base64urlEncode } from './utils/crypto.js';

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

// Build env object for service compatibility
const env: Pick<Env, 'ELYDORA_DB' | 'ELYDORA_EVIDENCE' | 'ELYDORA_CACHE' | 'TSA_URL'> = {
  ELYDORA_DB: db,
  ELYDORA_EVIDENCE: evidence,
  ELYDORA_CACHE: cache,
  TSA_URL: process.env.TSA_URL,
};

// ---------------------------------------------------------------------------
// Message type
// ---------------------------------------------------------------------------

interface QueueMessageBody {
  type: 'operation' | 'export' | 'tsa';
  operation_id?: string;
  org_id?: string;
  agent_id?: string;
  export_id?: string;
  query_params?: string;
  epoch_id?: string;
  root_hash?: string;
  r2_epoch_key?: string;
}

// ---------------------------------------------------------------------------
// Job processor
// ---------------------------------------------------------------------------

async function processJob(job: Job): Promise<void> {
  const body = job.data as QueueMessageBody;

  switch (body.type) {
    case 'operation':
      await processOperation(body);
      break;
    case 'export':
      await processExport(body);
      break;
    case 'tsa':
      await processTSA(body);
      break;
    default:
      console.error(`Unknown queue message type: ${String((body as unknown as Record<string, unknown>).type)}`);
  }
}

// ---------------------------------------------------------------------------
// Operation post-processing
// ---------------------------------------------------------------------------

async function processOperation(body: QueueMessageBody): Promise<void> {
  if (!body.operation_id || !body.org_id) {
    throw new Error('Operation queue message missing required fields.');
  }

  const operation = await env.ELYDORA_DB
    .prepare('SELECT * FROM operations WHERE operation_id = ?')
    .bind(body.operation_id)
    .first<Operation>();

  if (!operation) {
    throw new Error(`Operation ${body.operation_id} not found in database.`);
  }

  const cacheKey = `chain:${operation.agent_id}:latest`;
  await env.ELYDORA_CACHE.put(
    cacheKey,
    JSON.stringify({
      chain_hash: operation.chain_hash,
      seq_no: operation.seq_no,
    }),
    { expirationTtl: 86400 },
  );
}

// ---------------------------------------------------------------------------
// Export job processing
// ---------------------------------------------------------------------------

async function processExport(body: QueueMessageBody): Promise<void> {
  if (!body.export_id || !body.org_id || !body.query_params) {
    throw new Error('Export queue message missing required fields.');
  }

  const { export_id, org_id, query_params } = body;

  await env.ELYDORA_DB
    .prepare('UPDATE exports SET status = ? WHERE export_id = ?')
    .bind('running', export_id)
    .run();

  try {
    const params = JSON.parse(query_params) as {
      start_time: number;
      end_time: number;
      agent_id: string | null;
      operation_type: string | null;
      format: 'json' | 'pdf';
    };

    const conditions: string[] = ['org_id = ?', 'created_at >= ?', 'created_at <= ?'];
    const bindings: (string | number)[] = [org_id, params.start_time, params.end_time];

    if (params.agent_id) {
      conditions.push('agent_id = ?');
      bindings.push(params.agent_id);
    }

    if (params.operation_type) {
      conditions.push('operation_type = ?');
      bindings.push(params.operation_type);
    }

    const whereClause = 'WHERE ' + conditions.join(' AND ');
    const query = `SELECT * FROM operations ${whereClause} ORDER BY created_at ASC`;

    const result = await env.ELYDORA_DB
      .prepare(query)
      .bind(...bindings)
      .all<Operation>();

    const operations = result.results ?? [];

    let exportData: string | Uint8Array;
    let contentType: string;

    if (params.format === 'json') {
      exportData = JSON.stringify(
        {
          export_id,
          org_id,
          generated_at: Date.now(),
          query: {
            start_time: params.start_time,
            end_time: params.end_time,
            agent_id: params.agent_id,
            operation_type: params.operation_type,
          },
          total_operations: operations.length,
          operations,
        },
        null,
        2,
      );
      contentType = 'application/json';
    } else {
      const fmtDate = (ms: number) =>
        new Date(ms).toISOString().slice(0, 16).replace('T', ' ');

      exportData = buildBrandedPDF({
        export_id,
        org_id,
        generated_at: new Date().toISOString(),
        time_range: `${fmtDate(params.start_time)}  to  ${fmtDate(params.end_time)}`,
        agent_filter: params.agent_id ?? 'All agents',
        type_filter: params.operation_type ?? 'All types',
        total: operations.length,
        operations,
      });
      contentType = 'application/pdf';
    }

    const r2Key = `exports/${org_id}/${export_id}`;
    await env.ELYDORA_EVIDENCE.put(r2Key, exportData, {
      httpMetadata: { contentType },
      customMetadata: { export_id, org_id },
    });

    const now = Date.now();
    await env.ELYDORA_DB
      .prepare(
        'UPDATE exports SET status = ?, r2_export_key = ?, completed_at = ? WHERE export_id = ?',
      )
      .bind('done', r2Key, now, export_id)
      .run();
  } catch (error) {
    const now = Date.now();
    await env.ELYDORA_DB
      .prepare('UPDATE exports SET status = ?, completed_at = ? WHERE export_id = ?')
      .bind('failed', now, export_id)
      .run();

    throw error;
  }
}

// ---------------------------------------------------------------------------
// TSA anchoring
// ---------------------------------------------------------------------------

async function processTSA(body: QueueMessageBody): Promise<void> {
  if (!body.root_hash || !body.r2_epoch_key) {
    throw new Error('TSA queue message missing required fields.');
  }

  const { root_hash, r2_epoch_key } = body;
  const tsaUrl = env.TSA_URL ?? DEFAULT_TSA_URL;

  const hashBytes = base64urlDecode(root_hash);
  const tsaResponse = await requestTimestamp(hashBytes, tsaUrl);

  const anchorData = {
    tsa_token: base64urlEncode(tsaResponse),
    tsa_url: tsaUrl,
    anchored_at: Date.now(),
  };

  await env.ELYDORA_EVIDENCE.put(
    `${r2_epoch_key}/tsa-anchor`,
    JSON.stringify(anchorData),
    { httpMetadata: { contentType: 'application/json' } },
  );
}

// ---------------------------------------------------------------------------
// Start the BullMQ worker
// ---------------------------------------------------------------------------

const redisUrl = new URL(process.env.REDIS_URL!);
const connection = {
  host: redisUrl.hostname,
  port: Number(redisUrl.port) || 6379,
};

await cache.connect();

const worker = new Worker(QUEUE_NAME, processJob, {
  connection,
  concurrency: 5,
});

console.log(`Elydora queue worker started (queue: ${QUEUE_NAME})`);

worker.on('completed', (job) => {
  console.log(`Job ${job.id} completed`);
});

worker.on('failed', (job, err) => {
  console.error(`Job ${job?.id} failed:`, err.message);
});

// Graceful shutdown
async function shutdown() {
  console.log('Shutting down worker...');
  await worker.close();
  await cache.close();
  await db.close();
  process.exit(0);
}

process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);
