import assert from 'node:assert/strict';
import { lstat, mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  assertManagedHandler,
  cliPath,
  createFixture,
  legacyCommand,
  managedHandler,
  readGrokConfig,
  registryModuleUrl,
  runNode,
  runPlugin,
} from '../test-support/grok-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

function assertManagedTriple(config) {
  assertManagedHandler(managedHandler(config, 'PreToolUse'));
  assertManagedHandler(managedHandler(config, 'PostToolUse'));
  assertManagedHandler(managedHandler(config, 'PostToolUseFailure'));
  assert.equal(
    managedHandler(config, 'PostToolUse').command,
    managedHandler(config, 'PostToolUseFailure').command,
  );
}

test('Grok Build is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('grok'), {
    name: 'Grok Build',
    configDir: '~/.grok/hooks',
    configFile: 'elydora-audit.json',
  });
  const fixture = await createFixture();
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      GROK_HOME: fixture.grokHome,
    }, fixture.projectDir);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Grok Build \(grok\)/);
  } finally {
    await fixture.close();
  }
});

test('Grok installs an exact managed triple and preserves valid user hooks', async () => {
  const existing = {
    schemaVersion: 1,
    hooks: {
      SessionStart: [{
        hooks: [{ type: 'http', url: 'https://example.test/hook', timeout: 0 }],
        label: 'keep group metadata',
      }],
      PreToolUse: [{
        matcher: 'Bash|run_terminal_command',
        hooks: [{ type: 'command', command: 'existing-command', timeout: 5 }],
      }],
    },
  };
  const source = `${JSON.stringify(existing, null, 2)}\n`;
  const fixture = await createFixture({ config: source });
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /PostToolUseFailure hooks installed/i);
    const firstDocument = await readGrokConfig(fixture.configPath);
    assert.equal(firstDocument.config.schemaVersion, 1);
    assert.deepEqual(firstDocument.config.hooks.SessionStart, existing.hooks.SessionStart);
    assert.deepEqual(firstDocument.config.hooks.PreToolUse[0], existing.hooks.PreToolUse[0]);
    assert.equal(firstDocument.config.hooks.PreToolUse.length, 2);
    assertManagedTriple(firstDocument.config);

    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.configPath, 'utf-8'), firstDocument.raw);
    await assertMissing(path.join(fixture.homeDir, '.grok', 'hooks', 'elydora-audit.json'));
    await assertMissing(path.join(fixture.homeDir, '.claude', 'settings.json'));
    await assertMissing(path.join(fixture.homeDir, '.cursor', 'hooks.json'));
  } finally {
    await fixture.close();
  }
});

test('empty GROK_HOME resolves to the documented default', async () => {
  const fixture = await createFixture({ explicitGrokHome: false });
  try {
    assert.equal((await fixture.install()).code, 0);
    fixture.grokHomeOverride = '';
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);
  } finally {
    await fixture.close();
  }
});

test('Grok parses the hook file before creating runtime files', async () => {
  const fixture = await createFixture({ config: '{ malformed' });
  try {
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /parse Grok user hooks/i);
    assert.equal(await readFile(fixture.configPath, 'utf-8'), '{ malformed');
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});

test('Grok rejects inactive native hook shapes before writes', async (t) => {
  const cases = [
    ['duplicate field', '{"hooks":{},"hooks":{}}', /duplicate field "hooks"/i],
    ['hooks shape', JSON.stringify({ hooks: null }), /field "hooks" must be an object/i],
    ['event shape', JSON.stringify({ hooks: { PreToolUse: null } }), /must be an array/i],
    ['group shape', JSON.stringify({ hooks: { PreToolUse: [null] } }), /group.*must be an object/i],
    ['matcher shape', JSON.stringify({ hooks: { PreToolUse: [{ matcher: 1, hooks: [] }] } }), /matcher must be a string/i],
    ['lifecycle matcher', JSON.stringify({ hooks: { SessionStart: [{ matcher: 'x', hooks: [] }] } }), /cannot declare a matcher/i],
    ['handler shape', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [null] }] } }), /handler.*must be an object/i],
    ['handler type', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'file' }] }] } }), /unsupported type/i],
    ['command value', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: '' }] }] } }), /non-empty command/i],
    ['http value', JSON.stringify({ hooks: { PostToolUse: [{ hooks: [{ type: 'http', url: '' }] }] } }), /non-empty url/i],
    ['negative timeout', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', timeout: -1 }] }] } }), /non-negative integer/i],
    ['fraction timeout', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', timeout: 1.5 }] }] } }), /non-negative integer/i],
    ['environment shape', JSON.stringify({ hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', env: { A: 1 } }] }] } }), /env must map names to strings/i],
  ];
  for (const [name, config, pattern] of cases) {
    await t.test(name, async () => {
      const fixture = await createFixture({ config });
      try {
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, pattern);
        assert.equal(await readFile(fixture.configPath, 'utf-8'), config);
        await assertMissing(fixture.agentDir);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Grok migrates exact legacy commands and preserves ownership lookalikes', async () => {
  const fixture = await createFixture();
  const lookalike = `${legacyCommand(fixture.guardScriptPath)} --inspect`;
  const legacy = {
    hooks: {
      PreToolUse: [
        { hooks: [{ type: 'command', command: legacyCommand(fixture.guardScriptPath), timeout: 10 }] },
        { hooks: [{ type: 'command', command: lookalike, timeout: 10 }] },
      ],
      PostToolUse: [{ hooks: [{
        type: 'command',
        command: legacyCommand(fixture.hookScriptPath),
        timeout: 10,
      }] }],
    },
  };
  try {
    await mkdir(path.dirname(fixture.configPath), { recursive: true });
    await writeFile(fixture.configPath, JSON.stringify(legacy, null, 2));
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    let { config } = await readGrokConfig(fixture.configPath);
    assertManagedTriple(config);
    assert(config.hooks.PreToolUse.some((group) => group.hooks[0].command === lookalike));

    const managedGroup = config.hooks.PreToolUse.at(-1);
    managedGroup.hooks.push({ type: 'command', command: 'user-command', timeout: 10 });
    await writeFile(fixture.configPath, JSON.stringify(config, null, 2));
    const uninstall = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(uninstall.code, 0, uninstall.stderr);
    config = (await readGrokConfig(fixture.configPath)).config;
    assert.deepEqual(config.hooks.PreToolUse.flatMap((group) => group.hooks), [
      { type: 'command', command: lookalike, timeout: 10 },
      { type: 'command', command: 'user-command', timeout: 10 },
    ]);
    assert.equal(config.hooks.PostToolUse, undefined);
    assert.equal(config.hooks.PostToolUseFailure, undefined);
  } finally {
    await fixture.close();
  }
});

test('Grok uninstall preserves user config and removes a fully managed file', async () => {
  const userSource = `${JSON.stringify({ owner: 'user', hooks: { Notification: [] } }, null, 2)}\n`;
  const userFixture = await createFixture({ config: userSource });
  try {
    assert.equal((await userFixture.install()).code, 0);
    const result = await runPlugin(userFixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    assert.deepEqual((await readGrokConfig(userFixture.configPath)).config, {
      owner: 'user',
      hooks: { Notification: [] },
    });
  } finally {
    await userFixture.close();
  }

  const managedFixture = await createFixture();
  try {
    assert.equal((await managedFixture.install()).code, 0);
    const result = await runPlugin(managedFixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    await assertMissing(managedFixture.configPath);
  } finally {
    await managedFixture.close();
  }
});

test('Grok status requires one complete triple, exact identity, and private key', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, true);

    let { config } = await readGrokConfig(fixture.configPath);
    delete config.hooks.PostToolUseFailure;
    await writeFile(fixture.configPath, JSON.stringify(config, null, 2));
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);

    assert.equal((await fixture.install()).code, 0);
    config = (await readGrokConfig(fixture.configPath)).config;
    config.hooks.PostToolUseFailure.push(config.hooks.PostToolUseFailure.at(-1));
    await writeFile(fixture.configPath, JSON.stringify(config, null, 2));
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);

    assert.equal((await fixture.install()).code, 0);
    await writeFile(path.join(fixture.agentDir, 'private.key'), 'invalid');
    status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /private key is invalid/i);
  } finally {
    await fixture.close();
  }
});

test('Grok installation leaves no transaction files', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    for (const directory of [fixture.agentDir, path.dirname(fixture.configPath)]) {
      const names = await readdir(directory);
      assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false, names.join(', '));
    }
  } finally {
    await fixture.close();
  }
});
