import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  managedHandler,
  readSettings,
  runClaudeHook,
  startApiServer,
} from '../test-support/claudecode-test-helpers.mjs';

function officialPayload(event, overrides = {}) {
  return {
    session_id: 'session-1',
    prompt_id: '302d811d-0d17-41ad-a359-d2cb618fd42b',
    transcript_path: '/tmp/session-1.jsonl',
    cwd: '/tmp/project',
    permission_mode: 'default',
    effort: { level: 'high' },
    hook_event_name: event,
    tool_name: 'Bash',
    tool_input: { command: 'npm test', description: 'Run tests' },
    tool_use_id: 'toolu_01ABC123',
    ...overrides,
  };
}

test('Claude runtimes enforce active state and preserve official event payloads', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { settings } = await readSettings(fixture.settingsPath);
    const guard = managedHandler(settings, 'PreToolUse');
    const successAudit = managedHandler(settings, 'PostToolUse');
    const failureAudit = managedHandler(settings, 'PostToolUseFailure');

    const pre = officialPayload('PreToolUse');
    const guardResult = await runClaudeHook(guard, JSON.stringify(pre), fixture);
    assert.equal(guardResult.code, 0, guardResult.stderr);
    assert.equal(guardResult.stdout, '');

    const success = officialPayload('PostToolUse', {
      tool_response: {
        stdout: 'tests passed',
        stderr: '',
        interrupted: false,
        isImage: false,
      },
    });
    const successResult = await runClaudeHook(successAudit, JSON.stringify(success), fixture);
    assert.equal(successResult.code, 0, successResult.stderr);

    const failure = officialPayload('PostToolUseFailure', {
      error: 'Command exited with non-zero status code 1',
      is_interrupt: false,
      duration_ms: 4187,
    });
    const failureResult = await runClaudeHook(failureAudit, JSON.stringify(failure), fixture);
    assert.equal(failureResult.code, 0, failureResult.stderr);

    assert.equal(api.requests.length, 3);
    assert.deepEqual(
      api.requests.map((request) => [request.method, request.url]),
      [
        ['GET', '/v1/agents/agent-1'],
        ['POST', '/v1/operations'],
        ['POST', '/v1/operations'],
      ],
    );
    const successOperation = JSON.parse(api.requests[1].raw);
    const failureOperation = JSON.parse(api.requests[2].raw);
    assert.deepEqual(successOperation.payload, success);
    assert.deepEqual(failureOperation.payload, failure);
    assert.deepEqual(successOperation.subject, { session_id: 'session-1' });
    assert.deepEqual(successOperation.action, { tool: 'Bash' });
    assert.equal(api.requests[1].headers.authorization, 'Bearer token-1');
    assert.equal(failureOperation.prev_chain_hash, successOperation.chain_hash);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Claude guard returns official blocking exit code 2 for frozen and revoked agents', async (t) => {
  for (const status of ['frozen', 'revoked']) {
    await t.test(status, async () => {
      const api = await startApiServer({ status });
      const fixture = await createFixture({ baseUrl: api.baseUrl });
      try {
        assert.equal((await fixture.install()).code, 0);
        const { settings } = await readSettings(fixture.settingsPath);
        const guard = managedHandler(settings, 'PreToolUse');
        const result = await runClaudeHook(
          guard,
          JSON.stringify(officialPayload('PreToolUse')),
          fixture,
        );
        assert.equal(result.code, 2);
        assert.equal(result.stdout, '');
        assert.match(result.stderr, new RegExp(`Agent "claudecode" is ${status}`, 'i'));
        assert.match(result.stderr, /Tool execution blocked/i);
      } finally {
        await fixture.close();
        await api.close();
      }
    });
  }
});

test('Claude runtime failures stay observable while guard and audit delivery remain fail-open', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const { settings } = await readSettings(fixture.settingsPath);
    const guard = managedHandler(settings, 'PreToolUse');
    const audit = managedHandler(settings, 'PostToolUse');

    const guardResult = await runClaudeHook(
      guard,
      JSON.stringify(officialPayload('PreToolUse')),
      fixture,
    );
    assert.equal(guardResult.code, 0);
    assert.match(guardResult.stderr, /Failed to resolve agent status/i);

    const auditResult = await runClaudeHook(
      audit,
      JSON.stringify(officialPayload('PostToolUse', { tool_response: { stdout: '' } })),
      fixture,
    );
    assert.equal(auditResult.code, 0);
    const errorLog = await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8');
    assert.match(errorLog, /\[claudecode\]/i);
    assert.match(errorLog, /fetch failed|ECONNREFUSED/i);

    const malformed = await runClaudeHook(audit, '{ malformed', fixture);
    assert.equal(malformed.code, 0);
    const updatedLog = await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8');
    assert.match(updatedLog, /Hook input is invalid JSON/i);
  } finally {
    await fixture.close();
  }
});
