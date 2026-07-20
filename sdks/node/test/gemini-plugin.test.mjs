import assert from 'node:assert/strict';
import { lstat, mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  assertManagedHandler,
  cliPath,
  createFixture,
  legacyHandler,
  managedHandler,
  parseSettings,
  readSettings,
  registryModuleUrl,
  runNode,
  runPlugin,
  VALID_PRIVATE_KEY,
  writeSettings,
} from '../test-support/gemini-test-helpers.mjs';

const GUARD_NAME = 'elydora-guard';
const AUDIT_NAME = 'elydora-audit';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

function assertManagedPair(settings) {
  const guard = managedHandler(settings, 'BeforeTool', GUARD_NAME);
  const audit = managedHandler(settings, 'AfterTool', AUDIT_NAME);
  assertManagedHandler(guard, GUARD_NAME);
  assertManagedHandler(audit, AUDIT_NAME);
  return { guard, audit };
}

test('Gemini CLI is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('gemini'), {
    name: 'Gemini CLI',
    configDir: '~/.gemini',
    configFile: 'settings.json',
  });
  const fixture = await createFixture();
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      GEMINI_CLI_HOME: fixture.geminiCliHome,
    }, fixture.projectDir);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Gemini CLI \(gemini\)/);
  } finally {
    await fixture.close();
  }
});

test('Gemini installs an exact managed pair and preserves JSONC settings', async () => {
  const existing = [
    '{',
    '  // Keep this user preference.',
    '  "theme": "GitHub",',
    '  "hooks": {',
    '    "FutureEvent": [null],',
    '    "BeforeTool": [{ "matcher": "read_file", "hooks": [{ "type": "command", "command": "user-hook" }] }]',
    '  }',
    '}',
    '',
  ].join('\r\n');
  const fixture = await createFixture({ settings: existing });
  const projectSettings = path.join(fixture.projectDir, '.gemini', 'settings.json');
  const systemSettings = path.join(fixture.rootDir, 'system-settings.json');
  await writeSettings(projectSettings, '{ "owner": "project" }\n');
  await writeSettings(systemSettings, '{ "owner": "system" }\n');
  try {
    const environment = { GEMINI_CLI_SYSTEM_SETTINGS_PATH: systemSettings };
    const first = await fixture.install({}, environment);
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /run \/hooks list/i);
    const installed = await readSettings(fixture.settingsPath);
    assert.match(installed.raw, /Keep this user preference/);
    assert.match(installed.raw, /\r\n/);
    assert.equal(installed.settings.theme, 'GitHub');
    assert.deepEqual(installed.settings.hooks.FutureEvent, [null]);
    assert.equal(installed.settings.hooks.BeforeTool[0].hooks[0].command, 'user-hook');
    assertManagedPair(installed.settings);

    const second = await fixture.install({}, environment);
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.settingsPath, 'utf-8'), installed.raw);
    assert.equal(await readFile(projectSettings, 'utf-8'), '{ "owner": "project" }\n');
    assert.equal(await readFile(systemSettings, 'utf-8'), '{ "owner": "system" }\n');
  } finally {
    await fixture.close();
  }
});

test('Gemini resolves official GEMINI_CLI_HOME path semantics', async () => {
  const fallback = await createFixture({ explicitGeminiHome: false });
  try {
    assert.equal((await fallback.install()).code, 0);
    assertManagedPair((await readSettings(fallback.settingsPath)).settings);
  } finally {
    await fallback.close();
  }

  for (const [value, expected] of [
    ['', (fixture) => fixture.settingsPath],
    ['relative gemini', (fixture) => path.join(
      fixture.projectDir,
      'relative gemini',
      '.gemini',
      'settings.json',
    )],
    ['~', (fixture) => path.join(fixture.projectDir, '~', '.gemini', 'settings.json')],
  ]) {
    const fixture = await createFixture();
    fixture.geminiCliHomeOverride = value;
    try {
      const result = await fixture.install();
      assert.equal(result.code, 0, result.stderr);
      assertManagedPair((await readSettings(expected(fixture))).settings);
    } finally {
      await fixture.close();
    }
  }
});

test('Gemini rejects malformed official settings before every write', async (t) => {
  const cases = [
    ['syntax', '{ malformed', /parse Gemini CLI user settings/i],
    ['root', '[]', /must contain a JSON object/i],
    ['trailing comma', '{ "theme": true, }', /parse Gemini CLI user settings/i],
    ['duplicate', '{ "hooks": {}, "hooks": {} }', /duplicate field "hooks"/i],
    ['hooks shape', '{ "hooks": null }', /field "hooks" must be an object/i],
    ['event shape', '{ "hooks": { "BeforeTool": null } }', /must be an array/i],
    ['group shape', '{ "hooks": { "BeforeTool": [null] } }', /group.*must be an object/i],
    ['matcher shape', '{ "hooks": { "BeforeTool": [{ "matcher": 1, "hooks": [] }] } }', /matcher must be a string/i],
    ['sequential shape', '{ "hooks": { "BeforeTool": [{ "sequential": 1, "hooks": [] }] } }', /sequential must be a boolean/i],
    ['hooks missing', '{ "hooks": { "BeforeTool": [{}] } }', /must contain a hooks array/i],
    ['handler shape', '{ "hooks": { "BeforeTool": [{ "hooks": [null] }] } }', /handler.*must be an object/i],
    ['handler type', '{ "hooks": { "BeforeTool": [{ "hooks": [{ "type": "http" }] }] } }', /unsupported type/i],
    ['internal handler type', '{ "hooks": { "BeforeTool": [{ "hooks": [{ "type": "runtime", "name": "x" }] }] } }', /unsupported type/i],
    ['command value', '{ "hooks": { "BeforeTool": [{ "hooks": [{ "type": "command", "command": "" }] }] } }', /non-empty command/i],
    ['timeout value', '{ "hooks": { "BeforeTool": [{ "hooks": [{ "type": "command", "command": "x", "timeout": -1 }] }] } }', /non-negative finite number/i],
    ['environment value', '{ "hooks": { "BeforeTool": [{ "hooks": [{ "type": "command", "command": "x", "env": { "A": 1 } }] }] } }', /env must map names to strings/i],
    ['controls shape', '{ "hooksConfig": null }', /hooksConfig.*must be an object/i],
    ['controls field', '{ "hooksConfig": { "future": true } }', /unsupported field "future"/i],
    ['enabled shape', '{ "hooksConfig": { "enabled": "yes" } }', /enabled.*must be a boolean/i],
    ['disabled shape', '{ "hooksConfig": { "disabled": [1] } }', /array of strings/i],
    ['notifications shape', '{ "hooksConfig": { "notifications": 1 } }', /notifications.*must be a boolean/i],
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

test('Gemini respects canonical hook controls', async () => {
  for (const [settings, pattern] of [
    [{ hooksConfig: { enabled: false } }, /hooksConfig.enabled/i],
    [{ hooksConfig: { disabled: [GUARD_NAME] } }, /elydora-guard/i],
    [{ hooksConfig: { disabled: [AUDIT_NAME] } }, /elydora-audit/i],
  ]) {
    const fixture = await createFixture({ settings });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, pattern);
      await assertMissing(fixture.agentDir);
    } finally {
      await fixture.close();
    }
  }

  const legacy = await createFixture();
  try {
    await writeSettings(legacy.settingsPath, {
      hooksConfig: { disabled: [legacyHandler(legacy.guardScriptPath).command] },
    });
    const result = await legacy.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /hooksConfig.disabled/i);
    await assertMissing(legacy.agentDir);
  } finally {
    await legacy.close();
  }
});

test('Gemini migrates exact legacy handlers and preserves ownership lookalikes', async () => {
  const fixture = await createFixture();
  const lookalike = { ...legacyHandler(fixture.guardScriptPath) };
  lookalike.command += ' --inspect';
  await writeSettings(fixture.settingsPath, {
    hooks: {
      BeforeTool: [
        { hooks: [legacyHandler(fixture.guardScriptPath)] },
        { hooks: [lookalike] },
      ],
      AfterTool: [{ hooks: [legacyHandler(fixture.hookScriptPath)] }],
    },
  });
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    let settings = (await readSettings(fixture.settingsPath)).settings;
    assertManagedPair(settings);
    assert(settings.hooks.BeforeTool.some((group) => group.hooks[0].command === lookalike.command));

    const managed = settings.hooks.BeforeTool.at(-1);
    managed.hooks.push({ type: 'command', command: 'user-command' });
    await writeSettings(fixture.settingsPath, settings);
    const uninstall = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(uninstall.code, 0, uninstall.stderr);
    settings = (await readSettings(fixture.settingsPath)).settings;
    assert.deepEqual(settings.hooks.BeforeTool.flatMap((group) => group.hooks), [
      lookalike,
      { type: 'command', command: 'user-command' },
    ]);
    assert.equal(settings.hooks.AfterTool, undefined);
  } finally {
    await fixture.close();
  }
});

test('Gemini uninstall preserves user settings and removes an owned file', async () => {
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

test('Gemini status requires enabled exact hooks and strict runtime identity', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, true);

    let settings = (await readSettings(fixture.settingsPath)).settings;
    settings.hooks.AfterTool.push(settings.hooks.AfterTool.at(-1));
    await writeSettings(fixture.settingsPath, settings);
    status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, false);

    assert.equal((await fixture.install()).code, 0);
    settings = (await readSettings(fixture.settingsPath)).settings;
    settings.hooksConfig = { disabled: [AUDIT_NAME] };
    await writeSettings(fixture.settingsPath, settings);
    status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, false);
    assert.equal(status.hookConfigured, false);

    settings.hooksConfig = { disabled: [] };
    await writeSettings(fixture.settingsPath, settings);
    await writeFile(path.join(fixture.agentDir, 'private.key'), 'invalid');
    const invalid = await runPlugin(fixture, 'status', null);
    assert.equal(invalid.code, 1);
    assert.match(invalid.stderr, /private key is invalid/i);
  } finally {
    await fixture.close();
  }
});

test('Gemini CLI completes install, status, and uninstall end to end', async () => {
  const fixture = await createFixture();
  const privateKeyFile = path.join(fixture.rootDir, 'install-private.key');
  const tokenFile = path.join(fixture.rootDir, 'install-token.txt');
  const environment = {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    GEMINI_CLI_HOME: fixture.geminiCliHome,
  };
  try {
    await writeFile(privateKeyFile, `${VALID_PRIVATE_KEY}\n`, { mode: 0o600 });
    await writeFile(tokenFile, 'token-1\n', { mode: 0o600 });
    const install = await runNode([
      '--no-warnings',
      cliPath,
      'install',
      '--agent', 'gemini',
      '--org_id', 'org-1',
      '--agent_id', 'agent-1',
      '--kid', 'kid-1',
      '--private_key_file', privateKeyFile,
      '--token_file', tokenFile,
      '--base_url', fixture.baseUrl,
    ], environment, fixture.projectDir);
    assert.equal(install.code, 0, install.stderr);
    assertManagedPair((await readSettings(fixture.settingsPath)).settings);

    const status = await runNode(
      ['--no-warnings', cliPath, 'status'],
      environment,
      fixture.projectDir,
    );
    assert.equal(status.code, 0, status.stderr);
    assert.match(status.stdout, /Gemini CLI \(gemini\) \[installed\]/);

    const uninstall = await runNode([
      '--no-warnings',
      cliPath,
      'uninstall',
      '--agent', 'gemini',
      '--agent_id', 'agent-1',
    ], environment, fixture.projectDir);
    assert.equal(uninstall.code, 0, uninstall.stderr);
    await assertMissing(fixture.settingsPath);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
