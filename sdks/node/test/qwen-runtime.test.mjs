import assert from 'node:assert/strict';
import { readFile, stat } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  managedHandler,
  readSettings,
  runQwenHook,
  startApiServer,
} from '../test-support/qwen.mjs';

const GUARD_NAME = 'elydora-guard';
const AUDIT_NAME = 'elydora-audit';

function officialInput(fixture, event) {
  return {
    session_id: 'session-1',
    transcript_path: path.join(fixture.projectDir, 'transcript.jsonl'),
    cwd: fixture.projectDir,
    hook_event_name: event,
    timestamp: '2026-07-19T00:00:00.000Z',
    permission_mode: 'default',
    tool_name: 'run_shell_command',
    tool_input: { command: 'echo test' },
    tool_use_id: 'toolu_1',
    tool_call_id: 'call_1',
    ...(event === 'PostToolUse' ? {
      tool_response: { output: 'test', error: null },
    } : {}),
    ...(event === 'PostToolUseFailure' ? {
      error: 'command failed',
      is_interrupt: false,
    } : {}),
  };
}

async function installedHandlers(fixture) {
  const settings = (await readSettings(fixture.settingsPath)).settings;
  return {
    guard: managedHandler(settings, 'PreToolUse', GUARD_NAME),
    audit: managedHandler(settings, 'PostToolUse', AUDIT_NAME),
    failure: managedHandler(settings, 'PostToolUseFailure', AUDIT_NAME),
  };
}

test('Qwen guard accepts active agents and preserves native exit semantics', async () => {
  const api = await startApiServer({ status: 'active' });
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { guard } = await installedHandlers(fixture);
    const result = await runQwenHook(
      guard,
      JSON.stringify(officialInput(fixture, 'PreToolUse')),
      fixture,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.equal(result.stdout, '');
    assert.equal(api.requests.filter((request) => request.method === 'GET').length, 1);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Qwen guard propagates frozen and revoked states through exit code 2', async (t) => {
  for (const status of ['frozen', 'revoked']) {
    await t.test(status, async () => {
      const api = await startApiServer({ status });
      const fixture = await createFixture({ baseUrl: api.baseUrl });
      try {
        assert.equal((await fixture.install()).code, 0);
        const { guard } = await installedHandlers(fixture);
        const result = await runQwenHook(
          guard,
          JSON.stringify(officialInput(fixture, 'PreToolUse')),
          fixture,
        );
        assert.equal(result.code, 2, result.stderr);
        assert.equal(result.stdout, '');
        assert.match(result.stderr, new RegExp(`is ${status}`, 'i'));
      } finally {
        await fixture.close();
        await api.close();
      }
    });
  }
});

test('Qwen audit forwards complete success and failure payloads', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { audit, failure } = await installedHandlers(fixture);
    for (const [handler, event] of [
      [audit, 'PostToolUse'],
      [failure, 'PostToolUseFailure'],
    ]) {
      const payload = officialInput(fixture, event);
      payload.future_provider_field = { preserved: true };
      const result = await runQwenHook(handler, JSON.stringify(payload), fixture);
      assert.equal(result.code, 0, result.stderr);
      assert.equal(result.stdout, '');
    }
    const posts = api.requests.filter((request) => request.method === 'POST');
    assert.equal(posts.length, 2);
    assert.deepEqual(JSON.parse(posts[0].raw).payload, {
      ...officialInput(fixture, 'PostToolUse'),
      future_provider_field: { preserved: true },
    });
    assert.deepEqual(JSON.parse(posts[1].raw).payload, {
      ...officialInput(fixture, 'PostToolUseFailure'),
      future_provider_field: { preserved: true },
    });
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Qwen audit keeps delivery failures observable and fail-open', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const { audit } = await installedHandlers(fixture);
    const result = await runQwenHook(
      audit,
      JSON.stringify(officialInput(fixture, 'PostToolUse')),
      fixture,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.equal(result.stdout, '');
    assert.match(
      await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'),
      /Elydora audit|fetch failed|ECONNREFUSED/i,
    );
  } finally {
    await fixture.close();
  }
});

test('Qwen runtime artifacts use private modes on POSIX', {
  skip: process.platform === 'win32' ? 'POSIX mode bits are not authoritative on Windows' : false,
}, async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    for (const [filePath, expected] of [
      [path.join(fixture.agentDir, 'config.json'), 0o600],
      [path.join(fixture.agentDir, 'private.key'), 0o600],
      [fixture.guardScriptPath, 0o700],
      [fixture.hookScriptPath, 0o700],
      [fixture.settingsPath, 0o600],
    ]) {
      assert.equal((await stat(filePath)).mode & 0o777, expected);
    }
  } finally {
    await fixture.close();
  }
});
