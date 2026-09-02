import assert from 'node:assert/strict';
import { lstat, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  VALID_PRIVATE_KEY,
  cliPath,
  createFixture,
  environment,
  managedHandler,
  readSettings,
  registryModuleUrl,
  runNode,
  runPlugin,
} from '../test-support/letta.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

function assertManagedTriple(settings) {
  for (const event of ['PreToolUse', 'PostToolUse', 'PostToolUseFailure']) {
    const group = settings.hooks[event].find((entry) => entry.hooks[0]?.timeout === 10_000);
    assert.deepEqual(Object.keys(group).sort(), ['hooks', 'matcher']);
    assert.equal(group.matcher, '*');
    assert.equal(group.hooks.length, 1);
    assert.deepEqual(Object.keys(group.hooks[0]).sort(), ['command', 'timeout', 'type']);
    assert.equal(group.hooks[0].type, 'command');
    assert.equal(group.hooks[0].timeout, 10_000);
    assert.doesNotMatch(group.hooks[0].command, /^node /);
  }
  assert.equal(
    managedHandler(settings, 'PostToolUse').command,
    managedHandler(settings, 'PostToolUseFailure').command,
  );
}

test('Letta Code is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('letta'), {
    name: 'Letta Code',
    configDir: '~/.letta',
    configFile: 'settings.json',
  });
  const fixture = await createFixture();
  try {
    const result = await runNode(
      ['--no-warnings', cliPath, 'status'],
      environment(fixture),
      fixture.projectDir,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Letta Code \(letta\)/);
  } finally {
    await fixture.close();
  }
});

test('Letta installs the current three-event contract and preserves every source', async () => {
  const globalSettings = [
    '{',
    '  "theme": "dark",',
    '  "hooks": {',
    '    "FutureEvent": [null],',
    '    "PreToolUse": [{ "matcher": "Bash", "hooks": [{ "type": "command", "command": "user-hook", "quiet": true }] }]',
    '  }',
    '}',
    '',
  ].join('\r\n');
  const projectSettings = { hooks: { PostToolUse: [{
    matcher: 'Read',
    hooks: [{ type: 'prompt', prompt: 'Review $ARGUMENTS', model: 'fast' }],
  }] } };
  const localSettings = { windowTitle: 'workspace' };
  const fixture = await createFixture({ globalSettings, projectSettings, localSettings });
  try {
    const projectBefore = await readFile(fixture.projectPath, 'utf-8');
    const localBefore = await readFile(fixture.localPath, 'utf-8');
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /run \/hooks/i);
    assert.match(first.stdout, /restart active sessions/i);
    const raw = await readFile(fixture.globalPath, 'utf-8');
    assert.match(raw, /\r\n/);
    const settings = JSON.parse(raw);
    assert.equal(settings.theme, 'dark');
    assert.deepEqual(settings.hooks.FutureEvent, [null]);
    assert.equal(settings.hooks.PreToolUse[0].hooks[0].command, 'user-hook');
    assertManagedTriple(settings);
    assert.equal(await readFile(fixture.projectPath, 'utf-8'), projectBefore);
    assert.equal(await readFile(fixture.localPath, 'utf-8'), localBefore);
    for (const filePath of [
      fixture.guardScriptPath,
      fixture.hookScriptPath,
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
    ]) await lstat(filePath);
    const before = await readFile(fixture.globalPath, 'utf-8');
    assert.equal((await fixture.install()).code, 0);
    assert.equal(await readFile(fixture.globalPath, 'utf-8'), before);
  } finally {
    await fixture.close();
  }
});

test('Letta validates known hook schemas and preserves future events', async () => {
  const fixture = await createFixture({ globalSettings: {
    hooks: {
      FutureEvent: [null],
      PreToolUse: [{ matcher: '[', hooks: [{
        type: 'command',
        command: 'user-command',
        timeout: 0,
        quiet: false,
        futureField: true,
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
    const settings = await readSettings(fixture.globalPath);
    assert.deepEqual(settings.hooks.FutureEvent, [null]);
    assert.equal(settings.hooks.PreToolUse[0].matcher, '[');
    assert.equal(settings.hooks.PreToolUse[0].hooks[0].futureField, true);
    assert.equal(settings.hooks.Stop[0].hooks[0].type, 'prompt');
  } finally {
    await fixture.close();
  }
});

test('Letta rejects malformed affected settings before runtime writes', async (t) => {
  const cases = [
    ['syntax', '{ malformed', /parse Letta Code global settings/i],
    ['root', '[]', /must contain a JSON object/i],
    ['comment', '{ // comment\n "hooks": {} }', /parse Letta Code global settings/i],
    ['trailing comma', '{ "theme": true, }', /parse Letta Code global settings/i],
    ['duplicate', '{ "hooks": {}, "hooks": {} }', /duplicate field "hooks"/i],
    ['hooks shape', '{ "hooks": [] }', /field "hooks" must be an object/i],
    ['disabled', '{ "hooks": { "disabled": "yes" } }', /hooks.disabled.*boolean/i],
    ['event shape', '{ "hooks": { "PreToolUse": null } }', /must be an array/i],
    ['group shape', '{ "hooks": { "PreToolUse": [null] } }', /group.*object/i],
    ['tool matcher', '{ "hooks": { "PreToolUse": [{ "hooks": [] }] } }', /matcher must be a string/i],
    ['simple matcher', '{ "hooks": { "Stop": [{ "matcher": "*", "hooks": [] }] } }', /matcher is unsupported/i],
    ['hooks missing', '{ "hooks": { "PreToolUse": [{ "matcher": "*" }] } }', /hooks array/i],
    ['handler shape', '{ "hooks": { "Stop": [{ "hooks": [null] }] } }', /hooks\[0\].*object/i],
    ['handler type', '{ "hooks": { "Stop": [{ "hooks": [{ "type": "http" }] }] } }', /unsupported type/i],
    ['command', '{ "hooks": { "PreToolUse": [{ "matcher": "*", "hooks": [{ "type": "command" }] }] } }', /non-empty command/i],
    ['prompt', '{ "hooks": { "Stop": [{ "hooks": [{ "type": "prompt" }] }] } }', /non-empty prompt/i],
    ['model', '{ "hooks": { "Stop": [{ "hooks": [{ "type": "prompt", "prompt": "x", "model": 1 }] }] } }', /model must be a string/i],
    ['quiet', '{ "hooks": { "Stop": [{ "hooks": [{ "type": "command", "command": "x", "quiet": 1 }] }] } }', /quiet must be a boolean/i],
    ['timeout', '{ "hooks": { "Stop": [{ "hooks": [{ "type": "command", "command": "x", "timeout": -1 }] }] } }', /non-negative finite number/i],
  ];
  for (const [name, source, pattern] of cases) {
    await t.test(name, async () => {
      const fixture = await createFixture({ globalSettings: source });
      try {
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, pattern);
        assert.equal(await readFile(fixture.globalPath, 'utf-8'), source);
        await assertMissing(fixture.agentDir);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Letta surfaces malformed project sources before writes', async (t) => {
  for (const field of ['projectSettings', 'localSettings']) {
    await t.test(field, async () => {
      const fixture = await createFixture({ [field]: '{ malformed' });
      try {
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, /parse Letta Code (project|project-local) settings/i);
        await assertMissing(fixture.globalPath);
        await assertMissing(fixture.agentDir);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Letta applies the official hooks.disabled precedence', async (t) => {
  const cases = [
    ['global true', { globalSettings: { hooks: { disabled: true } } }, 1],
    ['project true', { projectSettings: { hooks: { disabled: true } } }, 1],
    ['local true', { localSettings: { hooks: { disabled: true } } }, 1],
    ['global false override', {
      globalSettings: { hooks: { disabled: false } },
      projectSettings: { hooks: { disabled: true } },
      localSettings: { hooks: { disabled: true } },
    }, 0],
  ];
  for (const [name, options, expectedCode] of cases) {
    await t.test(name, async () => {
      const fixture = await createFixture(options);
      try {
        const result = await fixture.install();
        assert.equal(result.code, expectedCode, result.stderr);
        if (expectedCode === 1) {
          assert.match(result.stderr, /hooks.disabled/i);
          await assertMissing(fixture.agentDir);
        }
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Letta migrates exact legacy handlers and preserves ownership lookalikes', async () => {
  const fixture = await createFixture({ globalSettings: {} });
  const legacy = (scriptPath) => ({
    matcher: '*',
    hooks: [{ type: 'command', command: `node "${scriptPath}"` }],
  });
  const lookalike = legacy(fixture.guardScriptPath);
  lookalike.hooks[0].quiet = true;
  await writeFile(fixture.globalPath, `${JSON.stringify({ hooks: {
    PreToolUse: [legacy(fixture.guardScriptPath), lookalike],
    PostToolUse: [legacy(fixture.hookScriptPath)],
  } }, null, 2)}\n`, { mode: 0o600 });
  try {
    assert.equal((await fixture.install()).code, 0);
    let settings = await readSettings(fixture.globalPath);
    assertManagedTriple(settings);
    assert(settings.hooks.PreToolUse.some((group) => group.hooks[0].quiet === true));
    const managed = settings.hooks.PreToolUse.find((group) => group.hooks[0].timeout === 10_000);
    managed.userField = 'preserve-group';
    managed.hooks.push({ type: 'command', command: 'user-command' });
    await writeFile(fixture.globalPath, `${JSON.stringify(settings, null, 2)}\n`);
    assert.equal((await runPlugin(fixture, 'uninstall', 'agent-1')).code, 0);
    settings = await readSettings(fixture.globalPath);
    assert(settings.hooks.PreToolUse.some((group) => group.hooks[0].quiet === true));
    assert(settings.hooks.PreToolUse.some((group) => (
      group.hooks.some((handler) => handler.command === 'user-command')
    )));
    assert.equal(settings.hooks.PostToolUse, undefined);
    assert.equal(settings.hooks.PostToolUseFailure, undefined);
  } finally {
    await fixture.close();
  }
});

test('Letta status requires exact hooks and strict runtime identity', async () => {
  const fixture = await createFixture({ projectSettings: {} });
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, true);
    await writeFile(fixture.projectPath, '{"hooks":{"disabled":true}}\n');
    status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.hookConfigured, false);
    await writeFile(fixture.projectPath, '{}\n');
    status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, true);
    const settings = await readSettings(fixture.globalPath);
    settings.hooks.PostToolUseFailure.push(settings.hooks.PostToolUseFailure.at(-1));
    await writeFile(fixture.globalPath, `${JSON.stringify(settings, null, 2)}\n`);
    status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, false);
    assert.equal((await fixture.install()).code, 0);
    await writeFile(path.join(fixture.agentDir, 'private.key'), 'invalid');
    const invalid = await runPlugin(fixture, 'status', null);
    assert.equal(invalid.code, 1);
    assert.match(invalid.stderr, /private key is invalid/i);
    assert.equal((await fixture.install()).code, 0);
    await writeFile(fixture.hookScriptPath, 'tampered');
    status = JSON.parse((await runPlugin(fixture, 'status', null)).stdout);
    assert.equal(status.installed, false);
  } finally {
    await fixture.close();
  }
});

test('Letta uninstall removes a settings file containing only managed hooks', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    assert.equal((await runPlugin(fixture, 'uninstall', 'agent-1')).code, 0);
    await assertMissing(fixture.globalPath);
  } finally {
    await fixture.close();
  }
});

test('Letta CLI completes install, status, and uninstall end to end', async () => {
  const fixture = await createFixture({ globalSettings: { theme: 'dark' } });
  const privateKeyFile = path.join(fixture.rootDir, 'install-private.key');
  const tokenFile = path.join(fixture.rootDir, 'install-token.txt');
  try {
    await writeFile(privateKeyFile, `${VALID_PRIVATE_KEY}\n`, { mode: 0o600 });
    await writeFile(tokenFile, 'token-1\n', { mode: 0o600 });
    const install = await runNode([
      '--no-warnings', cliPath, 'install',
      '--agent', 'letta',
      '--org_id', 'org-1',
      '--agent_id', 'agent-1',
      '--kid', 'kid-1',
      '--private_key_file', privateKeyFile,
      '--token_file', tokenFile,
      '--base_url', fixture.baseUrl,
    ], environment(fixture), fixture.projectDir);
    assert.equal(install.code, 0, install.stderr);
    assertManagedTriple(await readSettings(fixture.globalPath));
    const status = await runNode(
      ['--no-warnings', cliPath, 'status'],
      environment(fixture),
      fixture.projectDir,
    );
    assert.equal(status.code, 0, status.stderr);
    assert.match(status.stdout, /Letta Code \(letta\) \[installed\]/);
    const uninstall = await runNode([
      '--no-warnings', cliPath, 'uninstall',
      '--agent', 'letta',
      '--agent_id', 'agent-1',
    ], environment(fixture), fixture.projectDir);
    assert.equal(uninstall.code, 0, uninstall.stderr);
    assert.deepEqual(await readSettings(fixture.globalPath), { theme: 'dark' });
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
