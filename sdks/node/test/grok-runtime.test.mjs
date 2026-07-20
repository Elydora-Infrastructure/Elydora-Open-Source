import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  managedHandler,
  readGrokConfig,
  runGrokHook,
  startApiServer,
} from '../test-support/grok-test-helpers.mjs';

const prePayload = {
  hookEventName: 'pre_tool_use',
  sessionId: 'session-1',
  cwd: 'C:/project',
  workspaceRoot: 'C:/project',
  toolName: 'run_terminal_command',
  toolInput: { command: 'npm test' },
  toolUseId: 'tool-use-1',
  toolInputTruncated: false,
  timestamp: '2026-07-19T12:00:00Z',
};

test('Grok runtimes enforce active state and preserve success and failure payloads', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const { config } = await readGrokConfig(fixture.configPath);
    const guard = managedHandler(config, 'PreToolUse');
    const successAudit = managedHandler(config, 'PostToolUse');
    const failureAudit = managedHandler(config, 'PostToolUseFailure');

    const guardResult = await runGrokHook(
      guard.command,
      JSON.stringify(prePayload),
      fixture,
      { GROK_HOOK_EVENT: 'injected-command-fragment' },
    );
    assert.equal(guardResult.code, 0, guardResult.stderr);
    assert.equal(guardResult.stdout, '');

    const successPayload = {
      ...prePayload,
      hookEventName: 'post_tool_use',
      toolResult: { output: 'tests passed' },
      toolResultTruncated: false,
      durationMs: 125,
    };
    const success = await runGrokHook(
      successAudit.command,
      JSON.stringify(successPayload),
      fixture,
    );
    assert.equal(success.code, 0, success.stderr);

    const failurePayload = {
      ...prePayload,
      hookEventName: 'post_tool_use_failure',
      toolResult: { error: 'command failed', exitCode: 1 },
      toolResultTruncated: false,
      durationMs: 40,
    };
    const failure = await runGrokHook(
      failureAudit.command,
      JSON.stringify(failurePayload),
      fixture,
    );
    assert.equal(failure.code, 0, failure.stderr);

    const operations = api.requests
      .filter((request) => request.method === 'POST')
      .map((request) => JSON.parse(request.raw));
    assert.equal(operations.length, 2);
    assert.deepEqual(operations[0].payload, successPayload);
    assert.deepEqual(operations[1].payload, failurePayload);
    assert.equal(operations[0].subject.session_id, 'session-1');
    assert.equal(operations[1].action.tool, 'run_terminal_command');
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Grok guard emits official deny JSON and exit code 2 for frozen and revoked agents', async (t) => {
  for (const status of ['frozen', 'revoked']) {
    await t.test(status, async () => {
      const api = await startApiServer({ status });
      const fixture = await createFixture({ baseUrl: api.baseUrl });
      try {
        assert.equal((await fixture.install()).code, 0);
        const { config } = await readGrokConfig(fixture.configPath);
        const result = await runGrokHook(
          managedHandler(config, 'PreToolUse').command,
          JSON.stringify(prePayload),
          fixture,
        );
        assert.equal(result.code, 2);
        assert.match(result.stderr, new RegExp(status, 'i'));
        const decision = JSON.parse(result.stdout);
        assert.deepEqual(Object.keys(decision).sort(), ['decision', 'reason']);
        assert.equal(decision.decision, 'deny');
        assert.match(decision.reason, new RegExp(status, 'i'));
      } finally {
        await fixture.close();
        await api.close();
      }
    });
  }
});

test('Grok audit runtime records malformed input and API failures', async () => {
  const api = await startApiServer({ operationStatus: 503 });
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { config } = await readGrokConfig(fixture.configPath);
    const command = managedHandler(config, 'PostToolUse').command;
    const malformed = await runGrokHook(command, '{ malformed', fixture);
    assert.equal(malformed.code, 0);
    assert.equal(malformed.stderr, '');

    const upstream = await runGrokHook(command, JSON.stringify({
      ...prePayload,
      hookEventName: 'post_tool_use',
      toolResult: { output: 'test' },
    }), fixture);
    assert.equal(upstream.code, 0);
    assert.equal(upstream.stderr, '');
    const log = await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8');
    assert.match(log, /invalid JSON/i);
    assert.match(log, /HTTP 503/i);
  } finally {
    await fixture.close();
    await api.close();
  }
});
