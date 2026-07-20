import assert from 'node:assert/strict';
import {
  lstat,
  mkdir,
  readFile,
  readdir,
  realpath,
  rename,
  rm,
  stat,
  symlink,
  writeFile,
} from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  VALID_PRIVATE_KEY,
  assertNativeHandler,
  createFixture,
  installConfig,
  legacyHandler,
  managedHandler,
  registryModuleUrl,
  runNode,
  runPlugin,
  writeJson,
} from '../test-support/codex-test-helpers.mjs';

const GUARD_STATUS = 'Checking Elydora agent state';
const AUDIT_STATUS = 'Recording Elydora tool use';

test('Codex is registered with the native user hooks file', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('codex'), {
    name: 'OpenAI Codex',
    configDir: '~/.codex',
    configFile: 'hooks.json',
  });
});

test('Codex follows and canonicalizes the official CODEX_HOME root', async () => {
  const fixture = await createFixture();
  try {
    const target = path.join(fixture.rootDir, 'custom Codex home');
    const configured = path.join(fixture.rootDir, 'codex-home-link');
    await mkdir(target, { recursive: true });
    let codexHome = target;
    try {
      await symlink(target, configured, 'junction');
      codexHome = configured;
    } catch (error) {
      if (error?.code !== 'EPERM') throw error;
    }
    const install = await runPlugin(
      fixture,
      'install',
      installConfig(fixture),
      { CODEX_HOME: codexHome },
    );
    assert.equal(install.code, 0, install.stderr);
    const expectedPath = path.join(await realpath(target), 'hooks.json');
    assert.match(await readFile(expectedPath, 'utf-8'), /PreToolUse/);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });

    const status = await runPlugin(fixture, 'status', null, { CODEX_HOME: codexHome });
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).configPath, expectedPath);
    assert.equal(JSON.parse(status.stdout).installed, true);
  } finally {
    await fixture.close();
  }
});

test('Codex rejects missing and file-backed CODEX_HOME values before writes', async () => {
  const fixture = await createFixture();
  try {
    const missing = path.join(fixture.rootDir, 'missing-codex-home');
    let result = await runPlugin(
      fixture,
      'install',
      installConfig(fixture),
      { CODEX_HOME: missing },
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /Resolve CODEX_HOME/i);
    await assert.rejects(lstat(path.join(fixture.homeDir, '.elydora')), { code: 'ENOENT' });

    const filePath = path.join(fixture.rootDir, 'codex-home-file');
    await writeFile(filePath, 'file');
    result = await runPlugin(
      fixture,
      'install',
      installConfig(fixture),
      { CODEX_HOME: filePath },
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /CODEX_HOME is not a directory/i);
    await assert.rejects(lstat(path.join(fixture.homeDir, '.elydora')), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Codex install preserves sources, migrates legacy handlers, and is idempotent', async () => {
  const fixture = await createFixture();
  try {
    await writeJson(fixture.configPath, {
      description: 'User hooks',
      custom: { keep: true },
      hooks: {
        SessionStart: [{ matcher: 'startup', hooks: [{ type: 'command', command: 'keep' }] }],
        PreToolUse: [
          { matcher: 'Bash', hooks: [{ type: 'command', command: 'user-pre' }] },
          { matcher: '*', hooks: [legacyHandler(fixture.guardScriptPath, GUARD_STATUS)] },
        ],
        PostToolUse: [
          { matcher: '*', hooks: [legacyHandler(fixture.hookScriptPath, AUDIT_STATUS)] },
        ],
      },
    });
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /run \/hooks and approve both/i);
    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);

    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(config.description, 'User hooks');
    assert.deepEqual(config.custom, { keep: true });
    assert.equal(config.hooks.SessionStart[0].hooks[0].command, 'keep');
    assert.equal(config.hooks.PreToolUse.length, 2);
    assert.equal(config.hooks.PostToolUse.length, 1);
    assert.deepEqual(config.hooks.PreToolUse[0].hooks, [{ type: 'command', command: 'user-pre' }]);
    assertNativeHandler(managedHandler(config, 'PreToolUse', GUARD_STATUS), GUARD_STATUS);
    assertNativeHandler(managedHandler(config, 'PostToolUse', AUDIT_STATUS), AUDIT_STATUS);
  } finally {
    await fixture.close();
  }
});

test('Codex rejects malformed and ambiguous JSON before creating runtime files', async () => {
  const invalidConfigs = [
    '{ malformed',
    '[]\n',
    '{"hooks":null}\n',
    '{"hooks":{"PreToolUse":null}}\n',
    '{"hooks":{"PreToolUse":[null]}}\n',
    '{"hooks":{"PreToolUse":[{"hooks":null}]}}\n',
    '{"hooks":{"PreToolUse":[{"hooks":[null]}]}}\n',
    '{"hooks":{},"hooks":{}}\n',
    '{"hooks":{},}\n',
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

test('Codex status requires an exact hook pair, matching identity, and runtime secrets', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);

    const keyPath = path.join(fixture.agentDir, 'private.key');
    await rm(keyPath);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);

    assert.equal((await fixture.install()).code, 0);
    const hooks = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    managedHandler(hooks, 'PreToolUse', GUARD_STATUS).group.matcher = 'Bash';
    await writeJson(fixture.configPath, hooks);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).hookConfigured, false);

    assert.equal((await fixture.install()).code, 0);
    await writeJson(path.join(fixture.agentDir, 'config.json'), {
      agent_id: 'another-agent',
      agent_name: 'codex',
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

test('Codex uninstall removes exact ownership and preserves lookalike handlers', async () => {
  const fixture = await createFixture({
    existingConfig: {
      hooks: {
        SessionStart: [{ matcher: 'startup', hooks: [{ type: 'command', command: 'keep' }] }],
      },
    },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const otherGuard = legacyHandler(
      path.join(fixture.homeDir, '.elydora', 'agent-10', 'guard.js'),
      GUARD_STATUS,
    );
    const otherAudit = legacyHandler(
      path.join(fixture.homeDir, '.elydora', 'agent-10', 'hook.js'),
      AUDIT_STATUS,
    );
    const lookalike = {
      type: 'command',
      command: `inspect ${fixture.guardScriptPath}`,
      commandWindows: `inspect ${fixture.guardScriptPath}`,
      timeout: 10,
      statusMessage: GUARD_STATUS,
    };
    const modifiedGroup = {
      matcher: 'Bash',
      hooks: [structuredClone(managedHandler(config, 'PreToolUse', GUARD_STATUS).handler)],
    };
    config.hooks.PreToolUse.push(
      { matcher: '*', hooks: [otherGuard] },
      { matcher: '*', hooks: [lookalike] },
      modifiedGroup,
    );
    config.hooks.PostToolUse.push({ matcher: '*', hooks: [otherAudit] });
    await writeJson(fixture.configPath, config);

    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    const remaining = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(remaining.hooks.SessionStart[0].hooks[0].command, 'keep');
    assert.equal(remaining.hooks.PreToolUse.length, 3);
    assert.equal(remaining.hooks.PostToolUse.length, 1);
    assert.match(remaining.hooks.PreToolUse[0].hooks[0].command, /agent-10/);
    assert.deepEqual(remaining.hooks.PreToolUse[1].hooks[0], lookalike);
    assert.deepEqual(remaining.hooks.PreToolUse[2], { matcher: 'Bash', hooks: [] });
  } finally {
    await fixture.close();
  }
});

test('Codex confines generated runtimes to the managed agent directory', async () => {
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

test('Codex validates runtime identity, credentials, and API origin before writes', async () => {
  const invalidOverrides = [
    { agentName: 'cursor' },
    { privateKey: 'invalid' },
    { token: '' },
    { baseUrl: 'file:///tmp/elydora' },
    { baseUrl: 'https://user:secret@api.elydora.com' },
    { baseUrl: 'https://api.elydora.com?tenant=one' },
  ];
  for (const overrides of invalidOverrides) {
    const fixture = await createFixture();
    try {
      const result = await runPlugin(fixture, 'install', installConfig(fixture, overrides));
      assert.equal(result.code, 1, JSON.stringify(overrides));
      await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
      await assert.rejects(lstat(path.join(fixture.homeDir, '.elydora')), { code: 'ENOENT' });
    } finally {
      await fixture.close();
    }
  }
});

test('Codex rejects linked hook and runtime files', async (t) => {
  for (const kind of ['hooks', 'config', 'key', 'guard', 'audit']) {
    const fixture = await createFixture();
    try {
      if (kind === 'hooks') {
        const target = path.join(fixture.homeDir, 'hooks-target.json');
        const original = '{"hooks":{}}\n';
        await mkdir(path.dirname(fixture.configPath), { recursive: true });
        await writeFile(target, original);
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
        continue;
      }

      assert.equal((await fixture.install()).code, 0);
      const filePath = {
        config: path.join(fixture.agentDir, 'config.json'),
        key: path.join(fixture.agentDir, 'private.key'),
        guard: fixture.guardScriptPath,
        audit: fixture.hookScriptPath,
      }[kind];
      const contents = await readFile(filePath);
      const target = path.join(fixture.homeDir, `${kind}-target`);
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

test('Codex removes an entirely managed file and leaves absent hooks absent', async () => {
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

test('Codex uninstall rejects a linked default hooks directory', async (t) => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const codexDirectory = path.dirname(fixture.configPath);
    const target = path.join(fixture.rootDir, 'codex-hooks-target');
    await rename(codexDirectory, target);
    try {
      await symlink(target, codexDirectory, 'junction');
    } catch (error) {
      if (error?.code === 'EPERM') {
        t.skip(`symbolic links unavailable: ${error.message}`);
        return;
      }
      throw error;
    }

    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 1);
    assert.match(result.stderr, /physical directory/i);
    assert.match(await readFile(path.join(target, 'hooks.json'), 'utf-8'), /PreToolUse/);
  } finally {
    await fixture.close();
  }
});

test('Codex installation protects credentials and cleans transaction files', async () => {
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

test('Codex CLI preflight preserves malformed hooks before runtime creation', async () => {
  const fixture = await createFixture({ existingConfig: '{ malformed' });
  try {
    const privateKeyPath = path.join(fixture.rootDir, 'private.key');
    await writeFile(privateKeyPath, VALID_PRIVATE_KEY, { mode: 0o600 });
    const result = await runNode([
      path.resolve('dist/cli.js'),
      'install',
      '--agent', 'codex',
      '--org_id', 'org-1',
      '--agent_id', 'agent-1',
      '--private_key_file', privateKeyPath,
      '--kid', 'kid-1',
      '--base_url', fixture.baseUrl,
    ], {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
    }, fixture.projectDir);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /Codex user hooks/i);
    await assert.rejects(lstat(path.join(fixture.homeDir, '.elydora')), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});
