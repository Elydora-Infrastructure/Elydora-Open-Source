import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  managedHandler,
  runHook,
  startApiServer,
  writeJson,
} from '../test-support/cursor-test-helpers.mjs';

test('Cursor guard and both audit events use native JSON contracts', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const guard = managedHandler(config, 'preToolUse', 'guard.js');
    const successAudit = managedHandler(config, 'postToolUse', 'hook.js');
    const failureAudit = managedHandler(config, 'postToolUseFailure', 'hook.js');

    const prePayload = {
      conversation_id: 'conversation-1',
      generation_id: 'generation-1',
      hook_event_name: 'preToolUse',
      tool_name: 'Shell',
      tool_input: { command: 'Get-ChildItem', working_directory: fixture.projectDir },
      tool_use_id: 'call-1',
      cwd: fixture.projectDir,
    };
    const guardResult = await runHook(guard, `${JSON.stringify(prePayload)}\n`, fixture);
    assert.equal(guardResult.code, 0, guardResult.stderr);
    assert.deepEqual(JSON.parse(guardResult.stdout), { permission: 'allow' });

    const postPayload = {
      conversation_id: 'conversation-1',
      generation_id: 'generation-1',
      hook_event_name: 'postToolUse',
      tool_name: 'Shell',
      tool_input: { command: 'Get-ChildItem' },
      tool_output: '{"exitCode":0,"stdout":"ok"}',
      tool_use_id: 'call-1',
      cwd: fixture.projectDir,
      duration: 42,
    };
    const postResult = await runHook(successAudit, `${JSON.stringify(postPayload)}\n`, fixture);
    assert.equal(postResult.code, 0, postResult.stderr);
    assert.deepEqual(JSON.parse(postResult.stdout), {});

    const failurePayload = {
      conversation_id: 'conversation-1',
      generation_id: 'generation-1',
      hook_event_name: 'postToolUseFailure',
      tool_name: 'Shell',
      tool_input: { command: 'exit 7' },
      tool_error: { message: 'Process exited with code 7', exit_code: 7 },
      tool_use_id: 'call-2',
      cwd: fixture.projectDir,
      duration: 12,
    };
    const failureResult = await runHook(
      failureAudit,
      `${JSON.stringify(failurePayload)}\n`,
      fixture,
    );
    assert.equal(failureResult.code, 0, failureResult.stderr);
    assert.deepEqual(JSON.parse(failureResult.stdout), {});

    const operations = api.requests
      .filter((request) => request.method === 'POST')
      .map((request) => JSON.parse(request.raw));
    assert.equal(operations.length, 2);
    assert.deepEqual(operations[0].payload, postPayload);
    assert.deepEqual(operations[1].payload, failurePayload);
    assert.equal(operations[0].subject.session_id, 'conversation-1');
    assert.equal(operations[1].action.tool, 'Shell');
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Cursor guard emits a structured deny for frozen and revoked agents', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const guard = managedHandler(config, 'preToolUse', 'guard.js');
    for (const status of ['frozen', 'revoked']) {
      await writeJson(path.join(fixture.agentDir, 'status-cache.json'), {
        status,
        cached_at: Date.now(),
      });
      const result = await runHook(guard, '{}\n', fixture);
      assert.equal(result.code, 2, result.stderr);
      const response = JSON.parse(result.stdout);
      assert.equal(response.permission, 'deny');
      assert.match(response.agentMessage, new RegExp(status));
    }
  } finally {
    await fixture.close();
  }
});

test('Cursor strict runtimes surface malformed input, config, key, cache, and API errors', async () => {
  const api = await startApiServer({ operationStatus: 500 });
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const hooks = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const guard = managedHandler(hooks, 'preToolUse', 'guard.js');
    const audit = managedHandler(hooks, 'postToolUse', 'hook.js');
    const runtimeConfigPath = path.join(fixture.agentDir, 'config.json');
    const runtimeConfig = await readFile(runtimeConfigPath, 'utf-8');
    const privateKeyPath = path.join(fixture.agentDir, 'private.key');
    const privateKey = await readFile(privateKeyPath, 'utf-8');

    let result = await runHook(audit, '{ malformed', fixture);
    assert.equal(result.code, 1);
    assert.equal(result.stdout, '');
    assert.match(result.stderr, /invalid JSON/i);

    await writeFile(runtimeConfigPath, '{ malformed', { mode: 0o600 });
    result = await runHook(guard, '{}\n', fixture);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /agent config/i);
    await writeFile(runtimeConfigPath, runtimeConfig, { mode: 0o600 });

    await writeFile(privateKeyPath, 'invalid-key', { mode: 0o600 });
    result = await runHook(audit, '{}\n', fixture);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /private key/i);
    await writeFile(privateKeyPath, privateKey, { mode: 0o600 });

    await writeFile(path.join(fixture.agentDir, 'status-cache.json'), '{ malformed', {
      mode: 0o600,
    });
    result = await runHook(guard, '{}\n', fixture);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /status cache/i);

    await writeFile(path.join(fixture.agentDir, 'status-cache.json'), JSON.stringify({
      status: 'active',
      cached_at: 0,
    }), { mode: 0o600 });
    result = await runHook(audit, '{}\n', fixture);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /HTTP 500/i);
    assert.match(await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'), /HTTP 500/i);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Cursor guard fails closed on invalid status API data', async () => {
  const api = await startApiServer({ status: 'paused' });
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const hooks = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const result = await runHook(
      managedHandler(hooks, 'preToolUse', 'guard.js'),
      '{}\n',
      fixture,
    );
    assert.equal(result.code, 1);
    assert.equal(result.stdout, '');
    assert.match(result.stderr, /invalid agent status/i);
  } finally {
    await fixture.close();
    await api.close();
  }
});
