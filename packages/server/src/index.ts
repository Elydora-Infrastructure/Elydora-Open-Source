/**
 * Elydora API — Node.js entry point.
 */

import 'dotenv/config';
import { serve } from '@hono/node-server';
import { Hono } from 'hono';
import { cors } from 'hono/cors';
import type { Env, AppVariables } from './types.js';
import { requestIdMiddleware } from './middleware/request-id.js';
import { i18nMiddleware } from './middleware/i18n.js';
import { globalErrorHandler } from './middleware/error-handler.js';
import { auth } from './routes/auth.js';
import { agents } from './routes/agents.js';
import { operations } from './routes/operations.js';
import { audit } from './routes/audit.js';
import { epochs } from './routes/epochs.js';
import { exports_ } from './routes/exports.js';
import { jwks } from './routes/jwks.js';
import { getMessage } from './i18n/messages.js';

// Adapter imports
import { PostgresAdapter } from './adapters/postgres.js';
import { MinioAdapter } from './adapters/minio.js';
import { RedisCacheAdapter } from './adapters/redis-cache.js';
import { BullMQAdapter } from './adapters/bullmq.js';

// ---------------------------------------------------------------------------
// Initialize adapters from environment variables
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

// ---------------------------------------------------------------------------
// Validate required secrets at startup
// ---------------------------------------------------------------------------

const requiredSecrets = [
  { name: 'BETTER_AUTH_SECRET', value: process.env.BETTER_AUTH_SECRET },
  { name: 'ELYDORA_SIGNING_KEY', value: process.env.ELYDORA_SIGNING_KEY },
];
for (const { name, value } of requiredSecrets) {
  if (!value || value.startsWith('change-me')) {
    console.error(`FATAL: ${name} is not configured. Run ./scripts/install.sh to generate secrets.`);
    process.exit(1);
  }
}

// ---------------------------------------------------------------------------
// Build the env bindings object
// ---------------------------------------------------------------------------

const env: Env = {
  ELYDORA_DB: db,
  ELYDORA_EVIDENCE: evidence,
  ELYDORA_CACHE: cache,
  ELYDORA_QUEUE: queue,
  ENVIRONMENT: process.env.ENVIRONMENT ?? 'production',
  API_VERSION: process.env.API_VERSION ?? 'v1',
  PROTOCOL_VERSION: process.env.PROTOCOL_VERSION ?? '1.0',
  BETTER_AUTH_SECRET: process.env.BETTER_AUTH_SECRET!,
  BETTER_AUTH_URL: process.env.BETTER_AUTH_URL ?? 'http://localhost:8787',
  ELYDORA_SIGNING_KEY: process.env.ELYDORA_SIGNING_KEY!,
  ALLOWED_ORIGINS: process.env.ALLOWED_ORIGINS ?? '',
  TSA_URL: process.env.TSA_URL,
};

// ---------------------------------------------------------------------------
// Create the Hono application
// ---------------------------------------------------------------------------

const app = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// Inject env bindings into every request context
app.use('/*', async (c, next) => {
  // Copy all env bindings onto c.env
  Object.assign(c.env, env);
  await next();
});

// CORS — always restrict to the configured allowlist
app.use(
  '/*',
  cors({
    origin: (origin, c) => {
      const raw = c.env.ALLOWED_ORIGINS ?? 'http://localhost:3000,http://localhost:8787';
      const allowedOrigins = raw.split(',').map((s: string) => s.trim()).filter(Boolean);
      return allowedOrigins.includes(origin) ? origin : '';
    },
    allowMethods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'],
    allowHeaders: [
      'Content-Type',
      'Authorization',
      'X-Elydora-Protocol-Version',
      'X-Elydora-Signature',
      'X-Request-Id',
    ],
    exposeHeaders: [
      'X-Request-Id',
      'X-Elydora-Protocol-Version',
      'X-RateLimit-Limit',
      'X-RateLimit-Remaining',
      'X-RateLimit-Reset',
    ],
    maxAge: 86400,
    credentials: true,
  }),
);

// Security headers
app.use('/*', async (c, next) => {
  await next();
  c.header('X-Content-Type-Options', 'nosniff');
  c.header('X-Frame-Options', 'DENY');
  c.header('Referrer-Policy', 'strict-origin-when-cross-origin');
  c.header('Permissions-Policy', 'camera=(), microphone=(), geolocation=()');
  c.header('Content-Security-Policy', "default-src 'none'; frame-ancestors 'none'");
  if (c.env.ENVIRONMENT === 'production') {
    c.header('Strict-Transport-Security', 'max-age=63072000; includeSubDomains; preload');
  }
});

// Request ID
app.use('/*', requestIdMiddleware);

// Language detection
app.use('/*', i18nMiddleware);

// Global error handler
app.onError(globalErrorHandler);

// Health check
app.get('/v1/health', (c) => {
  return c.json(
    {
      status: 'healthy',
      version: c.env.API_VERSION,
      protocol_version: c.env.PROTOCOL_VERSION,
      timestamp: Date.now(),
    },
    200,
  );
});

// Route groups
app.route('/.well-known/elydora/jwks.json', jwks);
app.route('/v1/auth', auth);
app.route('/v1/agents', agents);
app.route('/v1/operations', operations);
app.route('/v1/audit', audit);
app.route('/v1/epochs', epochs);
app.route('/v1/exports', exports_);

// 404 fallback
app.notFound((c) => {
  const requestId = c.get('request_id') ?? 'unknown';
  const lang = c.get('lang') ?? 'en';
  return c.json(
    {
      error: {
        code: 'NOT_FOUND',
        message: getMessage('notFound.resource', lang),
        request_id: requestId,
      },
    },
    404,
  );
});

// ---------------------------------------------------------------------------
// Start the server
// ---------------------------------------------------------------------------

const port = Number(process.env.PORT) || 8787;

await cache.connect();
console.log(`Elydora API server starting on port ${port}`);

serve({ fetch: app.fetch, port }, (info) => {
  console.log(`Elydora API server listening on http://localhost:${info.port}`);
});

// Graceful shutdown
process.on('SIGTERM', async () => {
  console.log('SIGTERM received, shutting down...');
  await cache.close();
  await db.close();
  process.exit(0);
});

process.on('SIGINT', async () => {
  console.log('SIGINT received, shutting down...');
  await cache.close();
  await db.close();
  process.exit(0);
});
