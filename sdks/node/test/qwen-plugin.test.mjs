import assert from 'node:assert/strict';
import { lstat, mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  VALID_PRIVATE_KEY,
  assertManagedHandler,
  cliPath,
  configModuleUrl,
  contractModuleUrl,
  createFixture,
  installConfig,
  installationModuleUrl,
  legacyGroup,
  managedHandler,
  parseSettings,
  readSettings,
  registryModuleUrl,
  runNode,
  runPlugin,
  sourcesModuleUrl,
  writeSettings,
} from '../test-support/qwen.mjs';

const GUARD_NAME = 'elydora-guard';
const AUDIT_NAME = 'elydora-audit';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

function assertManagedTriple(settings) {
  const guard = managedHandler(settings, 'PreToolUse', GUARD_NAME);
  const audit = managedHandler(settings, 'PostToolUse', AUDIT_NAME);
  const failure = managedHandler(settings, 'PostToolUseFailure', AUDIT_NAME);
  assertManagedHandler(guard, GUARD_NAME);
  assertManagedHandler(audit, AUDIT_NAME);
  assertManagedHandler(failure, AUDIT_NAME);
  assert.equal(audit.command, failure.command);
  return { guard, audit, failure };
}

function isolatedEnvironment(fixture) {
  return {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    QWEN_HOME: '',
    QWEN_CODE_SYSTEM_DEFAULTS_PATH: path.join(fixture.rootDir, 'system-defaults.json'),
    QWEN_CODE_SYSTEM_SETTINGS_PATH: path.join(fixture.rootDir, 'system-settings.json'),
    QWEN_CODE_TRUSTED_FOLDERS_PATH: path.join(fixture.rootDir, 'trusted-folders.json'),
  };
}

test('Qwen Code is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('qwen'), {
    name: 'Qwen Code',
    configDir: '~/.qwen',
    configFile: 'settings.json',
  });
  const fixture = await createFixture();
  try {
    const result = await runNode(
      ['--no-warnings', cliPath, 'status'],
      isolatedEnvironment(fixture),
      fixture.projectDir,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Qwen Code \(qwen\)/);
  } finally {
    await fixture.close();
  }
});

test('Qwen installs an exact three-event contract and preserves every source', async () => {
  const existing = [
    '{',
    '  // Keep this user preference.',
    '  "theme": "GitHub",',
    '  "hooks": {',
    '    "FutureEvent": [null],',
    '    "PreToolUse": [{ "matcher": "read_file", "hooks": [{ "type": "command", "command": "user-hook" }] }]',
    '  }',
    '}',
    '',
  ].join('\r\n');
  const fixture = await createFixture({ settings: existing });
  const workspacePath = path.join(fixture.projectDir, '.qwen', 'settings.json');
  const defaultsPath = path.join(fixture.rootDir, 'system-defaults.json');
  const systemPath = path.join(fixture.rootDir, 'system-settings.json');
  await writeSettings(workspacePath, { owner: 'workspace' });
  await writeSettings(defaultsPath, { owner: 'defaults' });
  await writeSettings(systemPath, { owner: 'system' });
  const environment = {
    QWEN_CODE_SYSTEM_DEFAULTS_PATH: defaultsPath,
    QWEN_CODE_SYSTEM_SETTINGS_PATH: systemPath,
  };
  try {
    const first = await fixture.install({}, environment);
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /run \/hooks/i);
    const installed = await readSettings(fixture.settingsPath);
    assert.match(installed.raw, /Keep this user preference/);
    assert.match(installed.raw, /\r\n/);
    assert.equal(installed.settings.theme, 'GitHub');
    assert.deepEqual(installed.settings.hooks.FutureEvent, [null]);
    assert.equal(installed.settings.hooks.PreToolUse[0].hooks[0].command, 'user-hook');
    assertManagedTriple(installed.settings);
    for (const filePath of [
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
      fixture.guardScriptPath,
      fixture.hookScriptPath,
    ]) assert.equal((await lstat(filePath)).isFile(), true);

    const second = await fixture.install({}, environment);
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.settingsPath, 'utf-8'), installed.raw);
    assert.deepEqual(parseSettings(await readFile(workspacePath, 'utf-8')), { owner: 'workspace' });
    assert.deepEqual(parseSettings(await readFile(defaultsPath, 'utf-8')), { owner: 'defaults' });
    assert.deepEqual(parseSettings(await readFile(systemPath, 'utf-8')), { owner: 'system' });
  } finally {
    await fixture.close();
  }
});

test('Qwen resolves QWEN_HOME with official bootstrap precedence', async () => {
  const fixture = await createFixture();
  const firstHome = path.join(fixture.rootDir, 'first qwen home');
  const secondHome = path.join(fixture.rootDir, 'second qwen home');
  await writeSettings(
    path.join(fixture.homeDir, '.qwen', '.env'),
    `QWEN_HOME=${firstHome}\n`,
  );
  await writeSettings(path.join(fixture.homeDir, '.env'), `QWEN_HOME=${secondHome}\n`);
  await writeSettings(path.join(firstHome, '.env'), 'QWEN_RUNTIME_DIR=runtime\n');
  try {
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    const selected = path.join(firstHome, 'settings.json');
    assertManagedTriple((await readSettings(selected)).settings);
    await assertMissing(path.join(secondHome, 'settings.json'));
    await assertMissing(fixture.settingsPath);
    const status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.configPath, selected);
  } finally {
    await fixture.close();
  }
});

test('Qwen resolves explicit relative, tilde, and empty QWEN_HOME values', async () => {
  for (const [value, expectedPath] of [
    ['relative-qwen', (fixture) => path.join(fixture.projectDir, 'relative-qwen', 'settings.json')],
    ['~/custom-qwen', (fixture) => path.join(fixture.homeDir, 'custom-qwen', 'settings.json')],
    ['', (fixture) => fixture.settingsPath],
  ]) {
    const fixture = await createFixture();
    await writeSettings(
      path.join(fixture.homeDir, '.qwen', '.env'),
      `QWEN_HOME=${path.join(fixture.rootDir, 'ignored-home')}\n`,
    );
    try {
      const result = await fixture.install({}, { QWEN_HOME: value });
      assert.equal(result.code, 0, result.stderr);
      assertManagedTriple((await readSettings(expectedPath(fixture))).settings);
    } finally {
      await fixture.close();
    }
  }
});

test('Qwen validates current hook schemas and preserves future events', async () => {
  const fixture = await createFixture({ settings: {
    hooks: {
      FutureEvent: [null],
      MessageDisplay: [{ matcher: '[', hooks: [{
        type: 'http',
        url: 'http://127.0.0.1:8080/hook',
        headers: { Authorization: 'Bearer ${TOKEN}' },
        allowedEnvVars: ['TOKEN'],
        timeout: 10,
        once: true,
      }] }],
      Stop: [{ hooks: [{
        type: 'prompt',
        prompt: 'Evaluate $ARGUMENTS',
        model: 'fast',
        timeout: 30,
      }] }],
    },
  } });
  try {
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    const settings = (await readSettings(fixture.settingsPath)).settings;
    assert.deepEqual(settings.hooks.FutureEvent, [null]);
    assert.equal(settings.hooks.MessageDisplay[0].matcher, '[');
    assert.equal(settings.hooks.Stop[0].hooks[0].type, 'prompt');
  } finally {
    await fixture.close();
  }
});

test('Qwen rejects malformed affected settings before runtime writes', async (t) => {
  const cases = [
    ['syntax', '{ malformed', /parse Qwen Code user settings/i],
    ['root', '[]', /must contain a JSON object/i],
    ['trailing comma', '{ "theme": true, }', /parse Qwen Code user settings/i],
    ['duplicate', '{ "hooks": {}, "hooks": {} }', /duplicate field "hooks"/i],
    ['disable', '{ "disableAllHooks": "yes" }', /disableAllHooks.*boolean/i],
    ['hooks shape', '{ "hooks": [] }', /field "hooks" must be an object/i],
    ['event shape', '{ "hooks": { "PreToolUse": null } }', /must be an array/i],
    ['group shape', '{ "hooks": { "PreToolUse": [null] } }', /group.*must be an object/i],
    ['matcher shape', '{ "hooks": { "PreToolUse": [{ "matcher": 1, "hooks": [] }] } }', /matcher must be a string/i],
    ['matcher regex', '{ "hooks": { "PreToolUse": [{ "matcher": "[", "hooks": [] }] } }', /valid regular expression/i],
    ['notification matcher', '{ "hooks": { "Notification": [{ "matcher": "[", "hooks": [] }] } }', /valid regular expression/i],
    ['sequential', '{ "hooks": { "PreToolUse": [{ "sequential": 1, "hooks": [] }] } }', /sequential must be a boolean/i],
    ['hooks missing', '{ "hooks": { "PreToolUse": [{}] } }', /must contain a hooks array/i],
    ['handler shape', '{ "hooks": { "PreToolUse": [{ "hooks": [null] }] } }', /hooks\[0\].*object/i],
    ['handler type', '{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "function" }] }] } }', /unsupported type/i],
    ['command', '{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "command" }] }] } }', /non-empty command/i],
    ['http', '{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "http" }] }] } }', /non-empty url/i],
    ['prompt', '{ "hooks": { "Stop": [{ "hooks": [{ "type": "prompt" }] }] } }', /non-empty prompt/i],
    ['timeout', '{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "command", "command": "x", "timeout": -1 }] }] } }', /non-negative finite number/i],
    ['trust', '{ "security": { "folderTrust": { "enabled": "yes" } } }', /folderTrust.enabled.*boolean/i],
  ];
  for (const [name, settings, pattern] of cases) {
    await t.test(name, async () => {
      const fixture = await createFixture({ settings });
      try {
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, pattern);
        assert.equal(await readFile(fixture.settingsPath, 'utf-8'), settings);
        await assertMissing(fixture.agentDir);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Qwen surfaces malformed read-only sources and home routing', async () => {
  const system = await createFixture();
  const systemPath = path.join(system.rootDir, 'system.json');
  await writeSettings(systemPath, '{ malformed');
  try {
    const result = await system.install({}, { QWEN_CODE_SYSTEM_SETTINGS_PATH: systemPath });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /parse Qwen Code system override settings/i);
    assert.equal(await readFile(systemPath, 'utf-8'), '{ malformed');
    await assertMissing(system.settingsPath);
    await assertMissing(system.agentDir);
  } finally {
    await system.close();
  }

  const routing = await createFixture();
  await mkdir(path.join(routing.homeDir, '.qwen', '.env'), { recursive: true });
  try {
    const result = await routing.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /home environment.*physical file/i);
    await assertMissing(routing.settingsPath);
    await assertMissing(routing.agentDir);
  } finally {
    await routing.close();
  }
});

test('Qwen transaction aborts when a read-only source changes after prepare', async () => {
  const fixture = await createFixture();
  const systemPath = path.join(fixture.rootDir, 'system.json');
  const defaultsPath = path.join(fixture.rootDir, 'defaults.json');
  await writeSettings(systemPath, { owner: 'before' });
  const source = `
    import { writeFile } from 'node:fs/promises';
    import { readQwenSources } from ${JSON.stringify(sourcesModuleUrl)};
    import { renderQwenDocument } from ${JSON.stringify(configModuleUrl)};
    import {
      AUDIT_HOOK_NAME,
      GUARD_HOOK_NAME,
      buildQwenGroup,
    } from ${JSON.stringify(contractModuleUrl)};
    import {
      commitQwenInstallation,
      preflightQwenInstallation,
      prepareQwenInstallation,
    } from ${JSON.stringify(installationModuleUrl)};
    const config = JSON.parse(process.env.ELYDORA_TEST_CONFIG);
    const sources = await readQwenSources();
    const paths = await preflightQwenInstallation(config, sources);
    const rendered = renderQwenDocument(sources.user, undefined, new Map([
      ['PreToolUse', buildQwenGroup(paths.guardPath, GUARD_HOOK_NAME)],
      ['PostToolUse', buildQwenGroup(paths.auditPath, AUDIT_HOOK_NAME)],
      ['PostToolUseFailure', buildQwenGroup(paths.auditPath, AUDIT_HOOK_NAME)],
    ]));
    const prepared = await prepareQwenInstallation(config, sources, rendered);
    await writeFile(process.env.ELYDORA_SYSTEM_PATH, '{"owner":"after"}\\n');
    await commitQwenInstallation(prepared);
  `;
  try {
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        HOME: fixture.homeDir,
        USERPROFILE: fixture.homeDir,
        QWEN_HOME: '',
        QWEN_RUNTIME_DIR: '',
        QWEN_CODE_SYSTEM_SETTINGS_PATH: systemPath,
        QWEN_CODE_SYSTEM_DEFAULTS_PATH: defaultsPath,
        QWEN_CODE_TRUSTED_FOLDERS_PATH: path.join(fixture.rootDir, 'trusted.json'),
        ELYDORA_SYSTEM_PATH: systemPath,
        ELYDORA_TEST_CONFIG: JSON.stringify(installConfig(fixture)),
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /system override settings changed during Qwen Code installation/i);
    assert.deepEqual(parseSettings(await readFile(systemPath, 'utf-8')), { owner: 'after' });
    await assertMissing(fixture.settingsPath);
    await assertMissing(fixture.guardScriptPath);
    await assertMissing(fixture.hookScriptPath);
    await assertMissing(path.join(fixture.agentDir, 'config.json'));
    await assertMissing(path.join(fixture.agentDir, 'private.key'));
  } finally {
    await fixture.close();
  }
});

test('Qwen applies disableAllHooks precedence and workspace trust', async () => {
  const defaults = await createFixture({ settings: { disableAllHooks: false } });
  const defaultsPath = path.join(defaults.rootDir, 'defaults.json');
  await writeSettings(defaultsPath, { disableAllHooks: true });
  try {
    assert.equal((await defaults.install({}, {
      QWEN_CODE_SYSTEM_DEFAULTS_PATH: defaultsPath,
    })).code, 0);
  } finally {
    await defaults.close();
  }

  const system = await createFixture();
  const systemPath = path.join(system.rootDir, 'system.json');
  await writeSettings(systemPath, { disableAllHooks: true });
  try {
    const result = await system.install({}, { QWEN_CODE_SYSTEM_SETTINGS_PATH: systemPath });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /disableAllHooks.*system override/i);
    await assertMissing(system.agentDir);
  } finally {
    await system.close();
  }

  const untrusted = await createFixture({ settings: {
    security: { folderTrust: { enabled: true } },
  } });
  await writeSettings(path.join(untrusted.projectDir, '.qwen', 'settings.json'), {
    disableAllHooks: true,
  });
  await writeSettings(path.join(untrusted.homeDir, '.qwen', 'trustedFolders.json'), {
    [untrusted.projectDir]: 'DO_NOT_TRUST',
  });
  try {
    const result = await untrusted.install();
    assert.equal(result.code, 0, result.stderr);
    assertManagedTriple((await readSettings(untrusted.settingsPath)).settings);
  } finally {
    await untrusted.close();
  }
});

test('Qwen migrates exact legacy handlers and preserves ownership lookalikes', async () => {
  const fixture = await createFixture();
  const lookalike = legacyGroup(fixture.guardScriptPath);
  lookalike.hooks[0].timeout = 9_000;
  await writeSettings(fixture.settingsPath, {
    hooks: {
      PreToolUse: [legacyGroup(fixture.guardScriptPath), lookalike],
      PostToolUse: [legacyGroup(fixture.hookScriptPath)],
    },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    let settings = (await readSettings(fixture.settingsPath)).settings;
    assertManagedTriple(settings);
    assert(settings.hooks.PreToolUse.some((group) => group.hooks[0].timeout === 9_000));
    settings.hooks.PreToolUse.at(-1).userField = 'preserve-group';
    settings.hooks.PreToolUse.at(-1).hooks.push({ type: 'command', command: 'user-command' });
    await writeSettings(fixture.settingsPath, settings);
    const uninstall = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(uninstall.code, 0, uninstall.stderr);
    settings = (await readSettings(fixture.settingsPath)).settings;
    assert(settings.hooks.PreToolUse.some((group) => group.hooks[0].timeout === 9_000));
    assert(settings.hooks.PreToolUse.some((group) => (
      group.hooks.some((handler) => handler.command === 'user-command')
    )));
    assert.equal(settings.hooks.PostToolUse, undefined);
    assert.equal(settings.hooks.PostToolUseFailure, undefined);
  } finally {
    await fixture.close();
  }
});

test('Qwen uninstall removes an owned file and preserves user settings', async () => {
  const user = await createFixture({ settings: { theme: 'GitHub', hooks: { Notification: [] } } });
  try {
    assert.equal((await user.install()).code, 0);
    assert.equal((await runPlugin(user, 'uninstall', 'agent-1')).code, 0);
    assert.deepEqual((await readSettings(user.settingsPath)).settings, {
      theme: 'GitHub',
      hooks: { Notification: [] },
    });
  } finally {
    await user.close();
  }

  const owned = await createFixture();
  try {
    assert.equal((await owned.install()).code, 0);
    assert.match(await readFile(owned.settingsPath, 'utf-8'), /^\/\/ Managed by Elydora/);
    assert.equal((await runPlugin(owned, 'uninstall', 'agent-1')).code, 0);
    await assertMissing(owned.settingsPath);
  } finally {
    await owned.close();
  }
});

test('Qwen status requires exact hooks and strict runtime identity', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, true);
    const settings = (await readSettings(fixture.settingsPath)).settings;
    settings.hooks.PostToolUseFailure.push(settings.hooks.PostToolUseFailure.at(-1));
    await writeSettings(fixture.settingsPath, settings);
    status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, false);
    assert.equal((await fixture.install()).code, 0);
    await writeFile(path.join(fixture.agentDir, 'private.key'), 'invalid');
    const invalid = await runPlugin(fixture, 'status', null);
    assert.equal(invalid.code, 1);
    assert.match(invalid.stderr, /private key is invalid/i);
    assert.equal((await fixture.install()).code, 0);
    await writeFile(fixture.guardScriptPath, 'tampered');
    status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, false);
  } finally {
    await fixture.close();
  }
});

test('Qwen CLI completes install, status, and uninstall end to end', async () => {
  const fixture = await createFixture();
  const privateKeyFile = path.join(fixture.rootDir, 'install-private.key');
  const tokenFile = path.join(fixture.rootDir, 'install-token.txt');
  const environment = isolatedEnvironment(fixture);
  try {
    await writeFile(privateKeyFile, `${VALID_PRIVATE_KEY}\n`, { mode: 0o600 });
    await writeFile(tokenFile, 'token-1\n', { mode: 0o600 });
    const install = await runNode([
      '--no-warnings', cliPath, 'install',
      '--agent', 'qwen',
      '--org_id', 'org-1',
      '--agent_id', 'agent-1',
      '--kid', 'kid-1',
      '--private_key_file', privateKeyFile,
      '--token_file', tokenFile,
      '--base_url', fixture.baseUrl,
    ], environment, fixture.projectDir);
    assert.equal(install.code, 0, install.stderr);
    assertManagedTriple((await readSettings(fixture.settingsPath)).settings);
    const status = await runNode(
      ['--no-warnings', cliPath, 'status'],
      environment,
      fixture.projectDir,
    );
    assert.equal(status.code, 0, status.stderr);
    assert.match(status.stdout, /Qwen Code \(qwen\) \[installed\]/);
    const uninstall = await runNode([
      '--no-warnings', cliPath, 'uninstall',
      '--agent', 'qwen',
      '--agent_id', 'agent-1',
    ], environment, fixture.projectDir);
    assert.equal(uninstall.code, 0, uninstall.stderr);
    await assertMissing(fixture.settingsPath);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
