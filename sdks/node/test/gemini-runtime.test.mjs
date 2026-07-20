import assert from 'node:assert/strict';
import { readFile, stat } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  managedHandler,
  readSettings,
  runGeminiHook,
  startApiServer,
} from '../test-support/gemini-test-helpers.mjs';

const GUARD_NAME = 'elydora-guard';
const AUDIT_NAME = 'elydora-audit';

function officialInput(fixture, event) {
  return {
    session_id: 'session-1',
    transcript_path: path.join(fixture.projectDir, 'transcript.jsonl'),
    cwd: fixture.projectDir,
    hook_event_name: event,
    timestamp: '2026-07-19T00:00:00.000Z',
    tool_name: 'run_shell_command',
    tool_input: { command: 'echo test' },
    ...(event === 'AfterTool' ? {
      tool_response: { output: 'test', error: null },
    } : {}),
  };
}

async function installedHandlers(fixture) {
  const settings = (await readSettings(fixture.settingsPath)).settings;
  return {
    guard: managedHandler(settings, 'BeforeTool', GUARD_NAME),
    audit: managedHandler(settings, 'AfterTool', AUDIT_NAME),
  };
}

test('Gemini guard accepts active agents with valid JSON stdout', async () => {
  const api = await startApiServer({ status: 'active' });
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const { guard } = await installedHandlers(fixture);
    const input = JSON.stringify(officialInput(fixture, 'BeforeTool'));
    const result = await runGeminiHook(guard, input, fixture);
    assert.equal(result.code, 0, result.stderr);
    assert.deepEqual(JSON.parse(result.stdout), {});
    assert.equal(api.requests.filter((request) => request.method === 'GET').length, 1);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Gemini guard propagates frozen and revoked states through exit code 2', async (t) => {
  for (const status of ['frozen', 'revoked']) {
    await t.test(status, async () => {
      const api = await startApiServer({ status });
      const fixture = await createFixture({ baseUrl: api.baseUrl });
      try {
        assert.equal((await fixture.install()).code, 0);
        const { guard } = await installedHandlers(fixture);
        const result = await runGeminiHook(
          guard,
          JSON.stringify(officialInput(fixture, 'BeforeTool')),
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

test('Gemini audit forwards the complete native AfterTool payload', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { audit } = await installedHandlers(fixture);
    const payload = officialInput(fixture, 'AfterTool');
    payload.mcp_context = { server_name: 'filesystem', tool_name: 'write_file' };
    payload.original_request_name = 'write_file';
    payload.future_provider_field = { preserved: true };
    const source = JSON.stringify(payload);
    const result = await runGeminiHook(audit, source, fixture);
    assert.equal(result.code, 0, result.stderr);
    assert.deepEqual(JSON.parse(result.stdout), {});
    const request = api.requests.find((entry) => entry.method === 'POST');
    assert(request);
    assert.deepEqual(JSON.parse(request.raw).payload, payload);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Gemini runtimes keep failures observable while preserving fail-open behavior', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const { guard, audit } = await installedHandlers(fixture);
    const invalidGuard = await runGeminiHook(guard, '{ malformed', fixture);
    assert.equal(invalidGuard.code, 0, invalidGuard.stderr);
    assert.deepEqual(JSON.parse(invalidGuard.stdout), {});
    assert.match(invalidGuard.stderr, /invalid JSON/i);

    const auditResult = await runGeminiHook(
      audit,
      JSON.stringify(officialInput(fixture, 'AfterTool')),
      fixture,
    );
    assert.equal(auditResult.code, 0, auditResult.stderr);
    assert.deepEqual(JSON.parse(auditResult.stdout), {});
    assert.match(
      await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'),
      /Elydora audit|fetch failed|ECONNREFUSED/i,
    );
  } finally {
    await fixture.close();
  }
});

test('Gemini runtime artifacts use private modes on POSIX', {
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
