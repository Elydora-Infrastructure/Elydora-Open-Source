import assert from 'node:assert/strict';
import { mkdir, readFile, rm, symlink, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  VALID_PRIVATE_KEY,
  assertMissing,
  assertNativeGroup,
  assertNoTransactionFiles,
  cliPath,
  createFixture,
  environment,
  managedGroup,
  managedHandler,
  readJsonc,
  registryModuleUrl,
  runHook,
  runNode,
  runPlugin,
  startApiServer,
  writeConfig,
} from '../test-support/droid-test-helpers.mjs';

function currentHooks(root) {
  return root.hooks;
}

test('Factory Droid is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('droid'), {
    name: 'Factory Droid',
    configDir: '~/.factory',
    configFile: 'hooks.json',
  });
  const fixture = await createFixture();
  try {
    const result = await runNode(
      ['--no-warnings', cliPath, 'agents'],
      environment(fixture),
      fixture.workspaceDir,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /droid\s+Factory Droid/);
  } finally {
    await fixture.close();
  }
});

test('Droid installs the official hook container and complete managed runtime', async () => {
  const fixture = await createFixture();
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /run \/hooks/);
    const rootSource = await readFile(fixture.rootPath, 'utf-8');
    assert.match(rootSource, /^\/\/ Managed by Elydora\r?\n/);
    const hooks = currentHooks(await readJsonc(fixture.rootPath));
    assertNativeGroup(managedGroup(hooks, 'PreToolUse', 'guard.js'));
    assertNativeGroup(managedGroup(hooks, 'PostToolUse', 'hook.js'));
    if (process.platform === 'win32') {
      assert.match(managedHandler(hooks, 'PreToolUse', 'guard.js').command, /^& '/);
    }
    assert.deepEqual(JSON.parse(await readFile(path.join(fixture.agentDir, 'config.json'), 'utf-8')), {
      org_id: 'org-1',
      agent_id: fixture.agentId,
      kid: 'kid-1',
      base_url: fixture.baseUrl,
      token: 'token-1',
      agent_name: 'droid',
    });
    assert.equal(await readFile(path.join(fixture.agentDir, 'private.key'), 'utf-8'), VALID_PRIVATE_KEY);
    assert.match(await readFile(fixture.hookScriptPath, 'utf-8'), /const NATIVE_PAYLOAD = true;/);

    const managedPaths = [
      fixture.rootPath,
      fixture.guardScriptPath,
      fixture.hookScriptPath,
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
    ];
    const before = new Map(await Promise.all(managedPaths.map(async (filePath) => [
      filePath,
      await readFile(filePath, 'utf-8'),
    ])));
    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    for (const [filePath, source] of before) {
      assert.equal(await readFile(filePath, 'utf-8'), source, filePath);
    }
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Droid migrates the legacy Windows command form to its native PowerShell contract', async (t) => {
  if (process.platform !== 'win32') {
    t.skip('Legacy double-quoted commands are Windows-specific');
    return;
  }
  const fixture = await createFixture({ rootConfig: { hooks: {} } });
  try {
    const legacyGroup = (scriptPath) => ({
      matcher: '*',
      hooks: [{
        type: 'command',
        command: `"${process.execPath}" "${scriptPath}"`,
        timeout: 10,
      }],
    });
    await writeConfig(fixture.rootPath, {
      hooks: {
        PreToolUse: [legacyGroup(fixture.guardScriptPath)],
        PostToolUse: [legacyGroup(fixture.hookScriptPath)],
      },
    });
    assert.equal((await fixture.install()).code, 0);
    const hooks = currentHooks(await readJsonc(fixture.rootPath));
    assert.equal(hooks.PreToolUse.length, 1);
    assert.equal(hooks.PostToolUse.length, 1);
    assert.match(hooks.PreToolUse[0].hooks[0].command, /^& '/);
  } finally {
    await fixture.close();
  }
});

test('Droid applies whole-source precedence and preserves inactive user sources', async (t) => {
  await t.test('root hooks.json wins over settings hooks', async () => {
    const rootConfig = `{
  // active root source
  "hooks": {
    "PreToolUse": [{ "matcher": "Read", "hooks": [{ "type": "command", "command": "root-user" }] }]
  }
}\n`;
    const settings = `{
  // inactive settings source
  "theme": "dark",
  "hooks": {
    "PostToolUse": [{ "matcher": "Edit", "hooks": [{ "type": "command", "command": "settings-user" }] }]
  }
}\n`;
    const fixture = await createFixture({ rootConfig, settings });
    try {
      assert.equal((await fixture.install()).code, 0);
      const rootSource = await readFile(fixture.rootPath, 'utf-8');
      const settingsSource = await readFile(fixture.settingsPath, 'utf-8');
      const hooks = currentHooks(await readJsonc(fixture.rootPath));
      assert.match(rootSource, /active root source/);
      assert.equal(hooks.PreToolUse[0].hooks[0].command, 'root-user');
      assertNativeGroup(managedGroup(hooks, 'PreToolUse', 'guard.js'));
      assertNativeGroup(managedGroup(hooks, 'PostToolUse', 'hook.js'));
      assert.equal(settingsSource, settings);
    } finally {
      await fixture.close();
    }
  });

  await t.test('settings hooks are the fallback when hook files are absent', async () => {
    const settings = '{\r\n\t"theme": "dark",\r\n\t"hooks": {}\r\n}\r\n';
    const fixture = await createFixture({ settings });
    try {
      assert.equal((await fixture.install()).code, 0);
      await assertMissing(fixture.rootPath);
      const source = await readFile(fixture.settingsPath, 'utf-8');
      const hooks = (await readJsonc(fixture.settingsPath)).hooks;
      assert.match(source, /\r\n\t\t"PreToolUse"/);
      assertNativeGroup(managedGroup(hooks, 'PreToolUse', 'guard.js'));
      assertNativeGroup(managedGroup(hooks, 'PostToolUse', 'hook.js'));
    } finally {
      await fixture.close();
    }
  });

  await t.test('local settings hooks override base settings hooks', async () => {
    const settings = { hooks: { Notification: [] } };
    const localSettings = { hooks: { SessionStart: [] } };
    const fixture = await createFixture({ settings, localSettings });
    try {
      assert.equal((await fixture.install()).code, 0);
      await assertMissing(fixture.rootPath);
      const base = await readJsonc(fixture.settingsPath);
      const local = await readJsonc(fixture.localSettingsPath);
      assert.equal(base.hooks.PreToolUse, undefined);
      assertNativeGroup(managedGroup(local.hooks, 'PreToolUse', 'guard.js'));
      assertNativeGroup(managedGroup(local.hooks, 'PostToolUse', 'hook.js'));
    } finally {
      await fixture.close();
    }
  });

  await t.test('legacy hook source stays active until Factory migrates it', async () => {
    const legacyConfig = {
      PreToolUse: [{ matcher: 'Read', hooks: [{ type: 'command', command: 'legacy-user' }] }],
    };
    const settings = { hooks: { PostToolUse: [] } };
    const fixture = await createFixture({ legacyConfig, settings });
    try {
      assert.equal((await fixture.install()).code, 0);
      await assertMissing(fixture.rootPath);
      const legacy = await readJsonc(fixture.legacyPath);
      assertNativeGroup(managedGroup(legacy, 'PreToolUse', 'guard.js'));
      assertNativeGroup(managedGroup(legacy, 'PostToolUse', 'hook.js'));
      assert.equal((await readJsonc(fixture.settingsPath)).hooks.PreToolUse, undefined);
    } finally {
      await fixture.close();
    }
  });
});

test('Droid guard blocks frozen agents and audit preserves the native event payload', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const hooks = currentHooks(await readJsonc(fixture.rootPath));
    const guard = managedHandler(hooks, 'PreToolUse', 'guard.js');
    const audit = managedHandler(hooks, 'PostToolUse', 'hook.js');
    const prePayload = {
      session_id: 'session-1',
      transcript_path: path.join(fixture.homeDir, 'transcript.jsonl'),
      cwd: fixture.workspaceDir,
      permission_mode: 'auto-high',
      hook_event_name: 'PreToolUse',
      tool_name: 'Execute',
      tool_input: { command: 'echo test' },
    };
    await writeConfig(path.join(fixture.agentDir, 'status-cache.json'), {
      status: 'frozen',
      cached_at: Date.now(),
    });
    const guardResult = await runHook(guard.command, fixture, JSON.stringify(prePayload));
    assert.equal(guardResult.code, 2, guardResult.stderr);
    assert.match(guardResult.stderr, /Agent "droid" is frozen/);

    const postPayload = {
      ...prePayload,
      hook_event_name: 'PostToolUse',
      tool_response: { output: 'test', success: true },
    };
    const auditResult = await runHook(audit.command, fixture, JSON.stringify(postPayload));
    assert.equal(auditResult.code, 0, auditResult.stderr);
    const operation = JSON.parse(api.requests.find((request) => request.method === 'POST').raw);
    assert.deepEqual(operation.payload, postPayload);
    assert.deepEqual(operation.subject, { session_id: 'session-1' });
    assert.deepEqual(operation.action, { tool: 'Execute' });
  } finally {
    await api.close();
    await fixture.close();
  }
});

test('Droid status requires the exact hook and runtime contract', async (t) => {
  const cases = [
    ['missing audit group', async (fixture) => {
      const root = await readJsonc(fixture.rootPath);
      delete root.hooks.PostToolUse;
      await writeConfig(fixture.rootPath, root);
    }, /"hookConfigured":false/],
    ['tampered guard', (fixture) => writeFile(fixture.guardScriptPath, 'tampered\n'), /"installed":false/],
    ['tampered audit', (fixture) => writeFile(fixture.hookScriptPath, 'tampered\n'), /"installed":false/],
    ['malformed runtime config', (fixture) => writeFile(
      path.join(fixture.agentDir, 'config.json'),
      '{ malformed',
    ), /parse Elydora runtime config/i],
    ['invalid private key', (fixture) => writeFile(
      path.join(fixture.agentDir, 'private.key'),
      'invalid',
    ), /private key is invalid/i],
  ];
  for (const [label, mutate, expected] of cases) {
    await t.test(label, async () => {
      const fixture = await createFixture();
      try {
        assert.equal((await fixture.install()).code, 0);
        const healthy = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
        assert.deepEqual(healthy, {
          installed: true,
          agentName: 'droid',
          displayName: 'Factory Droid',
          hookConfigured: true,
          hookScriptExists: true,
          configPath: fixture.rootPath,
        });
        await mutate(fixture);
        const status = await runPlugin(fixture, 'status', null);
        assert.match(`${status.stdout}\n${status.stderr}`, expected);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Droid accepts current extension fields and rejects malformed hook contracts', async () => {
  const valid = await createFixture({
    rootConfig: {
      hooks: {
        FutureEvent: [{
          commandRegex: '^git (status|diff)$',
          hooks: [{ type: 'command', command: 'future-user', timeout: 2 }],
        }],
      },
    },
  });
  try {
    assert.equal((await valid.install()).code, 0);
    assert.equal((await readJsonc(valid.rootPath)).hooks.FutureEvent[0].hooks[0].command, 'future-user');
  } finally {
    await valid.close();
  }

  const cases = [
    { rootConfig: '{ malformed', target: 'rootPath' },
    { rootConfig: '{"hooks":{},"hooks":{}}', target: 'rootPath' },
    { rootConfig: { hooks: [] }, target: 'rootPath' },
    { rootConfig: { hooks: { PreToolUse: null } }, target: 'rootPath' },
    { rootConfig: { hooks: { PreToolUse: [null] } }, target: 'rootPath' },
    { rootConfig: { hooks: { PreToolUse: [{ matcher: '[', hooks: [] }] } }, target: 'rootPath' },
    { rootConfig: { hooks: { PreToolUse: [{ commandRegex: '[', hooks: [] }] } }, target: 'rootPath' },
    { rootConfig: { hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', timeout: 0 }] }] } }, target: 'rootPath' },
    { rootConfig: { hooks: { PreToolUse: [{ hooks: [{ type: 'http', command: 'x' }] }] } }, target: 'rootPath' },
    { settings: { hooks: { showHookOutput: true } }, target: 'settingsPath' },
    { localSettings: '{ malformed', target: 'localSettingsPath' },
  ];
  for (const input of cases) {
    const fixture = await createFixture(input);
    try {
      const target = fixture[input.target];
      const before = await readFile(target, 'utf-8');
      const result = await fixture.install();
      assert.equal(result.code, 1, `${JSON.stringify(input)}\n${result.stderr}`);
      assert.equal(await readFile(target, 'utf-8'), before);
      await assertMissing(path.join(fixture.agentDir, 'config.json'));
    } finally {
      await fixture.close();
    }
  }
});

test('Droid uninstall removes exact ownership and preserves user hooks', async (t) => {
  await t.test('pre-existing JSONC keeps comments and user groups', async () => {
    const rootConfig = `{
  // user source
  "hooks": {
    "Notification": [{ "hooks": [{ "type": "command", "command": "keep" }] }]
  }
}\n`;
    const fixture = await createFixture({ rootConfig });
    try {
      assert.equal((await fixture.install()).code, 0);
      assert.equal((await runPlugin(fixture, 'uninstall', fixture.agentId)).code, 0);
      const source = await readFile(fixture.rootPath, 'utf-8');
      const root = await readJsonc(fixture.rootPath);
      assert.match(source, /user source/);
      assert.equal(root.hooks.Notification[0].hooks[0].command, 'keep');
      assert.equal(root.hooks.PreToolUse, undefined);
      assert.equal(root.hooks.PostToolUse, undefined);
    } finally {
      await fixture.close();
    }
  });

  await t.test('mixed groups and lookalike paths stay user-owned', async () => {
    const fixture = await createFixture({ rootConfig: { hooks: {} } });
    try {
      assert.equal((await fixture.install()).code, 0);
      const root = await readJsonc(fixture.rootPath);
      const group = managedGroup(root.hooks, 'PreToolUse', 'guard.js');
      const command = group.hooks[0].command;
      group.hooks.push({ type: 'command', command: 'user-command' });
      root.hooks.PreToolUse.push({
        matcher: '*',
        hooks: [{ type: 'command', command: command.replace('guard.js', 'guard.js.backup'), timeout: 10 }],
      });
      root.hooks.PreToolUse.push({
        matcher: '*',
        hooks: [{ type: 'command', command: command.replace('agent-1', 'agent-10'), timeout: 10 }],
      });
      await writeConfig(fixture.rootPath, root);
      assert.equal((await runPlugin(fixture, 'uninstall', fixture.agentId)).code, 0);
      const remaining = await readJsonc(fixture.rootPath);
      assert.match(JSON.stringify(remaining), /user-command/);
      assert.match(JSON.stringify(remaining), /guard\.js\.backup/);
      assert.match(JSON.stringify(remaining), /agent-10/);
      assert.equal(remaining.hooks.PostToolUse, undefined);
    } finally {
      await fixture.close();
    }
  });

  await t.test('an empty Elydora-owned hook file is removed', async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      assert.equal((await runPlugin(fixture, 'uninstall', fixture.agentId)).code, 0);
      await assertMissing(fixture.rootPath);
    } finally {
      await fixture.close();
    }
  });

  await t.test('an empty installation stays empty', async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await runPlugin(fixture, 'uninstall', fixture.agentId)).code, 0);
      await assertMissing(fixture.rootPath);
    } finally {
      await fixture.close();
    }
  });
});

test('Droid rejects linked configuration and runtime paths before writes', async (t) => {
  for (const kind of ['factory', 'hook', 'runtime']) {
    await t.test(kind, async () => {
      const fixture = await createFixture();
      try {
        const target = path.join(fixture.rootDir, `${kind}-target`);
        await mkdir(target, { recursive: true });
        if (kind === 'factory') {
          await mkdir(fixture.homeDir, { recursive: true });
          await symlink(target, fixture.factoryDir, 'junction');
        } else if (kind === 'hook') {
          await mkdir(fixture.factoryDir, { recursive: true });
          const targetFile = path.join(target, 'hooks.json');
          await writeConfig(targetFile, { hooks: {} });
          await symlink(targetFile, fixture.rootPath, 'file');
        } else {
          await mkdir(fixture.homeDir, { recursive: true });
          await symlink(target, path.join(fixture.homeDir, '.elydora'), 'junction');
        }
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, /physical (directory|file)/i);
      } catch (error) {
        if (error?.code === 'EPERM') {
          t.skip(`Symbolic links unavailable: ${error.message}`);
          return;
        }
        throw error;
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Droid CLI preflight blocks disabled hooks before creating runtime files', async () => {
  const fixture = await createFixture({ settings: { hooksDisabled: true } });
  const privateKeyPath = path.join(fixture.rootDir, 'private.key');
  try {
    await writeFile(privateKeyPath, VALID_PRIVATE_KEY, { mode: 0o600 });
    const result = await runNode([
      '--no-warnings',
      cliPath,
      'install',
      '--agent', 'droid',
      '--org_id', 'org-1',
      '--agent_id', fixture.agentId,
      '--kid', 'kid-1',
      '--private_key_file', privateKeyPath,
      '--base_url', fixture.baseUrl,
    ], environment(fixture), fixture.workspaceDir);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /hooks are disabled/i);
    await assertMissing(path.join(fixture.agentDir, 'config.json'));
    await assertMissing(fixture.rootPath);
  } finally {
    await rm(privateKeyPath, { force: true });
    await fixture.close();
  }
});
