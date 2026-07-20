import assert from 'node:assert/strict';
import { lstat, readFile, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  VALID_PRIVATE_KEY,
  auditWrapperName,
  cliPath,
  createFixture,
  generatedCommand,
  guardWrapperName,
  installConfig,
  managedHandler,
  pluginModuleUrl,
  readSettings,
  registryModuleUrl,
  runNode,
  runPlugin,
} from '../test-support/augment-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

test('Augment Code CLI is registered and owns its complete runtime installation', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  const { augmentPlugin } = await import(pluginModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('augment'), {
    name: 'Augment Code CLI',
    configDir: '~/.augment',
    configFile: 'settings.json',
  });
  assert.equal(augmentPlugin.managesRuntime, true);
  const fixture = await createFixture();
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
    }, fixture.projectDir);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Augment Code CLI \(augment\)/);
  } finally {
    await fixture.close();
  }
});

test('Auggie installation preserves official settings and is idempotent', async () => {
  const existing = {
    telemetryEnabled: false,
    hooks: {
      SessionStart: [{
        hooks: [{ type: 'command', command: 'existing-command', args: ['one'], timeout: 5_000 }],
        metadata: {
          includeConversationData: true,
          includeMCPMetadata: false,
          includeUserContext: true,
        },
        label: 'keep group metadata',
      }],
      PromptSubmit: [{ hooks: [{ type: 'command', command: 'prompt-hook' }] }],
      Notification: [{ hooks: [{ type: 'command', command: 'notification-hook' }] }],
      PreToolUse: [{
        matcher: 'launch-process',
        hooks: [{ type: 'command', command: 'user-command' }],
      }],
    },
  };
  const fixture = await createFixture({ settings: existing });
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    const firstRaw = await readFile(fixture.settingsPath, 'utf-8');
    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.settingsPath, 'utf-8'), firstRaw);

    const { settings } = await readSettings(fixture.settingsPath);
    assert.equal(settings.telemetryEnabled, false);
    assert.deepEqual(settings.hooks.SessionStart, existing.hooks.SessionStart);
    assert.deepEqual(settings.hooks.PromptSubmit, existing.hooks.PromptSubmit);
    assert.deepEqual(settings.hooks.Notification, existing.hooks.Notification);
    assert.deepEqual(settings.hooks.PreToolUse[0], existing.hooks.PreToolUse[0]);
    assert.equal(settings.hooks.PreToolUse.length, 2);
    assert.equal(settings.hooks.PostToolUse.length, 1);
    for (const [event, wrapperPath] of [
      ['PreToolUse', fixture.guardWrapperPath],
      ['PostToolUse', fixture.auditWrapperPath],
    ]) {
      const handler = managedHandler(settings, event, wrapperPath);
      assert.deepEqual(Object.keys(handler).sort(), ['command', 'timeout', 'type']);
      assert.equal(handler.type, 'command');
      assert.equal(handler.timeout, 10_000);
    }

    const runtimeConfig = JSON.parse(
      await readFile(path.join(fixture.agentDir, 'config.json'), 'utf-8'),
    );
    assert.deepEqual(runtimeConfig, {
      org_id: 'org-1',
      agent_id: 'agent-1',
      kid: 'kid-1',
      base_url: fixture.baseUrl,
      token: 'token-1',
      agent_name: 'augment',
    });
    assert.equal(await readFile(path.join(fixture.agentDir, 'private.key'), 'utf-8'), VALID_PRIVATE_KEY);
    assert.match(await readFile(fixture.hookScriptPath, 'utf-8'), /const NATIVE_PAYLOAD = true;/);
    const guardWrapper = await readFile(fixture.guardWrapperPath, 'utf-8');
    assert.match(guardWrapper, /guard\.js/);
    assert.match(await readFile(fixture.auditWrapperPath, 'utf-8'), new RegExp('hook\\.js'));
    if (process.platform !== 'win32') {
      assert.equal((await stat(fixture.guardWrapperPath)).mode & 0o111, 0o100);
      assert.match(guardWrapper, /^#!\/bin\/sh\nexec /);
    } else {
      assert.match(guardWrapper, /^@echo off\r?\n/);
      assert.match(guardWrapper, /exit \/b %errorlevel%/);
    }
    await assertMissing(path.join(fixture.projectDir, '.augment', 'settings.json'));
  } finally {
    await fixture.close();
  }
});

test('Auggie install replaces stale managed handlers and preserves user groups', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const { settings } = await readSettings(fixture.settingsPath);
    settings.hooks.PreToolUse.unshift({ hooks: [], label: 'keep empty group' });
    for (const [event, wrapperName] of [
      ['PreToolUse', guardWrapperName],
      ['PostToolUse', auditWrapperName],
    ]) {
      settings.hooks[event].push({ hooks: [{
        type: 'command',
        command: generatedCommand(path.join(fixture.agentDir, '..', 'agent-old', wrapperName)),
        timeout: 10_000,
      }] });
    }
    await writeFile(fixture.settingsPath, `${JSON.stringify(settings, null, 2)}\n`);
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    const current = (await readSettings(fixture.settingsPath)).settings;
    assert.doesNotMatch(JSON.stringify(current), /agent-old/);
    assert.deepEqual(current.hooks.PreToolUse[0], { hooks: [], label: 'keep empty group' });
    assert.equal(current.hooks.PreToolUse.length, 2);
    assert.equal(current.hooks.PostToolUse.length, 1);
  } finally {
    await fixture.close();
  }
});

test('Auggie uninstall removes exact ownership and preserves user settings', async () => {
  const fixture = await createFixture({ settings: {
    owner: 'user',
    hooks: { Notification: [] },
  } });
  try {
    assert.equal((await fixture.install()).code, 0);
    const { settings } = await readSettings(fixture.settingsPath);
    settings.hooks.PreToolUse[0].hooks.push({ type: 'command', command: 'user-command' });
    settings.hooks.PreToolUse.push({ hooks: [{
      type: 'command',
      command: generatedCommand(`${fixture.guardWrapperPath}.backup`),
      timeout: 10_000,
    }] });
    settings.hooks.PreToolUse.push({ hooks: [{
      type: 'command',
      command: generatedCommand(path.join(fixture.agentDir, '..', 'agent-10', guardWrapperName)),
      timeout: 10_000,
    }] });
    await writeFile(fixture.settingsPath, `${JSON.stringify(settings, null, 2)}\n`);
    const uninstallId = process.platform === 'win32' ? 'AGENT-1' : 'agent-1';
    const result = await runPlugin(fixture, 'uninstall', uninstallId);
    assert.equal(result.code, 0, result.stderr);
    const remaining = (await readSettings(fixture.settingsPath)).settings;
    assert.equal(remaining.owner, 'user');
    assert.deepEqual(remaining.hooks.Notification, []);
    assert.equal(remaining.hooks.PreToolUse[0].hooks[0].command, 'user-command');
    assert.match(JSON.stringify(remaining), /augment-guard.*backup/);
    assert.match(JSON.stringify(remaining), /agent-10/);
    assert.equal(remaining.hooks.PostToolUse, undefined);
  } finally {
    await fixture.close();
  }
});

test('Auggie uninstall removes settings owned entirely by Elydora', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    await assertMissing(fixture.settingsPath);
  } finally {
    await fixture.close();
  }
});

test('Auggie rejects malformed settings and invalid official hook contracts before writes', async () => {
  const cases = [
    '{ malformed',
    'null',
    '[]',
    '{"hooks":{},"hooks":{}}',
    { hooks: null },
    { hooks: { UnknownEvent: [] } },
    { hooks: { PreToolUse: null } },
    { hooks: { PreToolUse: [null] } },
    { hooks: { SessionStart: [{ matcher: '.*', hooks: [] }] } },
    { hooks: { PreToolUse: [{ matcher: '[', hooks: [] }] } },
    { hooks: { PreToolUse: [{ hooks: null }] } },
    { hooks: { PreToolUse: [{ hooks: [{ type: 'http', command: 'x' }] }] } },
    { hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: '', args: [] }] }] } },
    { hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', args: [1] }] }] } },
    { hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', timeout: 0 }] }] } },
    { hooks: { PreToolUse: [{ hooks: [], metadata: { includeUserContext: 'yes' } }] } },
  ];
  for (const settings of cases) {
    const fixture = await createFixture({ settings });
    try {
      const before = await readFile(fixture.settingsPath, 'utf-8');
      const result = await fixture.install();
      assert.equal(result.code, 1, `${JSON.stringify(settings)}\n${result.stderr}`);
      assert.equal(await readFile(fixture.settingsPath, 'utf-8'), before);
      await assertMissing(fixture.guardWrapperPath);
      await assertMissing(fixture.auditWrapperPath);
      await assertMissing(fixture.guardScriptPath);
    } finally {
      await fixture.close();
    }
  }
});

test('Auggie validates complete install config before creating files', async () => {
  const fixture = await createFixture();
  try {
    const result = await runPlugin(fixture, 'install', {
      ...installConfig(fixture),
      privateKey: 'invalid',
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /canonical 32-byte base64url/i);
    await assertMissing(fixture.settingsPath);
    await assertMissing(fixture.guardScriptPath);
  } finally {
    await fixture.close();
  }
});
