import assert from 'node:assert/strict';
import { lstat, mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import { stringify as stringifyToml } from '@decimalturn/toml-patch';
import {
  assertManagedHook,
  cliPath,
  createFixture,
  installConfig,
  legacyCommand,
  managedHook,
  readKimiConfig,
  registryModuleUrl,
  runNode,
  runPlugin,
} from '../test-support/kimi-test-helpers.mjs';

const userStableConfig = [
  '# stable user config',
  'default_model = "kimi-code/k3"',
  '',
  '[[hooks]]',
  'event = "SessionStart"',
  'command = "existing-stable"',
  'timeout = 30 # keep stable hook',
  '',
].join('\n');

const userLegacyConfig = [
  '# legacy user config',
  'telemetry = false',
  '',
  '[[hooks]]',
  'event = "SessionEnd"',
  'command = "existing-legacy"',
  '',
].join('\n');

async function assertMissing(filePath) {
  await assert.rejects(readFile(filePath), { code: 'ENOENT' });
}

function assertManagedTriple(config) {
  assertManagedHook(managedHook(config, 'PreToolUse'), 'PreToolUse');
  assertManagedHook(managedHook(config, 'PostToolUse'), 'PostToolUse');
  assertManagedHook(managedHook(config, 'PostToolUseFailure'), 'PostToolUseFailure');
  assert.equal(
    managedHook(config, 'PostToolUse').command,
    managedHook(config, 'PostToolUseFailure').command,
  );
}

test('Kimi is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('kimi'), {
    name: 'Kimi Code',
    configDir: '~/.kimi-code',
    configFile: 'config.toml',
  });

  const fixture = await createFixture({ legacyDetected: false });
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      KIMI_CODE_HOME: fixture.kimiHome,
    }, fixture.projectDir);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Kimi Code \(kimi\)/);
  } finally {
    await fixture.close();
  }
});

test('Kimi installs an exact managed triple in both configs and is idempotent', async () => {
  const fixture = await createFixture({
    stableConfig: userStableConfig,
    legacyConfig: userLegacyConfig,
  });
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /Kimi Code and kimi-cli/i);
    const firstSources = [];
    for (const [configPath, comment, userCommand] of [
      [fixture.stablePath, '# stable user config', 'existing-stable'],
      [fixture.legacyPath, '# legacy user config', 'existing-legacy'],
    ]) {
      const { raw, config } = await readKimiConfig(configPath);
      firstSources.push(raw);
      assert.match(raw, new RegExp(comment));
      assert(config.hooks.some((hook) => hook.command === userCommand));
      assert.equal(config.hooks.length, 4);
      assertManagedTriple(config);
    }

    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.stablePath, 'utf-8'), firstSources[0]);
    assert.equal(await readFile(fixture.legacyPath, 'utf-8'), firstSources[1]);
    await assertMissing(path.join(fixture.homeDir, '.kimi-code', 'config.toml'));
  } finally {
    await fixture.close();
  }
});

test('fresh stable Kimi installation leaves the legacy migration source absent', async () => {
  const fixture = await createFixture({ legacyDetected: false });
  try {
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    const { config } = await readKimiConfig(fixture.stablePath);
    assert.equal(config.hooks.length, 3);
    assertManagedTriple(config);
    await assertMissing(fixture.legacyPath);
  } finally {
    await fixture.close();
  }
});

test('empty KIMI_CODE_HOME resolves to the documented default', async () => {
  const fixture = await createFixture({
    explicitKimiHome: false,
    legacyDetected: false,
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    fixture.kimiHomeOverride = '';
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);
    assertManagedTriple((await readKimiConfig(fixture.stablePath)).config);
  } finally {
    await fixture.close();
  }
});

test('legacy-only installation leaves the stable migration target absent', async () => {
  const fixture = await createFixture({
    explicitKimiHome: false,
    stableDetected: false,
  });
  try {
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    assertManagedTriple((await readKimiConfig(fixture.legacyPath)).config);
    await assertMissing(fixture.stablePath);
  } finally {
    await fixture.close();
  }
});

test('Kimi parses every selected config before creating runtime files', async () => {
  const fixture = await createFixture({
    stableConfig: userStableConfig,
    legacyConfig: '[malformed',
  });
  try {
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /parse kimi-cli legacy hooks config/i);
    assert.equal(await readFile(fixture.stablePath, 'utf-8'), userStableConfig);
    assert.equal(await readFile(fixture.legacyPath, 'utf-8'), '[malformed');
    await assert.rejects(lstat(fixture.agentDir), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Kimi rejects fields and events outside each official contract', async (t) => {
  for (const [name, stableConfig, legacyConfig, pattern] of [
    [
      'unsupported field',
      '[[hooks]]\nevent = "PreToolUse"\ncommand = "existing"\ncwd = "/tmp"\n',
      undefined,
      /unsupported field "cwd"/i,
    ],
    [
      'legacy stable-only event',
      undefined,
      '[[hooks]]\nevent = "Interrupt"\ncommand = "existing"\n',
      /unsupported event "Interrupt"/i,
    ],
    [
      'timeout bound',
      '[[hooks]]\nevent = "PreToolUse"\ncommand = "existing"\ntimeout = 601\n',
      undefined,
      /integer from 1 to 600/i,
    ],
  ]) {
    await t.test(name, async () => {
      const fixture = await createFixture({ stableConfig, legacyConfig });
      try {
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, pattern);
        await assert.rejects(lstat(fixture.agentDir), { code: 'ENOENT' });
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Kimi migrates exact legacy commands and preserves command lookalikes', async () => {
  const fixture = await createFixture({ legacyDetected: false });
  const lookalike = `${legacyCommand(fixture.guardScriptPath)} --inspect`;
  const source = [
    '[[hooks]]',
    'event = "PreToolUse"',
    `command = ${JSON.stringify(legacyCommand(fixture.guardScriptPath))}`,
    'timeout = 10',
    '',
    '[[hooks]]',
    'event = "PostToolUse"',
    `command = ${JSON.stringify(legacyCommand(fixture.hookScriptPath))}`,
    'timeout = 10',
    '',
    '[[hooks]]',
    'event = "PreToolUse"',
    `command = ${JSON.stringify(lookalike)}`,
    'timeout = 10',
    '',
  ].join('\n');
  try {
    await mkdir(path.dirname(fixture.stablePath), { recursive: true });
    await writeFile(fixture.stablePath, source, { encoding: 'utf-8', mode: 0o600 });
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    let config = (await readKimiConfig(fixture.stablePath)).config;
    assert.equal(config.hooks.length, 4);
    assert(config.hooks.some((hook) => hook.command === lookalike));
    assertManagedTriple(config);

    const uninstall = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(uninstall.code, 0, uninstall.stderr);
    config = (await readKimiConfig(fixture.stablePath)).config;
    assert.deepEqual(config.hooks.map((hook) => ({ ...hook })), [{
      event: 'PreToolUse',
      command: lookalike,
      timeout: 10,
    }]);
  } finally {
    await fixture.close();
  }
});

test('Kimi uninstall preserves user config and removes fully managed configs', async () => {
  const userFixture = await createFixture({
    stableConfig: userStableConfig,
    legacyConfig: userLegacyConfig,
  });
  try {
    assert.equal((await userFixture.install()).code, 0);
    const result = await runPlugin(userFixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    const stable = await readKimiConfig(userFixture.stablePath);
    const legacy = await readKimiConfig(userFixture.legacyPath);
    assert.match(stable.raw, /# keep stable hook/);
    assert.match(legacy.raw, /# legacy user config/);
    assert.deepEqual(stable.config.hooks.map((hook) => hook.command), ['existing-stable']);
    assert.deepEqual(legacy.config.hooks.map((hook) => hook.command), ['existing-legacy']);
  } finally {
    await userFixture.close();
  }

  const managedFixture = await createFixture();
  try {
    assert.equal((await managedFixture.install()).code, 0);
    const result = await runPlugin(managedFixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    await assertMissing(managedFixture.stablePath);
    await assertMissing(managedFixture.legacyPath);
  } finally {
    await managedFixture.close();
  }
});

test('Kimi status requires the complete triple, runtime identity, and private key', async () => {
  const fixture = await createFixture({ legacyDetected: false });
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);

    const { config } = await readKimiConfig(fixture.stablePath);
    config.hooks = config.hooks.filter((hook) => hook.event !== 'PostToolUseFailure');
    await writeFile(fixture.stablePath, stringifyToml(config), 'utf-8');
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

test('Kimi installation leaves no transaction files', async () => {
  const fixture = await createFixture();
  try {
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    for (const directory of [fixture.agentDir, fixture.kimiHome, fixture.legacyHome]) {
      const names = await readdir(directory);
      assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false, names.join(', '));
    }
  } finally {
    await fixture.close();
  }
});
