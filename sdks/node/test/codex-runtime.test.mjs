import assert from 'node:assert/strict';
import { readFile, rm, symlink, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  managedHandler,
  runHook,
  startApiServer,
} from '../test-support/codex-test-helpers.mjs';

const GUARD_STATUS = 'Checking Elydora agent state';
const AUDIT_STATUS = 'Recording Elydora tool use';

function officialPayload(event, overrides = {}) {
  return {
    session_id: 'session-1',
    transcript_path: null,
    cwd: 'C:\\workspace',
    hook_event_name: event,
    model: 'gpt-5.4',
    turn_id: 'turn-1',
    permission_mode: 'default',
    tool_name: 'Bash',
    tool_use_id: 'call-1',
    tool_input: { command: 'echo test' },
    ...overrides,
  };
}

test('Codex runtimes enforce active state and preserve official event JSON', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const guard = managedHandler(config, 'PreToolUse', GUARD_STATUS).handler;
    const audit = managedHandler(config, 'PostToolUse', AUDIT_STATUS).handler;

    const pre = officialPayload('PreToolUse');
    const guardResult = await runHook(guard, JSON.stringify(pre), fixture);
    assert.equal(guardResult.code, 0, guardResult.stderr);
    assert.equal(guardResult.stdout, '');
    assert.equal(api.requests[0].method, 'GET');
    assert.equal(api.requests[0].url, '/v1/agents/agent-1');
    assert.equal(api.requests[0].headers.authorization, 'Bearer token-1');

    const success = officialPayload('PostToolUse', {
      tool_response: { output: 'test', exit_code: 0 },
    });
    const successResult = await runHook(audit, JSON.stringify(success), fixture);
    assert.equal(successResult.code, 0, successResult.stderr);
    assert.equal(successResult.stdout, '');

    const failure = officialPayload('PostToolUse', {
      tool_use_id: 'call-2',
      tool_input: { command: 'exit 7' },
      tool_response: { output: '', stderr: 'failed', exit_code: 7 },
    });
    const failureResult = await runHook(audit, JSON.stringify(failure), fixture);
    assert.equal(failureResult.code, 0, failureResult.stderr);
    assert.equal(failureResult.stdout, '');

    const operations = api.requests
      .filter((request) => request.method === 'POST')
      .map((request) => JSON.parse(request.raw));
    assert.equal(operations.length, 2);
    assert.deepEqual(operations[0].payload, success);
    assert.deepEqual(operations[1].payload, failure);
    assert.equal(operations[0].subject.session_id, 'session-1');
    assert.equal(operations[0].action.tool, 'Bash');
    assert.equal(api.requests.at(-1).headers.authorization, 'Bearer token-1');
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Codex guard propagates exit code 2 for frozen and revoked agents', async () => {
  for (const status of ['frozen', 'revoked']) {
    const api = await startApiServer({ status });
    const fixture = await createFixture({ baseUrl: api.baseUrl });
    try {
      assert.equal((await fixture.install()).code, 0);
      const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
      const guard = managedHandler(config, 'PreToolUse', GUARD_STATUS).handler;
      const result = await runHook(
        guard,
        JSON.stringify(officialPayload('PreToolUse')),
        fixture,
      );
      assert.equal(result.code, 2);
      assert.equal(result.stdout, '');
      assert.match(result.stderr, new RegExp(`agent \\"codex\\" is ${status}`, 'i'));
    } finally {
      await fixture.close();
      await api.close();
    }
  }
});

test('Codex fail-open guard reports malformed input, config, and status data', async () => {
  const activeApi = await startApiServer();
  const activeFixture = await createFixture({ baseUrl: activeApi.baseUrl });
  try {
    assert.equal((await activeFixture.install()).code, 0);
    const config = JSON.parse(await readFile(activeFixture.configPath, 'utf-8'));
    const guard = managedHandler(config, 'PreToolUse', GUARD_STATUS).handler;
    const malformed = await runHook(guard, '{ malformed', activeFixture);
    assert.equal(malformed.code, 0);
    assert.match(malformed.stderr, /invalid JSON.*fail-open/i);

    await rm(path.join(activeFixture.agentDir, 'config.json'));
    const missingConfig = await runHook(
      guard,
      JSON.stringify(officialPayload('PreToolUse')),
      activeFixture,
    );
    assert.equal(missingConfig.code, 0);
    assert.match(missingConfig.stderr, /failed to read agent config/i);
  } finally {
    await activeFixture.close();
    await activeApi.close();
  }

  const invalidApi = await startApiServer({ status: 'unexpected' });
  const invalidFixture = await createFixture({ baseUrl: invalidApi.baseUrl });
  try {
    assert.equal((await invalidFixture.install()).code, 0);
    const config = JSON.parse(await readFile(invalidFixture.configPath, 'utf-8'));
    const guard = managedHandler(config, 'PreToolUse', GUARD_STATUS).handler;
    const result = await runHook(
      guard,
      JSON.stringify(officialPayload('PreToolUse')),
      invalidFixture,
    );
    assert.equal(result.code, 0);
    assert.match(result.stderr, /invalid agent status/i);
  } finally {
    await invalidFixture.close();
    await invalidApi.close();
  }
});

test('Codex fail-open audit records malformed input and API failures', async () => {
  const api = await startApiServer({ operationStatus: 503 });
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const audit = managedHandler(config, 'PostToolUse', AUDIT_STATUS).handler;

    const malformed = await runHook(audit, '{ malformed', fixture);
    assert.equal(malformed.code, 0);
    let log = await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8');
    assert.match(log, /Hook input is invalid JSON/i);

    const failedApi = await runHook(
      audit,
      JSON.stringify(officialPayload('PostToolUse', { tool_response: { exit_code: 0 } })),
      fixture,
    );
    assert.equal(failedApi.code, 0);
    log = await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8');
    assert.match(log, /Audit API returned HTTP 503/i);
    await assert.rejects(readFile(path.join(fixture.agentDir, 'chain-state.json')), {
      code: 'ENOENT',
    });
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Codex runtimes reject linked cache, chain, and error state', async (t) => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const guard = managedHandler(config, 'PreToolUse', GUARD_STATUS).handler;
    const audit = managedHandler(config, 'PostToolUse', AUDIT_STATUS).handler;

    const cacheTarget = path.join(fixture.rootDir, 'cache-target');
    const cachePath = path.join(fixture.agentDir, 'status-cache.json');
    await writeFile(cacheTarget, 'cache sentinel', { mode: 0o600 });
    try {
      await symlink(cacheTarget, cachePath);
    } catch (error) {
      if (error?.code === 'EPERM') {
        t.skip(`symbolic links unavailable: ${error.message}`);
        return;
      }
      throw error;
    }
    const guardResult = await runHook(
      guard,
      JSON.stringify(officialPayload('PreToolUse')),
      fixture,
    );
    assert.equal(guardResult.code, 0);
    assert.match(guardResult.stderr, /Status cache path is not a physical file/i);
    assert.equal(await readFile(cacheTarget, 'utf-8'), 'cache sentinel');
    await rm(cachePath);

    const chainTarget = path.join(fixture.rootDir, 'chain-target');
    const chainPath = path.join(fixture.agentDir, 'chain-state.json');
    await writeFile(chainTarget, 'chain sentinel', { mode: 0o600 });
    await symlink(chainTarget, chainPath);
    const auditResult = await runHook(
      audit,
      JSON.stringify(officialPayload('PostToolUse', { tool_response: { exit_code: 0 } })),
      fixture,
    );
    assert.equal(auditResult.code, 0);
    assert.equal(await readFile(chainTarget, 'utf-8'), 'chain sentinel');
    assert.match(
      await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'),
      /Chain state path is not a physical file/i,
    );
    await rm(chainPath);

    const errorTarget = path.join(fixture.rootDir, 'error-target');
    const errorPath = path.join(fixture.agentDir, 'error.log');
    await writeFile(errorTarget, 'error sentinel', { mode: 0o600 });
    await rm(errorPath);
    await symlink(errorTarget, errorPath);
    const errorResult = await runHook(audit, '{ malformed', fixture);
    assert.equal(errorResult.code, 0);
    assert.match(errorResult.stderr, /Error log path is not a physical file/i);
    assert.equal(await readFile(errorTarget, 'utf-8'), 'error sentinel');
  } finally {
    await fixture.close();
    await api.close();
  }
});
