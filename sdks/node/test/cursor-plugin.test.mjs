import assert from 'node:assert/strict';
import {
  lstat,
  mkdir,
  readFile,
  readdir,
  rm,
  stat,
  symlink,
  writeFile,
} from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  assertNativeHandler,
  createFixture,
  installConfig,
  legacyHandler,
  managedHandler,
  registryModuleUrl,
  runPlugin,
  writeJson,
} from '../test-support/cursor-test-helpers.mjs';

test('Cursor is registered with the native user hook file', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('cursor'), {
    name: 'Cursor',
    configDir: '~/.cursor',
    configFile: 'hooks.json',
  });
});

test('Cursor install preserves hooks, migrates legacy entries, and is idempotent', async () => {
  const fixture = await createFixture();
  try {
    await writeJson(fixture.configPath, {
      description: 'user-owned',
      hooks: {
        sessionStart: [{ command: 'user-session' }],
        preToolUse: [
          { command: 'user-pre' },
          legacyHandler(fixture.guardScriptPath),
        ],
        postToolUse: [legacyHandler(fixture.hookScriptPath)],
        postToolUseFailure: [legacyHandler(fixture.hookScriptPath)],
      },
    });
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);

    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(config.version, 1);
    assert.equal(config.description, 'user-owned');
    assert.deepEqual(config.hooks.sessionStart, [{ command: 'user-session' }]);
    assert.deepEqual(config.hooks.preToolUse[0], { command: 'user-pre' });
    assert.equal(config.hooks.preToolUse.length, 2);
    assert.equal(config.hooks.postToolUse.length, 1);
    assert.equal(config.hooks.postToolUseFailure.length, 1);
    assertNativeHandler(managedHandler(config, 'preToolUse', 'guard.js'));
    assertNativeHandler(managedHandler(config, 'postToolUse', 'hook.js'));
    assertNativeHandler(managedHandler(config, 'postToolUseFailure', 'hook.js'));
  } finally {
    await fixture.close();
  }
});

test('Cursor status requires three handlers, matching identity, and every runtime secret', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);

    const hooks = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    delete hooks.hooks.postToolUseFailure;
    await writeJson(fixture.configPath, hooks);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).hookConfigured, false);

    assert.equal((await fixture.install()).code, 0);
    const keyPath = path.join(fixture.agentDir, 'private.key');
    await rm(keyPath);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);

    await writeFile(keyPath, 'restored', { mode: 0o600 });
    await writeJson(path.join(fixture.agentDir, 'config.json'), {
      agent_id: 'another-agent',
      agent_name: 'cursor',
    });
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);

    await writeFile(path.join(fixture.agentDir, 'config.json'), '{ malformed');
    status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /parse Elydora runtime config/i);
  } finally {
    await fixture.close();
  }
});

test('Cursor uninstall removes exact ownership and preserves user entries', async () => {
  const fixture = await createFixture({
    existingConfig: { version: 1, hooks: { sessionStart: [{ command: 'keep' }] } },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    for (const [event, script] of [
      ['preToolUse', 'guard.js'],
      ['postToolUse', 'hook.js'],
      ['postToolUseFailure', 'hook.js'],
    ]) {
      const other = structuredClone(managedHandler(config, event, script));
      other.command = other.command.replaceAll('agent-1', 'agent-10');
      config.hooks[event].push(other);
    }
    config.hooks.preToolUse.push({ command: 'user-pre' });
    await writeJson(fixture.configPath, config);

    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    const remaining = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.deepEqual(remaining.hooks.sessionStart, [{ command: 'keep' }]);
    assert.equal(remaining.hooks.preToolUse.length, 2);
    assert.equal(remaining.hooks.postToolUse.length, 1);
    assert.equal(remaining.hooks.postToolUseFailure.length, 1);
    assert.match(remaining.hooks.preToolUse[0].command, /agent-10/);
  } finally {
    await fixture.close();
  }
});

test('Cursor rejects malformed, duplicate, versionless user, and invalid configs before writes', async () => {
  const invalidConfigs = [
    '{ malformed',
    '[]\n',
    '{"hooks":{}}\n',
    '{"hooks":{"preToolUse":[{"command":"user"}]}}\n',
    '{"version":2,"hooks":{}}\n',
    '{"version":1,"hooks":null}\n',
    '{"version":1,"hooks":{"preToolUse":null}}\n',
    '{"version":1,"hooks":{"preToolUse":[null]}}\n',
    '{"version":1,"version":1,"hooks":{}}\n',
    '{"version":1,"hooks":{},}\n',
  ];
  for (const existingConfig of invalidConfigs) {
    const fixture = await createFixture({ existingConfig });
    try {
      const before = await readFile(fixture.configPath, 'utf-8');
      const result = await fixture.install();
      assert.equal(result.code, 1, `accepted ${existingConfig}`);
      assert.equal(await readFile(fixture.configPath, 'utf-8'), before);
      await assert.rejects(lstat(path.join(fixture.homeDir, '.elydora')), { code: 'ENOENT' });
    } finally {
      await fixture.close();
    }
  }
});

test('Cursor confines runtime scripts to the managed agent directory', async () => {
  for (const field of ['guardScriptPath', 'hookScriptPath']) {
    const fixture = await createFixture();
    try {
      const result = await runPlugin(fixture, 'install', installConfig(fixture, {
        [field]: path.join(fixture.homeDir, `unmanaged-${field}.js`),
      }));
      assert.equal(result.code, 1);
      assert.match(result.stderr, /managed agent directory/i);
      await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
    } finally {
      await fixture.close();
    }
  }
});

test('Cursor rejects a symbolic-link config and preserves its target', async (t) => {
  const fixture = await createFixture();
  try {
    const target = path.join(fixture.homeDir, 'cursor-hooks-target.json');
    const original = '{"version":1,"hooks":{}}\n';
    await mkdir(fixture.homeDir, { recursive: true });
    await writeFile(target, original);
    await mkdir(path.dirname(fixture.configPath), { recursive: true });
    try {
      await symlink(target, fixture.configPath);
    } catch (error) {
      if (error?.code === 'EPERM') {
        t.skip(`symbolic links unavailable: ${error.message}`);
        return;
      }
      throw error;
    }
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /physical file/i);
    assert.equal(await readFile(target, 'utf-8'), original);
    assert.equal((await lstat(fixture.configPath)).isSymbolicLink(), true);
  } finally {
    await fixture.close();
  }
});

test('Cursor status rejects symbolic-link runtime files', async (t) => {
  for (const name of ['config', 'key', 'guard', 'audit']) {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      const filePath = {
        config: path.join(fixture.agentDir, 'config.json'),
        key: path.join(fixture.agentDir, 'private.key'),
        guard: fixture.guardScriptPath,
        audit: fixture.hookScriptPath,
      }[name];
      const contents = await readFile(filePath);
      const target = path.join(fixture.homeDir, `${name}-runtime-target`);
      await writeFile(target, contents);
      await rm(filePath);
      try {
        await symlink(target, filePath);
      } catch (error) {
        if (error?.code === 'EPERM') {
          t.skip(`symbolic links unavailable: ${error.message}`);
          return;
        }
        throw error;
      }
      const status = await runPlugin(fixture, 'status', null);
      assert.equal(status.code, 1);
      assert.match(status.stderr, /physical file/i);
    } finally {
      await fixture.close();
    }
  }
});

test('Cursor removes an entirely managed config and leaves an absent config absent', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    assert.equal((await runPlugin(fixture, 'uninstall', 'agent-1')).code, 0);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });

    assert.equal((await runPlugin(fixture, 'uninstall', 'agent-1')).code, 0);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Cursor installation writes private files and leaves no transaction files', async () => {
  const fixture = await createFixture();
  try {
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    const names = [
      ...await readdir(fixture.agentDir),
      ...await readdir(path.dirname(fixture.configPath)),
    ];
    assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false);
    if (process.platform !== 'win32') {
      for (const filePath of [
        fixture.configPath,
        path.join(fixture.agentDir, 'config.json'),
        path.join(fixture.agentDir, 'private.key'),
      ]) {
        assert.equal((await stat(filePath)).mode & 0o777, 0o600);
      }
      for (const filePath of [fixture.guardScriptPath, fixture.hookScriptPath]) {
        assert.equal((await stat(filePath)).mode & 0o777, 0o700);
      }
    }
  } finally {
    await fixture.close();
  }
});
