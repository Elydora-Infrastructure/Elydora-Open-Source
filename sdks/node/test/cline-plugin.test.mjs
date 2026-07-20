import assert from 'node:assert/strict';
import {
  lstat,
  mkdir,
  readFile,
  readdir,
  stat,
  writeFile,
} from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  VALID_PRIVATE_KEY,
  cliPath,
  createFixture,
  environment,
  installConfig,
  readJson,
  registryModuleUrl,
  runNode,
  runPlugin,
  startApiServer,
} from '../test-support/cline-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

async function assertNoTransactionFiles(fixture) {
  const names = [
    ...await readdir(fixture.agentDir),
    ...await readdir(fixture.hooksDir),
  ];
  assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false, names.join(', '));
}

test('Cline is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('cline'), {
    name: 'Cline',
    configDir: '~/.cline/hooks',
    configFile: 'PreToolUse.mjs',
  });
  const fixture = await createFixture();
  try {
    const result = await runNode(
      ['--no-warnings', cliPath, 'status'],
      environment(fixture),
      fixture.projectDir,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Cline \(cline\)/);
  } finally {
    await fixture.close();
  }
});

test('Cline installs all six managed files atomically and idempotently', async () => {
  const fixture = await createFixture();
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    const files = [
      fixture.guardScriptPath,
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
      fixture.hookScriptPath,
      fixture.guardWrapperPath,
      fixture.auditWrapperPath,
    ];
    const sources = new Map(await Promise.all(files.map(async (filePath) => [
      filePath,
      await readFile(filePath, 'utf-8'),
    ])));
    assert.match(sources.get(fixture.guardWrapperPath), /^#!\/usr\/bin\/env node\n\/\/ @elydora-cline-hook /);
    assert.match(sources.get(fixture.auditWrapperPath), /^#!\/usr\/bin\/env node\n\/\/ @elydora-cline-hook /);
    assert.equal(sources.get(path.join(fixture.agentDir, 'private.key')), VALID_PRIVATE_KEY);
    assert.deepEqual(await readJson(path.join(fixture.agentDir, 'config.json')), {
      org_id: 'org-1',
      agent_id: fixture.agentId,
      kid: 'kid-1',
      base_url: fixture.baseUrl,
      token: 'token-1',
      agent_name: 'cline',
    });

    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    for (const [filePath, source] of sources) {
      assert.equal(await readFile(filePath, 'utf-8'), source, filePath);
    }
    await assertNoTransactionFiles(fixture);
    await assertMissing(path.join(fixture.homeDir, 'Documents', 'Cline', 'Hooks', 'PreToolUse.mjs'));
    await assertMissing(path.join(fixture.projectDir, '.cline', 'hooks', 'PreToolUse.mjs'));
    await assertMissing(path.join(fixture.projectDir, '.clinerules', 'hooks', 'PreToolUse.mjs'));
    if (process.platform !== 'win32') {
      assert.equal((await stat(fixture.agentDir)).mode & 0o777, 0o700);
      for (const filePath of files.slice(0, 4)) {
        const expected = filePath.endsWith('.json') || filePath.endsWith('.key') ? 0o600 : 0o700;
        assert.equal((await stat(filePath)).mode & 0o777, expected);
      }
    }
  } finally {
    await fixture.close();
  }
});

test('Cline uses the official default when CLINE_DIR is absent', async () => {
  const fixture = await createFixture();
  try {
    const result = await fixture.install({}, null);
    assert.equal(result.code, 0, result.stderr);
    const defaultHooks = path.join(fixture.homeDir, '.cline', 'hooks');
    await readFile(path.join(defaultHooks, 'PreToolUse.mjs'));
    await readFile(path.join(defaultHooks, 'PostToolUse.mjs'));
    await assertMissing(fixture.guardWrapperPath);
  } finally {
    await fixture.close();
  }
});

test('Cline wrappers preserve native payload bytes and emit documented cancellation JSON', async () => {
  const fixture = await createFixture();
  const capturePath = path.join(fixture.rootDir, 'captured.json');
  try {
    assert.equal((await fixture.install()).code, 0);
    await writeFile(
      fixture.guardScriptPath,
      "process.stdin.resume(); process.stderr.write('Agent is frozen by Elydora.\\n'); process.exit(2);\n",
    );
    const prePayload = JSON.stringify({
      clineVersion: '3.0.46',
      hookName: 'tool_call',
      taskId: 'task-1',
      workspaceRoots: [fixture.projectDir],
      model: { provider: 'openai', slug: 'gpt-5.3-codex' },
      tool_call: { id: 'call-1', name: 'read_file', input: { path: 'README.md' } },
      preToolUse: { toolName: 'read_file', parameters: { path: 'README.md' } },
    });
    const guard = await runNode(
      [fixture.guardWrapperPath],
      environment(fixture),
      fixture.projectDir,
      prePayload,
    );
    assert.equal(guard.code, 0, guard.stderr);
    assert.deepEqual(JSON.parse(guard.stdout), {
      cancel: true,
      errorMessage: 'Agent is frozen by Elydora.',
    });

    await writeFile(fixture.hookScriptPath, `
      const fs = require('node:fs');
      const chunks = [];
      process.stdin.on('data', (chunk) => chunks.push(chunk));
      process.stdin.on('end', () => fs.writeFileSync(
        ${JSON.stringify(capturePath)},
        Buffer.concat(chunks),
      ));
    `);
    const postPayload = JSON.stringify({
      clineVersion: '3.0.46',
      hookName: 'tool_result',
      taskId: 'task-1',
      tool_result: { name: 'read_file', input: { path: 'README.md' }, output: 'ok' },
      postToolUse: {
        toolName: 'read_file',
        parameters: { path: 'README.md' },
        result: 'ok',
        success: true,
        executionTimeMs: 5,
      },
    });
    const audit = await runNode(
      [fixture.auditWrapperPath],
      environment(fixture),
      fixture.projectDir,
      postPayload,
    );
    assert.equal(audit.code, 0, audit.stderr);
    assert.equal(audit.stdout, '');
    assert.equal(await readFile(capturePath, 'utf-8'), postPayload);
  } finally {
    await fixture.close();
  }
});

test('Cline audit submits the complete native event with derived action fields', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const payload = {
      clineVersion: '3.0.46',
      hookName: 'tool_result',
      timestamp: '2026-07-19T12:00:00.000Z',
      taskId: 'task-1',
      workspaceRoots: [fixture.projectDir],
      userId: 'user-1',
      agent_id: 'cline-agent',
      parent_agent_id: null,
      tool_result: {
        id: 'call-1',
        name: 'read_file',
        input: { path: 'README.md' },
        output: 'ok',
        durationMs: 5,
      },
      postToolUse: {
        toolName: 'read_file',
        parameters: { path: 'README.md' },
        result: 'ok',
        success: true,
        executionTimeMs: 5,
      },
    };
    const result = await runNode(
      [fixture.auditWrapperPath],
      environment(fixture),
      fixture.projectDir,
      JSON.stringify(payload),
    );
    assert.equal(result.code, 0, result.stderr);
    assert.equal(api.requests.length, 1);
    const operation = JSON.parse(api.requests[0].raw);
    assert.deepEqual(operation.payload, payload);
    assert.deepEqual(operation.action, { tool: 'read_file' });
    assert.deepEqual(operation.subject, { session_id: 'task-1' });
  } finally {
    await api.close();
    await fixture.close();
  }
});

test('Cline wrappers keep pass decisions quiet and surface runtime failures', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    await writeFile(fixture.guardScriptPath, 'process.stdin.resume();\n');
    let result = await runNode(
      [fixture.guardWrapperPath],
      environment(fixture),
      fixture.projectDir,
      '{}',
    );
    assert.equal(result.code, 0, result.stderr);
    assert.equal(result.stdout, '');

    await writeFile(
      fixture.hookScriptPath,
      "process.stdin.resume(); process.stderr.write('audit failed\\n'); process.exit(7);\n",
    );
    result = await runNode(
      [fixture.auditWrapperPath],
      environment(fixture),
      fixture.projectDir,
      '{}',
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /audit failed/);
    assert.match(result.stderr, /exited with code 7/);
  } finally {
    await fixture.close();
  }
});

test('Cline status requires exact physical hooks, runtimes, config, and key', async (t) => {
  const cases = [
    ['guard runtime', 'guard', 'tampered\n', /"installed":false/],
    ['runtime config', 'config', '{ malformed', /parse Elydora runtime config/i],
    ['private key', 'key', 'invalid', /private key is invalid/i],
    ['guard wrapper', 'wrapper', 'tampered\n', /managed template/i],
  ];
  for (const [label, target, contents, expected] of cases) {
    await t.test(label, async () => {
      const fixture = await createFixture();
      try {
        assert.equal((await fixture.install()).code, 0);
        const healthy = await runPlugin(fixture, 'status', null);
        assert.deepEqual(JSON.parse(healthy.stdout), {
          installed: true,
          agentName: 'cline',
          displayName: 'Cline',
          hookConfigured: true,
          hookScriptExists: true,
          configPath: fixture.hooksDir,
        });
        const targets = {
          guard: fixture.guardScriptPath,
          config: path.join(fixture.agentDir, 'config.json'),
          key: path.join(fixture.agentDir, 'private.key'),
          wrapper: fixture.guardWrapperPath,
        };
        const filePath = targets[target];
        const next = target === 'wrapper'
          ? `${await readFile(filePath, 'utf-8')}${contents}`
          : contents;
        await writeFile(filePath, next);
        const status = await runPlugin(fixture, 'status', null);
        assert.match(`${status.stdout}\n${status.stderr}`, expected);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Cline rejects collisions, corrupt metadata, and invalid credentials before writes', async (t) => {
  await t.test('user collision', async () => {
    const fixture = await createFixture({ existingGuard: '// user hook\n' });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /owned by another integration/i);
      assert.equal(await readFile(fixture.guardWrapperPath, 'utf-8'), '// user hook\n');
      await assertMissing(fixture.agentDir);
      await assertMissing(fixture.auditWrapperPath);
    } finally {
      await fixture.close();
    }
  });

  await t.test('corrupt ownership metadata', async () => {
    const corrupt = '#!/usr/bin/env node\n// @elydora-cline-hook invalid\n';
    const fixture = await createFixture({ existingGuard: corrupt });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /parse Elydora Cline hook metadata/i);
      assert.equal(await readFile(fixture.guardWrapperPath, 'utf-8'), corrupt);
      await assertMissing(fixture.agentDir);
    } finally {
      await fixture.close();
    }
  });

  await t.test('non-canonical private key', async () => {
    const fixture = await createFixture();
    try {
      const result = await fixture.install({ privateKey: `${VALID_PRIVATE_KEY}=` });
      assert.equal(result.code, 1);
      assert.match(result.stderr, /canonical 32-byte base64url/i);
      await assertMissing(fixture.agentDir);
      await assertMissing(fixture.guardWrapperPath);
    } finally {
      await fixture.close();
    }
  });
});

test('Cline uninstall removes exact ownership and preserves adjacent hooks', async () => {
  const fixture = await createFixture();
  const userHook = path.join(fixture.hooksDir, 'PreToolUse.py');
  try {
    assert.equal((await fixture.install()).code, 0);
    await writeFile(userHook, '# user hook\n');
    assert.equal((await runPlugin(fixture, 'uninstall', 'agent-10')).code, 0);
    await readFile(fixture.guardWrapperPath);
    await readFile(fixture.auditWrapperPath);
    assert.equal((await runPlugin(fixture, 'uninstall', fixture.agentId)).code, 0);
    await assertMissing(fixture.guardWrapperPath);
    await assertMissing(fixture.auditWrapperPath);
    assert.equal(await readFile(userHook, 'utf-8'), '# user hook\n');
  } finally {
    await fixture.close();
  }
});

test('Cline CLI preflight preserves a collision before runtime creation', async () => {
  const fixture = await createFixture({ existingAudit: '// user PostToolUse hook\n' });
  const privateKeyFile = path.join(fixture.rootDir, 'private-key');
  try {
    await writeFile(privateKeyFile, VALID_PRIVATE_KEY, { mode: 0o600 });
    const result = await runNode([
      '--no-warnings',
      cliPath,
      'install',
      '--agent', 'cline',
      '--org_id', 'org-1',
      '--agent_id', fixture.agentId,
      '--kid', 'kid-1',
      '--private_key_file', privateKeyFile,
      '--base_url', fixture.baseUrl,
    ], environment(fixture), fixture.projectDir);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /owned by another integration/i);
    assert.equal(await readFile(fixture.auditWrapperPath, 'utf-8'), '// user PostToolUse hook\n');
    await assertMissing(path.join(fixture.homeDir, '.elydora'));
    await assertMissing(fixture.guardWrapperPath);
  } finally {
    await fixture.close();
  }
});
