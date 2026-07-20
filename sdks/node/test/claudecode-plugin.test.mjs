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
  readSettings,
  registryModuleUrl,
  runNode,
  runPlugin,
  VALID_PRIVATE_KEY,
  writeJson,
} from '../test-support/claudecode-test-helpers.mjs';

const GUARD_STATUS = 'Checking Elydora agent state';
const AUDIT_STATUS = 'Recording Elydora tool use';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

function assertManagedTriple(settings, fixture) {
  assertManagedHandler(
    managedHandler(settings, 'PreToolUse'),
    fixture.guardScriptPath,
    GUARD_STATUS,
  );
  assertManagedHandler(
    managedHandler(settings, 'PostToolUse'),
    fixture.hookScriptPath,
    AUDIT_STATUS,
  );
  assertManagedHandler(
    managedHandler(settings, 'PostToolUseFailure'),
    fixture.hookScriptPath,
    AUDIT_STATUS,
  );
}

test('Claude Code is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('claudecode'), {
    name: 'Claude Code',
    configDir: '~/.claude',
    configFile: 'settings.json',
  });
  const fixture = await createFixture();
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      CLAUDE_CONFIG_DIR: fixture.claudeConfigDir,
    }, fixture.projectDir);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Claude Code \(claudecode\)/);
  } finally {
    await fixture.close();
  }
});

test('Claude installs one exact managed triple and preserves user settings', async () => {
  const existing = {
    $schema: 'https://json.schemastore.org/claude-code-settings.json',
    model: 'sonnet',
    disableAllHooks: false,
    hooks: {
      Notification: [{
        matcher: 'permission_prompt',
        hooks: [{ type: 'http', url: 'https://example.test/hook', timeout: 1 }],
      }],
      PreToolUse: [{
        matcher: 'Bash',
        hooks: [{ type: 'command', command: 'existing-command', timeout: 5 }],
      }],
      Stop: [{
        hooks: [{
          type: 'command',
          command: 'background-check',
          asyncRewake: true,
          rewakeMessage: 'Background validation failed',
          rewakeSummary: 'Validation feedback',
        }],
      }],
    },
  };
  const fixture = await createFixture({ settings: existing });
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /run \/hooks and claude doctor/i);
    const installed = await readSettings(fixture.settingsPath);
    assert.equal(installed.settings.model, 'sonnet');
    assert.deepEqual(installed.settings.hooks.Notification, existing.hooks.Notification);
    assert.deepEqual(installed.settings.hooks.PreToolUse[0], existing.hooks.PreToolUse[0]);
    assert.deepEqual(installed.settings.hooks.Stop, existing.hooks.Stop);
    assertManagedTriple(installed.settings, fixture);

    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.settingsPath, 'utf-8'), installed.raw);
  } finally {
    await fixture.close();
  }
});

test('Claude resolves absolute, relative, empty, and absent CLAUDE_CONFIG_DIR values', async () => {
  const custom = await createFixture();
  try {
    assert.equal((await custom.install()).code, 0);
    assert.equal((await readSettings(custom.settingsPath)).settings.hooks !== undefined, true);
    await assertMissing(path.join(custom.homeDir, '.claude', 'settings.json'));
  } finally {
    await custom.close();
  }

  const fallback = await createFixture({ explicitClaudeConfig: false });
  try {
    assert.equal((await fallback.install()).code, 0);
    assert.equal((await readSettings(fallback.settingsPath)).settings.hooks !== undefined, true);
  } finally {
    await fallback.close();
  }

  const relative = await createFixture();
  try {
    relative.claudeConfigOverride = 'relative claude';
    assert.equal((await relative.install()).code, 0);
    const relativePath = path.join(relative.projectDir, 'relative claude', 'settings.json');
    assert.equal((await readSettings(relativePath)).settings.hooks !== undefined, true);
  } finally {
    await relative.close();
  }

  const empty = await createFixture();
  try {
    empty.claudeConfigOverride = '';
    assert.equal((await empty.install()).code, 0);
    const currentDirectoryPath = path.join(empty.projectDir, 'settings.json');
    assert.equal((await readSettings(currentDirectoryPath)).settings.hooks !== undefined, true);
  } finally {
    await empty.close();
  }
});

test('Claude parses settings before creating runtime files', async () => {
  const fixture = await createFixture({ settings: '{ malformed' });
  try {
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /parse Claude Code user settings/i);
    assert.equal(await readFile(fixture.settingsPath, 'utf-8'), '{ malformed');
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});

test('Claude rejects invalid official hook shapes before writes', async (t) => {
  const cases = [
    ['duplicate', '{"hooks":{},"hooks":{}}', /duplicate field "hooks"/i],
    ['disabled type', JSON.stringify({ disableAllHooks: 'yes' }), /must be a boolean/i],
    ['hooks shape', JSON.stringify({ hooks: null }), /field "hooks" must be an object/i],
    ['unknown event', JSON.stringify({ hooks: { MadeUp: [] } }), /unsupported hook event/i],
    ['event shape', JSON.stringify({ hooks: { PreToolUse: null } }), /must be an array/i],
    ['group shape', JSON.stringify({ hooks: { PreToolUse: [null] } }), /group.*must be an object/i],
    ['group field', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [], label: 'x' }] } }), /unsupported field "label"/i],
    ['matcher shape', JSON.stringify({ hooks: { PreToolUse: [{ matcher: 1, hooks: [] }] } }), /matcher must be a string/i],
    ['hooks missing', JSON.stringify({ hooks: { PreToolUse: [{}] } }), /must contain a hooks array/i],
    ['handler shape', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [null] }] } }), /handler.*must be an object/i],
    ['handler type', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'file' }] }] } }), /unsupported type/i],
    ['handler field', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', invented: true }] }] } }), /unsupported field "invented"/i],
    ['command value', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: '' }] }] } }), /non-empty string/i],
    ['argument type', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', args: [1] }] }] } }), /array of strings/i],
    ['timeout zero', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', timeout: 0 }] }] } }), /positive finite number/i],
    ['empty rewake message', JSON.stringify({ hooks: { Stop: [{ hooks: [{ type: 'command', command: 'x', rewakeMessage: '' }] }] } }), /rewakeMessage.*non-empty string/i],
    ['empty rewake summary', JSON.stringify({ hooks: { Stop: [{ hooks: [{ type: 'command', command: 'x', rewakeSummary: '' }] }] } }), /rewakeSummary.*non-empty string/i],
    ['headers type', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'http', url: 'https://example.test', headers: { A: 1 } }] }] } }), /map names to strings/i],
    ['input type', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'mcp_tool', server: 's', tool: 't', input: [] }] }] } }), /input.*must be an object/i],
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

test('Claude refuses installation while user hooks are disabled', async () => {
  const fixture = await createFixture({ settings: { disableAllHooks: true } });
  try {
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /disabled by disableAllHooks/i);
    await assertMissing(fixture.agentDir);
    assert.deepEqual((await readSettings(fixture.settingsPath)).settings, {
      disableAllHooks: true,
    });
  } finally {
    await fixture.close();
  }
});

test('Claude migrates exact legacy hooks and preserves ownership lookalikes', async () => {
  const fixture = await createFixture();
  const lookalike = { ...legacyHandler(fixture.guardScriptPath) };
  lookalike.command += ' --inspect';
  const legacy = {
    hooks: {
      PreToolUse: [{ hooks: [legacyHandler(fixture.guardScriptPath)] }, { hooks: [lookalike] }],
      PostToolUse: [{ hooks: [legacyHandler(fixture.hookScriptPath)] }],
    },
  };
  try {
    await writeJson(fixture.settingsPath, legacy);
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    let { settings } = await readSettings(fixture.settingsPath);
    assertManagedTriple(settings, fixture);
    assert(settings.hooks.PreToolUse.some((group) => group.hooks[0].command === lookalike.command));

    const managed = settings.hooks.PreToolUse.at(-1);
    managed.hooks.push({ type: 'command', command: 'user-command', timeout: 5 });
    await writeJson(fixture.settingsPath, settings);
    const uninstall = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(uninstall.code, 0, uninstall.stderr);
    settings = (await readSettings(fixture.settingsPath)).settings;
    assert.deepEqual(settings.hooks.PreToolUse.flatMap((group) => group.hooks), [
      lookalike,
      { type: 'command', command: 'user-command', timeout: 5 },
    ]);
    assert.equal(settings.hooks.PostToolUse, undefined);
    assert.equal(settings.hooks.PostToolUseFailure, undefined);
  } finally {
    await fixture.close();
  }
});

test('Claude uninstall preserves user sources and removes a fully managed file', async () => {
  const user = await createFixture({ settings: { model: 'sonnet', hooks: { Notification: [] } } });
  try {
    assert.equal((await user.install()).code, 0);
    const result = await runPlugin(user, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    assert.deepEqual((await readSettings(user.settingsPath)).settings, {
      model: 'sonnet',
      hooks: { Notification: [] },
    });
  } finally {
    await user.close();
  }

  const managed = await createFixture();
  try {
    assert.equal((await managed.install()).code, 0);
    assert.equal((await runPlugin(managed, 'uninstall', 'agent-1')).code, 0);
    await assertMissing(managed.settingsPath);
  } finally {
    await managed.close();
  }
});

test('Claude status requires one enabled complete triple and strict runtime identity', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, true);

    let { settings } = await readSettings(fixture.settingsPath);
    delete settings.hooks.PostToolUseFailure;
    await writeJson(fixture.settingsPath, settings);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);

    assert.equal((await fixture.install()).code, 0);
    settings = (await readSettings(fixture.settingsPath)).settings;
    settings.hooks.PostToolUseFailure.push(settings.hooks.PostToolUseFailure.at(-1));
    await writeJson(fixture.settingsPath, settings);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);

    assert.equal((await fixture.install()).code, 0);
    settings = (await readSettings(fixture.settingsPath)).settings;
    settings.disableAllHooks = true;
    await writeJson(fixture.settingsPath, settings);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);

    settings.disableAllHooks = false;
    await writeJson(fixture.settingsPath, settings);
    await writeFile(path.join(fixture.agentDir, 'private.key'), 'invalid');
    status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /private key is invalid/i);
  } finally {
    await fixture.close();
  }
});

test('Claude leaves project and local settings unchanged', async () => {
  const fixture = await createFixture();
  const projectSettings = path.join(fixture.projectDir, '.claude', 'settings.json');
  const localSettings = path.join(fixture.projectDir, '.claude', 'settings.local.json');
  const projectSource = '{"hooks":{"PreToolUse":[]}}\n';
  const localSource = '{"model":"haiku"}\n';
  try {
    await mkdir(path.dirname(projectSettings), { recursive: true });
    await writeFile(projectSettings, projectSource);
    await writeFile(localSettings, localSource);
    assert.equal((await fixture.install()).code, 0);
    assert.equal(await readFile(projectSettings, 'utf-8'), projectSource);
    assert.equal(await readFile(localSettings, 'utf-8'), localSource);
  } finally {
    await fixture.close();
  }
});

test('Claude CLI completes install, status, and uninstall end to end', async () => {
  const fixture = await createFixture();
  const privateKeyFile = path.join(fixture.rootDir, 'install-private.key');
  const tokenFile = path.join(fixture.rootDir, 'install-token.txt');
  const environment = {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    CLAUDE_CONFIG_DIR: fixture.claudeConfigDir,
  };
  try {
    await writeFile(privateKeyFile, `${VALID_PRIVATE_KEY}\n`, { mode: 0o600 });
    await writeFile(tokenFile, 'token-1\n', { mode: 0o600 });
    const install = await runNode([
      '--no-warnings',
      cliPath,
      'install',
      '--agent', 'claudecode',
      '--org_id', 'org-1',
      '--agent_id', 'agent-1',
      '--kid', 'kid-1',
      '--private_key_file', privateKeyFile,
      '--token_file', tokenFile,
      '--base_url', fixture.baseUrl,
    ], environment, fixture.projectDir);
    assert.equal(install.code, 0, install.stderr);
    assert.match(install.stdout, /installed for Claude Code/i);
    assertManagedTriple((await readSettings(fixture.settingsPath)).settings, fixture);

    const status = await runNode(
      ['--no-warnings', cliPath, 'status'],
      environment,
      fixture.projectDir,
    );
    assert.equal(status.code, 0, status.stderr);
    assert.match(status.stdout, /Claude Code \(claudecode\) \[installed\]/);

    const uninstall = await runNode([
      '--no-warnings',
      cliPath,
      'uninstall',
      '--agent', 'claudecode',
      '--agent_id', 'agent-1',
    ], environment, fixture.projectDir);
    assert.equal(uninstall.code, 0, uninstall.stderr);
    assert.match(uninstall.stdout, /uninstalled for Claude Code/i);
    await assertMissing(fixture.settingsPath);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
