import assert from 'node:assert/strict';
import { existsSync, utimesSync, writeFileSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import http from 'node:http';
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

function mismatchServer(respond) {
  const submissions = [];
  const server = http.createServer((request, response) => {
    let raw = '';
    request.on('data', (chunk) => { raw += chunk; });
    request.on('end', () => {
      if (request.url !== '/v1/operations') {
        response.writeHead(200, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify({ agent: { status: 'active' } }));
        return;
      }
      submissions.push(JSON.parse(raw));
      const reply = respond(submissions) ?? mismatch(String(submissions.length).padStart(43, 'A'), 'x');
      const send = () => {
        response.writeHead(reply.status, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify(reply.body));
      };
      if (api.slow) setTimeout(send, 2600); else send();
    });
  });
  const api = { submissions, slow: false };
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve(Object.assign(api, {
    baseUrl: 'http://127.0.0.1:' + server.address().port,
    close: () => new Promise((done) => { server.closeAllConnections?.(); server.close(done); }),
  }))));
}

function mismatch(expected, actual) {
  return {
    status: 400,
    body: { error: { code: 'PREV_HASH_MISMATCH', message: 'Expected prev_chain_hash "' + expected + '", got "' + actual + '".' } },
  };
}

test('Claude audit retries a rejected prev_chain_hash with the server value', async () => {
  const expected = 'Rxlf4j36C3KvIQ3hWuOkX698BR5iDypUFuB70JjEuvM';
  const api = await mismatchServer((submissions) => (submissions.length === 1
    ? mismatch(expected, submissions[0].prev_chain_hash)
    : { status: 202, body: { receipt: { seq_no: 2 } } }));
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { settings } = await readSettings(fixture.settingsPath);
    const audit = managedHandler(settings, 'PostToolUse');
    const result = await runClaudeHook(
      audit,
      JSON.stringify(officialPayload('PostToolUse', { tool_response: { stdout: 'ok' } })),
      fixture,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.equal(api.submissions.length, 2);
    assert.equal(api.submissions[1].prev_chain_hash, expected);
    assert.notEqual(api.submissions[1].operation_id, api.submissions[0].operation_id);
    assert.notEqual(api.submissions[1].nonce, api.submissions[0].nonce);
    const state = JSON.parse(await readFile(path.join(fixture.agentDir, 'chain-state.json'), 'utf-8'));
    assert.equal(state.prev_chain_hash, api.submissions[1].chain_hash);
    assert.match(await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'), /resynced to server: Rxlf/);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Claude audit stops after five rejected prev_chain_hash attempts', async () => {
  const api = await mismatchServer((submissions) => (
    mismatch(String(submissions.length).padStart(43, 'A'), submissions.at(-1).prev_chain_hash)
  ));
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { settings } = await readSettings(fixture.settingsPath);
    const audit = managedHandler(settings, 'PostToolUse');
    const result = await runClaudeHook(
      audit,
      JSON.stringify(officialPayload('PostToolUse', { tool_response: { stdout: 'ok' } })),
      fixture,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.equal(api.submissions.length, 5);
    assert.equal(api.submissions[4].prev_chain_hash, '4'.padStart(43, 'A'));
    assert.match(await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'), /rejected prev_chain_hash 5 times/);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Claude audit stops retrying when the submit budget is spent', async () => {
  const api = await mismatchServer(() => null);
  api.slow = true;
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { settings } = await readSettings(fixture.settingsPath);
    const audit = managedHandler(settings, 'PostToolUse');
    const started = Date.now();
    const result = await runClaudeHook(
      audit,
      JSON.stringify(officialPayload('PostToolUse', { tool_response: { stdout: 'ok' } })),
      fixture,
    );
    const elapsed = Date.now() - started;
    assert.equal(result.code, 0, result.stderr);
    assert.ok(elapsed < 7000, `hook ran ${elapsed}ms`);
    assert.ok(api.submissions.length >= 2 && api.submissions.length <= 3, `${api.submissions.length} submissions`);
    assert.match(await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'), /aborted|retry budget exhausted/i);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Claude audit does not regress a chain head advanced by a concurrent hook', async () => {
  const newerHead = 'B'.repeat(43);
  let statePath;
  const api = await mismatchServer(() => {
    writeFileSync(statePath, JSON.stringify({ prev_chain_hash: newerHead }), { mode: 0o600 });
    return { status: 202, body: { receipt: { seq_no: 1 } } };
  });
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    statePath = path.join(fixture.agentDir, 'chain-state.json');
    const { settings } = await readSettings(fixture.settingsPath);
    const audit = managedHandler(settings, 'PostToolUse');
    const result = await runClaudeHook(
      audit,
      JSON.stringify(officialPayload('PostToolUse', { tool_response: { stdout: 'ok' } })),
      fixture,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.equal(api.submissions[0].prev_chain_hash, 'A'.repeat(43));
    const state = JSON.parse(await readFile(statePath, 'utf-8'));
    assert.equal(state.prev_chain_hash, newerHead);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Claude audit clears a stale chain-state lock and proceeds', async () => {
  const api = await mismatchServer(() => ({ status: 202, body: { receipt: { seq_no: 1 } } }));
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const lockPath = path.join(fixture.agentDir, 'chain-state.json.lock');
    writeFileSync(lockPath, '', { mode: 0o600 });
    const stale = new Date(Date.now() - 10_000);
    utimesSync(lockPath, stale, stale);
    const { settings } = await readSettings(fixture.settingsPath);
    const audit = managedHandler(settings, 'PostToolUse');
    const result = await runClaudeHook(
      audit,
      JSON.stringify(officialPayload('PostToolUse', { tool_response: { stdout: 'ok' } })),
      fixture,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.equal(api.submissions.length, 1);
    assert.equal(existsSync(lockPath), false);
    const state = JSON.parse(await readFile(path.join(fixture.agentDir, 'chain-state.json'), 'utf-8'));
    assert.equal(state.prev_chain_hash, api.submissions[0].chain_hash);
  } finally {
    await fixture.close();
    await api.close();
  }
});

test('Claude audit does not reclaim a chain-state lock whose owner is alive', async () => {
  const api = await mismatchServer(() => ({ status: 202, body: { receipt: { seq_no: 1 } } }));
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const lockPath = path.join(fixture.agentDir, 'chain-state.json.lock');
    writeFileSync(lockPath, String(process.pid), { mode: 0o600 });
    const stale = new Date(Date.now() - 10_000);
    utimesSync(lockPath, stale, stale);
    const { settings } = await readSettings(fixture.settingsPath);
    const audit = managedHandler(settings, 'PostToolUse');
    const result = await runClaudeHook(
      audit,
      JSON.stringify(officialPayload('PostToolUse', { tool_response: { stdout: 'ok' } })),
      fixture,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.equal(api.submissions.length, 1);
    assert.equal(existsSync(lockPath), true);
    assert.match(await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'), /lock timed out/);
  } finally {
    await fixture.close();
    await api.close();
  }
});
