import assert from 'node:assert/strict';
import { readFile, stat } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  managedHandler,
  readSettings,
  runLettaHook,
  startApiServer,
} from '../test-support/letta.mjs';

function officialInput(fixture, event) {
  const base = {
    event_type: event,
    working_directory: fixture.projectDir,
    session_id: 'session-1',
  };
  if (event === 'PreToolUse') {
    return {
      ...base,
      tool_name: 'Bash',
      tool_input: { command: 'echo test' },
      tool_call_id: 'call-1',
      agent_id: 'letta-agent-1',
    };
  }
  if (event === 'PostToolUse') {
    return {
      ...base,
      tool_name: 'Bash',
      tool_input: { command: 'echo test' },
      tool_call_id: 'call-1',
      tool_result: { status: 'success', output: 'test' },
      agent_id: 'letta-agent-1',
      preceding_reasoning: 'Run the command.',
      preceding_assistant_message: 'Executing.',
    };
  }
  return {
    ...base,
    tool_name: 'Bash',
    tool_input: { command: 'exit 1' },
    tool_call_id: 'call-2',
    error_message: 'Command failed',
    error_type: 'ProcessError',
    agent_id: 'letta-agent-1',
    preceding_reasoning: 'Run the command.',
    preceding_assistant_message: 'Executing.',
  };
}

async function installedHandlers(fixture) {
  const settings = await readSettings(fixture.globalPath);
  return {
    guard: managedHandler(settings, 'PreToolUse'),
    audit: managedHandler(settings, 'PostToolUse'),
    failure: managedHandler(settings, 'PostToolUseFailure'),
  };
}

test('Letta guard accepts active agents with native exit semantics', async () => {
  const api = await startApiServer('active');
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { guard } = await installedHandlers(fixture);
    const result = await runLettaHook(
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

test('Letta guard propagates frozen and revoked states through exit code 2', async (t) => {
  for (const status of ['frozen', 'revoked']) {
    await t.test(status, async () => {
      const api = await startApiServer(status);
      const fixture = await createFixture({ baseUrl: api.baseUrl });
      try {
        assert.equal((await fixture.install()).code, 0);
        const { guard } = await installedHandlers(fixture);
        const result = await runLettaHook(
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

test('Letta audit forwards complete success and failure payloads', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { audit, failure } = await installedHandlers(fixture);
    for (const [handler, event] of [
      [audit, 'PostToolUse'],
      [failure, 'PostToolUseFailure'],
    ]) {
      const payload = {
        ...officialInput(fixture, event),
        future_provider_field: { preserved: true },
      };
      const result = await runLettaHook(handler, JSON.stringify(payload), fixture);
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

test('Letta audit failures emit stderr, persist error.log, and exit 1', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const { audit } = await installedHandlers(fixture);
    const result = await runLettaHook(
      audit,
      JSON.stringify(officialInput(fixture, 'PostToolUse')),
      fixture,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /Elydora audit|fetch failed|ECONNREFUSED/i);
    assert.match(
      await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'),
      /Elydora audit|fetch failed|ECONNREFUSED/i,
    );
  } finally {
    await fixture.close();
  }
});

test('Letta runtime artifacts use private modes on POSIX', {
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
      [fixture.globalPath, 0o600],
    ]) {
      assert.equal((await stat(filePath)).mode & 0o777, expected);
    }
  } finally {
    await fixture.close();
  }
});
