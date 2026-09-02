import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import test from 'node:test';

import { Hono } from 'hono';
import { authMiddleware } from '../dist/middleware/auth.js';
import { auth } from '../dist/routes/auth.js';
import { authenticateApiToken, issueApiToken } from '../dist/services/api-token-service.js';

const NOW = Math.floor(Date.now() / 1000);

class FakeDatabase {
  constructor(firstResult = null) {
    this.firstResult = firstResult;
    this.statements = [];
  }

  prepare(sql) {
    const database = this;
    const statement = {
      sql,
      values: [],
      bind(...values) {
        statement.values = values;
        return statement;
      },
      async first() {
        database.statements.push({ sql, values: statement.values });
        return database.firstResult;
      },
      async run() {
        database.statements.push({ sql, values: statement.values });
        return { success: true };
      },
    };
    return statement;
  }
}

function liveRow(overrides = {}) {
  return {
    token_id: 'token-1',
    user_id: 'user-1',
    org_id: 'org-1',
    expires_at: null,
    user_org_id: 'org-1',
    user_role: 'integration_engineer',
    user_status: 'active',
    ...overrides,
  };
}

const ENV = { ELYDORA_DB: null, BETTER_AUTH_SECRET: 'secret', BETTER_AUTH_URL: 'http://localhost', ALLOWED_ORIGINS: '' };

test('issues a 32-byte token and stores only its hash', async () => {
  const db = new FakeDatabase();
  const issued = await issueApiToken(db, 'user-1', 'org-1', 3600);

  assert.equal(Buffer.from(issued.token, 'base64url').length, 32);
  assert.ok(issued.expires_at >= NOW + 3600);
  const [insert] = db.statements;
  assert.match(insert.sql, /INSERT INTO api_tokens/);
  assert.equal(insert.values[3], createHash('sha256').update(issued.token).digest('hex'));
  assert.equal(insert.values[5], issued.expires_at);
  assert.ok(!insert.values.includes(issued.token));
});

test('a null ttl issues a token without expiry', async () => {
  const db = new FakeDatabase();
  const issued = await issueApiToken(db, 'user-1', 'org-1', null);
  assert.equal(issued.expires_at, null);
  assert.equal(db.statements[0].values[5], null);
});

test('authenticates a live token through its hash', async () => {
  const db = new FakeDatabase(liveRow());
  const result = await authenticateApiToken(db, 'raw-token');
  assert.deepEqual(result, { token_id: 'token-1', user_id: 'user-1', org_id: 'org-1', role: 'integration_engineer' });
  assert.equal(db.statements[0].values[0], createHash('sha256').update('raw-token').digest('hex'));
});

test('rejects unknown, expired, suspended, and re-homed tokens', async () => {
  for (const row of [
    null,
    liveRow({ expires_at: NOW - 1 }),
    liveRow({ user_status: 'suspended' }),
    liveRow({ user_org_id: 'org-2' }),
  ]) {
    assert.equal(await authenticateApiToken(new FakeDatabase(row), 'raw-token'), null);
  }
  assert.notEqual(await authenticateApiToken(new FakeDatabase(liveRow({ expires_at: NOW + 60 })), 'raw-token'), null);
});

test('the middleware resolves an API bearer token before Better Auth', async () => {
  const app = new Hono();
  app.get('/whoami', authMiddleware, (c) => c.json({
    org_id: c.get('org_id'),
    role: c.get('role'),
    actor: c.get('actor'),
    auth_token_type: c.get('auth_token_type'),
  }));

  const response = await app.request(
    '/whoami',
    { headers: { Authorization: 'Bearer raw-token' } },
    { ...ENV, ELYDORA_DB: new FakeDatabase(liveRow()) },
  );

  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), {
    org_id: 'org-1',
    role: 'integration_engineer',
    actor: 'user-1',
    auth_token_type: 'api',
  });
});

test('an API token cannot issue another API token', async () => {
  const app = new Hono();
  app.onError((error, c) => c.json({ error: { code: error.errorCode, key: error.messageKey } }, error.statusCode));
  app.route('/v1/auth', auth);

  const response = await app.request(
    '/v1/auth/token',
    {
      method: 'POST',
      headers: { Authorization: 'Bearer raw-token', 'Content-Type': 'application/json' },
      body: JSON.stringify({ ttl_seconds: null }),
    },
    { ...ENV, ELYDORA_DB: new FakeDatabase(liveRow()) },
  );

  assert.equal(response.status, 400);
  assert.deepEqual(await response.json(), { error: { code: 'VALIDATION_ERROR', key: 'auth.issueRequiresSession' } });
});
