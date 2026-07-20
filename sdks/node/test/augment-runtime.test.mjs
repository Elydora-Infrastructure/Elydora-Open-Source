import assert from 'node:assert/strict';
import { readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  managedHandler,
  readSettings,
  runHandler,
  runPlugin,
  startApiServer,
} from '../test-support/augment-test-helpers.mjs';

async function status(fixture) {
  const result = await runPlugin(fixture, 'status', null);
  return { result, value: result.code === 0 ? JSON.parse(result.stdout) : undefined };
}

test('Auggie guard propagates exit code 2 for a frozen agent', async () => {
  const api = await startApiServer({ status: 'frozen' });
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const settings = (await readSettings(fixture.settingsPath)).settings;
    const guard = managedHandler(settings, 'PreToolUse', fixture.guardWrapperPath);
    const payload = JSON.stringify({
      hook_event_name: 'PreToolUse',
      conversation_id: 'conversation-1',
      workspace_roots: [fixture.projectDir],
      tool_name: 'launch-process',
      tool_input: { command: 'echo test' },
      is_mcp_tool: false,
    });
    const result = await runHandler(guard, payload, fixture);
    assert.equal(result.code, 2, result.stderr);
    assert.match(result.stderr, /Agent "augment" is frozen/);
    assert.equal(api.requests.filter((request) => request.method === 'GET').length, 1);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Auggie audit preserves the complete native hook payload', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const settings = (await readSettings(fixture.settingsPath)).settings;
    const audit = managedHandler(settings, 'PostToolUse', fixture.auditWrapperPath);
    const payload = {
      hook_event_name: 'PostToolUse',
      conversation_id: 'conversation-1',
      workspace_roots: [fixture.projectDir],
      tool_name: 'launch-process',
      tool_input: { command: 'echo test', nested: { preserve: true } },
      tool_output: { stdout: 'test', stderr: '', exit_code: 0 },
      is_mcp_tool: false,
      conversation_data: [{ role: 'user', content: 'preserve this' }],
      mcp_metadata: { server: 'local', transport: 'stdio' },
      user_context: { account: 'user-1' },
      future_field: { survives: ['exactly', 2] },
    };
    const result = await runHandler(audit, JSON.stringify(payload), fixture);
    assert.equal(result.code, 0, result.stderr);
    const request = api.requests.find((candidate) => candidate.method === 'POST');
    assert(request);
    assert.deepEqual(JSON.parse(request.raw).payload, payload);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Auggie status requires settings, exact runtime identity, key, scripts, and wrappers', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    assert.equal((await status(fixture)).value.installed, true);

    await rm(fixture.guardWrapperPath);
    assert.equal((await status(fixture)).value.installed, false);
    assert.equal((await fixture.install()).code, 0);

    await rm(fixture.hookScriptPath);
    assert.equal((await status(fixture)).value.installed, false);
    assert.equal((await fixture.install()).code, 0);

    await writeFile(fixture.auditWrapperPath, 'tampered wrapper\n');
    assert.equal((await status(fixture)).value.installed, false);
    assert.equal((await fixture.install()).code, 0);

    const keyPath = path.join(fixture.agentDir, 'private.key');
    await writeFile(keyPath, 'invalid');
    const invalidKey = await status(fixture);
    assert.equal(invalidKey.result.code, 1);
    assert.match(invalidKey.result.stderr, /private key is invalid/i);
  } finally {
    await fixture.close();
  }
});

test('Auggie status surfaces malformed and mismatched runtime metadata', async (t) => {
  for (const [name, contents, pattern] of [
    ['malformed', '{ malformed', /parse Elydora runtime config/i],
    ['duplicate', '{"agent_name":"augment","agent_name":"augment"}', /duplicate field/i],
    ['mismatched', JSON.stringify({
      org_id: 'org-1',
      agent_id: 'other-agent',
      kid: 'kid-1',
      base_url: 'https://api.elydora.com',
      agent_name: 'augment',
    }), /identity does not match/i],
    ['unsupported field', JSON.stringify({
      org_id: 'org-1',
      agent_id: 'agent-1',
      kid: 'kid-1',
      base_url: 'https://api.elydora.com',
      agent_name: 'augment',
      hidden: true,
    }), /unsupported field/i],
  ]) {
    await t.test(name, async () => {
      const fixture = await createFixture();
      try {
        assert.equal((await fixture.install()).code, 0);
        await writeFile(path.join(fixture.agentDir, 'config.json'), contents);
        const current = await status(fixture);
        assert.equal(current.result.code, 1);
        assert.match(current.result.stderr, pattern);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Auggie status ignores incomplete managed hook pairs', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const { settings } = await readSettings(fixture.settingsPath);
    delete settings.hooks.PostToolUse;
    await writeFile(fixture.settingsPath, `${JSON.stringify(settings, null, 2)}\n`);
    const current = await status(fixture);
    assert.equal(current.result.code, 0, current.result.stderr);
    assert.equal(current.value.hookConfigured, false);
    assert.equal(current.value.installed, false);
  } finally {
    await fixture.close();
  }
});
