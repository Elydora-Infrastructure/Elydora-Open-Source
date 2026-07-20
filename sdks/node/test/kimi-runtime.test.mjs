import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  managedHook,
  readKimiConfig,
  runKimiHook,
  startApiServer,
} from '../test-support/kimi-test-helpers.mjs';

const prePayload = {
  hook_event_name: 'PreToolUse',
  session_id: 'session-1',
  cwd: 'C:/project',
  tool_name: 'Bash',
  tool_input: { command: 'echo test' },
  tool_call_id: 'call-1',
};

test('Kimi runtimes enforce active state and preserve success and failure payloads', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({
    baseUrl: api.baseUrl,
    legacyDetected: false,
  });
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const { config } = await readKimiConfig(fixture.stablePath);
    const guard = managedHook(config, 'PreToolUse');
    const successAudit = managedHook(config, 'PostToolUse');
    const failureAudit = managedHook(config, 'PostToolUseFailure');

    const guardResult = await runKimiHook(
      guard.command,
      JSON.stringify(prePayload),
      fixture,
      { ELYDORA_HOOK_PATH: 'injected-command-fragment' },
    );
    assert.equal(guardResult.code, 0, guardResult.stderr);

    const successPayload = {
      ...prePayload,
      hook_event_name: 'PostToolUse',
      tool_output: 'test\n',
    };
    const success = await runKimiHook(
      successAudit.command,
      JSON.stringify(successPayload),
      fixture,
    );
    assert.equal(success.code, 0, success.stderr);

    const failurePayload = {
      ...prePayload,
      hook_event_name: 'PostToolUseFailure',
      error: { name: 'ToolError', message: 'command failed', code: 'tool.failed' },
    };
    const failure = await runKimiHook(
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
    assert.equal(operations[1].action.tool, 'Bash');
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Kimi guard propagates exit code 2 for frozen and revoked agents', async (t) => {
  for (const status of ['frozen', 'revoked']) {
    await t.test(status, async () => {
      const api = await startApiServer({ status });
      const fixture = await createFixture({
        baseUrl: api.baseUrl,
        legacyDetected: false,
      });
      try {
        assert.equal((await fixture.install()).code, 0);
        const { config } = await readKimiConfig(fixture.stablePath);
        const result = await runKimiHook(
          managedHook(config, 'PreToolUse').command,
          JSON.stringify(prePayload),
          fixture,
        );
        assert.equal(result.code, 2);
        assert.match(result.stderr, new RegExp(status, 'i'));
      } finally {
        await fixture.close();
        await api.close();
      }
    });
  }
});

test('Kimi audit runtime records malformed input and API failures', async () => {
  const api = await startApiServer({ operationStatus: 503 });
  const fixture = await createFixture({
    baseUrl: api.baseUrl,
    legacyDetected: false,
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { config } = await readKimiConfig(fixture.stablePath);
    const command = managedHook(config, 'PostToolUse').command;
    const malformed = await runKimiHook(command, '{ malformed', fixture);
    assert.equal(malformed.code, 0);
    assert.equal(malformed.stderr, '');

    const upstream = await runKimiHook(command, JSON.stringify({
      ...prePayload,
      hook_event_name: 'PostToolUse',
      tool_output: 'test',
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
