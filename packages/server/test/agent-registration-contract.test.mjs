import assert from 'node:assert/strict';
import test from 'node:test';

import { INTEGRATION_TYPES } from '../dist/shared/index.js';
import {
  registerAgent,
  updateAgentIntegrationType,
} from '../dist/services/agent-service.js';

const EXPECTED_INTEGRATION_TYPES = [
  'augment', 'claudecode', 'cline', 'codex', 'copilot', 'cursor', 'droid',
  'gemini', 'grok', 'kimi', 'kirocli', 'kiroide', 'letta', 'opencode', 'qwen',
  'enterprise', 'gui', 'sdk', 'other',
];
const PUBLIC_KEY = Buffer.alloc(32, 7).toString('base64url');

class FakeStatement {
  constructor(database, sql, values = []) {
    this.database = database;
    this.sql = sql;
    this.values = values;
  }

  bind(...values) {
    return new FakeStatement(this.database, this.sql, values);
  }

  async first() {
    return this.database.firstResult;
  }

  async all() {
    return { results: [] };
  }

  async run() {
    return { success: true };
  }
}

class FakeDatabase {
  constructor(firstResult = null) {
    this.firstResult = firstResult;
    this.prepareCount = 0;
    this.batches = [];
  }

  prepare(sql) {
    this.prepareCount += 1;
    return new FakeStatement(this, sql);
  }

  async batch(statements) {
    this.batches.push(statements);
    return statements.map(() => ({ results: [], success: true }));
  }
}

function registrationRequest(integrationType) {
  return {
    agent_id: `agent-${integrationType}`,
    integration_type: integrationType,
    display_name: 'Audit Agent',
    responsible_entity: 'platform@example.com',
    keys: [{ kid: 'key-v1', public_key: PUBLIC_KEY, algorithm: 'ed25519' }],
  };
}

async function expectIntegrationError(operation, messageKey) {
  await assert.rejects(operation, (error) => {
    assert.equal(error.name, 'AppError');
    assert.equal(error.statusCode, 400);
    assert.equal(error.errorCode, 'VALIDATION_ERROR');
    assert.equal(error.messageKey, messageKey);
    return true;
  });
}

test('integration types match the public registration contract', () => {
  assert.deepEqual(INTEGRATION_TYPES, EXPECTED_INTEGRATION_TYPES);
});

test('registration rejects missing and unknown integration types before database access', async () => {
  for (const [value, messageKey] of [
    [undefined, 'agent.missingIntegrationType'],
    ['future-cli', 'agent.invalidIntegrationType'],
  ]) {
    const database = new FakeDatabase();
    const request = registrationRequest(value);
    await expectIntegrationError(
      registerAgent(database, request, 'org-1', 'user-1'),
      messageKey,
    );
    assert.equal(database.prepareCount, 0);
    assert.equal(database.batches.length, 0);
  }

  for (const [value, messageKey] of [
    [undefined, 'agent.missingIntegrationType'],
    ['future-cli', 'agent.invalidIntegrationType'],
  ]) {
    const database = new FakeDatabase();
    await expectIntegrationError(
      updateAgentIntegrationType(database, 'agent-1', value, 'org-1', 'user-1'),
      messageKey,
    );
    assert.equal(database.prepareCount, 0);
  }
});

test('integration updates stay scoped to the authenticated organization', async () => {
  const existingAgent = {
    agent_id: 'agent-1',
    org_id: 'org-1',
    display_name: 'Audit Agent',
    responsible_entity: 'platform@example.com',
    integration_type: 'sdk',
    status: 'active',
    created_at: 1,
    updated_at: 1,
  };

  for (const integrationType of INTEGRATION_TYPES) {
    const database = new FakeDatabase(existingAgent);
    const response = await updateAgentIntegrationType(
      database,
      existingAgent.agent_id,
      integrationType,
      existingAgent.org_id,
      'user-1',
    );

    assert.equal(response.agent.integration_type, integrationType);
    const update = database.batches[0].find((statement) => (
      statement.sql.includes('UPDATE agents SET integration_type')
    ));
    assert.ok(update);
    assert.match(update.sql, /WHERE agent_id = \? AND org_id = \?/);
    assert.equal(update.values[0], integrationType);
    assert.equal(update.values[2], existingAgent.agent_id);
    assert.equal(update.values[3], existingAgent.org_id);
  }
});

test('registration persists every supported integration type without substitution', async () => {
  for (const integrationType of INTEGRATION_TYPES) {
    const database = new FakeDatabase();
    const response = await registerAgent(
      database,
      registrationRequest(integrationType),
      'org-1',
      'user-1',
    );

    assert.equal(response.agent.integration_type, integrationType);
    assert.equal(database.batches.length, 1);
    const agentInsert = database.batches[0].find((statement) => (
      statement.sql.includes('INSERT INTO agents')
    ));
    assert.ok(agentInsert);
    assert.equal(agentInsert.values[4], integrationType);
  }
});
