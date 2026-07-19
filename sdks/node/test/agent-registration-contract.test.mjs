import assert from 'node:assert/strict';
import http from 'node:http';
import test from 'node:test';

import { ElydoraClient, INTEGRATION_TYPES } from '../dist/index.js';

const EXPECTED_INTEGRATION_TYPES = [
  'augment',
  'claudecode',
  'cline',
  'codex',
  'copilot',
  'cursor',
  'droid',
  'gemini',
  'grok',
  'kimi',
  'kirocli',
  'kiroide',
  'letta',
  'opencode',
  'qwen',
  'enterprise',
  'gui',
  'sdk',
  'other',
];

async function createFixture() {
  const requests = [];
  const server = http.createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    requests.push({
      method: request.method,
      path: request.url,
      authorization: request.headers.authorization,
      body,
    });
    response.writeHead(201, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({
      agent: {
        agent_id: body.agent_id,
        org_id: 'org-1',
        display_name: body.display_name,
        responsible_entity: body.responsible_entity,
        integration_type: body.integration_type,
        status: 'active',
        created_at: 1,
        updated_at: 1,
      },
      keys: [],
    }));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert(address && typeof address === 'object');

  const client = new ElydoraClient({
    orgId: 'org-1',
    agentId: 'admin-agent',
    privateKey: 'unused',
    baseUrl: `http://127.0.0.1:${address.port}`,
    maxRetries: 0,
  });
  client.setToken('api-token');

  return {
    client,
    requests,
    async close() {
      await new Promise((resolve, reject) => {
        server.close((error) => (error ? reject(error) : resolve()));
      });
    },
  };
}

test('integration types match the public API contract', () => {
  assert.deepEqual(INTEGRATION_TYPES, EXPECTED_INTEGRATION_TYPES);
});

test('agent registration sends integration_type in one authenticated request', async () => {
  const fixture = await createFixture();
  try {
    await assert.rejects(
      fixture.client.registerAgent({ agent_id: 'agent-1', keys: [] }),
      {
        name: 'TypeError',
        message: /Invalid integration_type "undefined"/,
      },
    );
    await assert.rejects(
      fixture.client.registerAgent({
        agent_id: 'agent-1',
        integration_type: 'future-cli',
        keys: [],
      }),
      {
        name: 'TypeError',
        message: /Invalid integration_type "future-cli"/,
      },
    );
    assert.equal(fixture.requests.length, 0);

    const response = await fixture.client.registerAgent({
      agent_id: 'agent-1',
      integration_type: 'grok',
      display_name: 'Grok Agent',
      responsible_entity: 'platform@example.com',
      keys: [{ kid: 'key-v1', public_key: 'public-key', algorithm: 'ed25519' }],
    });

    assert.equal(response.agent.integration_type, 'grok');
    assert.deepEqual(fixture.requests, [{
      method: 'POST',
      path: '/v1/agents/register',
      authorization: 'Bearer api-token',
      body: {
        agent_id: 'agent-1',
        integration_type: 'grok',
        display_name: 'Grok Agent',
        responsible_entity: 'platform@example.com',
        keys: [{ kid: 'key-v1', public_key: 'public-key', algorithm: 'ed25519' }],
      },
    }]);
  } finally {
    await fixture.close();
  }
});
