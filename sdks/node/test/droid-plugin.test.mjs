import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';
import { parse } from 'jsonc-parser';

const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/droid.js')).href;
const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
const cliPath = path.resolve('dist/cli.js');

function runNode(args, env, input = '', cwd) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, args, {
      cwd,
      env: { ...process.env, ...env },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.once('error', reject);
    child.once('close', (code) => resolve({ code, stdout, stderr }));
    child.stdin.end(input);
  });
}

function runCommand(command, input = '') {
  return new Promise((resolve, reject) => {
    const child = spawn(command, { shell: true, stdio: ['pipe', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.once('error', reject);
    child.once('close', (code) => resolve({ code, stdout, stderr }));
    child.stdin.end(input);
  });
}

function parseJsonc(raw) {
  const errors = [];
  const value = parse(raw, errors, { allowTrailingComma: true });
  assert.deepEqual(errors, []);
  return value;
}

function serialize(value) {
  return typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`;
}

async function writeConfig(filePath, value) {
  if (value === undefined) return;
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, serialize(value));
}

async function runPlugin(fixture, method, argument) {
  const script = `
    import { droidPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await droidPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', script],
    {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    },
    '',
    fixture.workspaceDir,
  );
}

async function createFixture({ hooks, legacyHooks, settings, guardSource, hookSource } = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-droid-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote");
  const workspaceDir = path.join(homeDir, 'workspace');
  const factoryDir = path.join(homeDir, '.factory');
  const configPath = path.join(factoryDir, 'hooks.json');
  const legacyConfigPath = path.join(factoryDir, 'hooks', 'hooks.json');
  const settingsPath = path.join(factoryDir, 'settings.json');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(workspaceDir, { recursive: true });
  await mkdir(agentDir, { recursive: true });
  await writeFile(
    guardScriptPath,
    guardSource ?? "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n",
  );
  await writeFile(hookScriptPath, hookSource ?? 'process.exit(0);\n');
  await writeFile(path.join(agentDir, 'config.json'), JSON.stringify({
    agent_id: 'agent-1',
    agent_name: 'droid',
  }));
  await writeConfig(configPath, hooks);
  await writeConfig(legacyConfigPath, legacyHooks);
  await writeConfig(settingsPath, settings);
  const fixture = {
    agentDir,
    configPath,
    factoryDir,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    legacyConfigPath,
    settingsPath,
    workspaceDir,
    async close() { await rm(rootDir, { recursive: true, force: true }); },
  };
  fixture.installResult = await runPlugin(fixture, 'install', {
    agentName: 'droid',
    agentId: 'agent-1',
    guardScriptPath,
    hookScriptPath,
  });
  return fixture;
}

function managedHandler(groups, scriptPath) {
  for (const group of groups ?? []) {
    const handler = group.hooks.find(
      (candidate) => candidate.command?.includes(scriptPath),
    );
    if (handler) return handler;
  }
  return undefined;
}

test('Factory Droid is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('droid'), {
    name: 'Factory Droid',
    configDir: '~/.factory',
    configFile: 'hooks.json',
  });
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-droid-cli-'));
  const workspaceDir = path.join(homeDir, 'workspace');
  await mkdir(workspaceDir);
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: homeDir,
      USERPROFILE: homeDir,
    }, '', workspaceDir);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Factory Droid \(droid\)/);
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('Droid install preserves JSONC and follows per-event source precedence', async () => {
  const hooks = `{
  // root hook source
  "PreToolUse": [
    // keep root group comment
    { "matcher": "Read", "hooks": [{ "type": "command", "command": "root-user" }] }
  ],
  "Notification": []
}\n`;
  const settings = `{
  // general setting
  "theme": "dark",
  "hooks": {
    // settings fallback event
    "PostToolUse": [
      // keep settings group comment
      { "matcher": "Edit", "hooks": [{ "type": "command", "command": "settings-user" }] }
    ],
    "showHookOutput": true,
  },
}\n`;
  const fixture = await createFixture({ hooks, settings });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const second = await runPlugin(fixture, 'install', {
      agentName: 'droid',
      agentId: 'agent-1',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    assert.equal(second.code, 0, second.stderr);
    const hooksRaw = await readFile(fixture.configPath, 'utf-8');
    const settingsRaw = await readFile(fixture.settingsPath, 'utf-8');
    const root = parseJsonc(hooksRaw);
    const userSettings = parseJsonc(settingsRaw);
    assert.match(hooksRaw, /root hook source/);
    assert.match(hooksRaw, /keep root group comment/);
    assert.match(settingsRaw, /general setting/);
    assert.match(settingsRaw, /settings fallback event/);
    assert.match(settingsRaw, /keep settings group comment/);
    assert.equal(root.PreToolUse.length, 2);
    assert.equal(root.PreToolUse[0].hooks[0].command, 'root-user');
    assert.equal(root.PostToolUse, undefined);
    assert.equal(userSettings.theme, 'dark');
    assert.equal(userSettings.hooks.PostToolUse.length, 2);
    assert.equal(userSettings.hooks.PostToolUse[0].hooks[0].command, 'settings-user');
    assert.equal(userSettings.hooks.PreToolUse, undefined);
    assert.equal(userSettings.hooks.showHookOutput, true);
    for (const [groups, scriptPath] of [
      [root.PreToolUse, fixture.guardScriptPath],
      [userSettings.hooks.PostToolUse, fixture.hookScriptPath],
    ]) {
      const handler = managedHandler(groups, scriptPath);
      assert.deepEqual(Object.keys(handler).sort(), ['command', 'timeout', 'type']);
      assert.equal(handler.timeout, 10);
      assert.equal(handler.type, 'command');
    }
    await assert.rejects(
      readFile(path.join(fixture.workspaceDir, '.factory', 'hooks.json')),
      { code: 'ENOENT' },
    );
  } finally {
    await fixture.close();
  }
});

test('Droid keeps an active legacy source until Factory migrates it', async () => {
  const legacyHooks = {
    PreToolUse: [{ matcher: 'Read', hooks: [{ type: 'command', command: 'legacy-user' }] }],
  };
  const settings = {
    hooks: {
      PostToolUse: [{ matcher: 'Edit', hooks: [{ type: 'command', command: 'settings-user' }] }],
    },
  };
  const fixture = await createFixture({ legacyHooks, settings });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
    const legacy = parseJsonc(await readFile(fixture.legacyConfigPath, 'utf-8'));
    const currentSettings = parseJsonc(await readFile(fixture.settingsPath, 'utf-8'));
    assert(managedHandler(legacy.PreToolUse, fixture.guardScriptPath));
    assert(managedHandler(currentSettings.hooks.PostToolUse, fixture.hookScriptPath));
  } finally {
    await fixture.close();
  }
});

test('Droid reuses an existing settings hook container', async () => {
  const settingsSource = '{\r\n\t"owner": "user",\r\n\t"hooks": {}\r\n}\r\n';
  const fixture = await createFixture({ settings: settingsSource });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
    const settingsRaw = await readFile(fixture.settingsPath, 'utf-8');
    const settings = parseJsonc(settingsRaw);
    assert.match(settingsRaw, /\r\n\t\t"PreToolUse"/);
    assert.match(settingsRaw, /\r\n\t\t"PostToolUse"/);
    assert.equal(settings.owner, 'user');
    assert(managedHandler(settings.hooks.PreToolUse, fixture.guardScriptPath));
    assert(managedHandler(settings.hooks.PostToolUse, fixture.hookScriptPath));
  } finally {
    await fixture.close();
  }
});

test('Droid commands block freezes and forward official input byte-for-byte', async () => {
  const capturePath = path.join(os.tmpdir(), `elydora-droid-event-${process.pid}-${Date.now()}.json`);
  const hookSource = `
    const fs = require('node:fs');
    const chunks = [];
    process.stdin.on('data', (chunk) => chunks.push(chunk));
    process.stdin.on('end', () => fs.writeFileSync(${JSON.stringify(capturePath)}, Buffer.concat(chunks)));
  `;
  const fixture = await createFixture({ hookSource });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const root = parseJsonc(await readFile(fixture.configPath, 'utf-8'));
    const guard = managedHandler(root.PreToolUse, fixture.guardScriptPath);
    const audit = managedHandler(root.PostToolUse, fixture.hookScriptPath);
    const prePayload = JSON.stringify({
      session_id: 'session-1',
      transcript_path: path.join(fixture.homeDir, 'transcript.jsonl'),
      cwd: fixture.workspaceDir,
      permission_mode: 'auto-high',
      hook_event_name: 'PreToolUse',
      tool_name: 'Execute',
      tool_input: { command: 'echo test' },
    });
    const guardResult = await runCommand(guard.command, prePayload);
    assert.equal(guardResult.code, 2);
    assert.match(guardResult.stderr, /Agent is frozen by Elydora/);
    const postPayload = JSON.stringify({
      ...JSON.parse(prePayload),
      hook_event_name: 'PostToolUse',
      tool_response: { output: 'test', success: true },
    });
    const auditResult = await runCommand(audit.command, postPayload);
    assert.equal(auditResult.code, 0, auditResult.stderr);
    assert.equal(await readFile(capturePath, 'utf-8'), postPayload);
  } finally {
    await fixture.close();
    await rm(capturePath, { force: true });
  }
});

test('Droid status requires an effective pair and both runtime files', async () => {
  const fixture = await createFixture({
    hooks: { PreToolUse: [] },
    settings: { hooks: { PostToolUse: [] } },
  });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);
    await rm(fixture.hookScriptPath);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);
    const root = parseJsonc(await readFile(fixture.configPath, 'utf-8'));
    root.hooksDisabled = true;
    await writeFile(fixture.configPath, `${JSON.stringify(root, null, 2)}\n`);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).hookConfigured, false);
  } finally {
    await fixture.close();
  }
});

test('Droid uninstall removes exact ownership and preserves user sources', async () => {
  const hooks = `{
  // keep root comment
  "PreToolUse": [{ "matcher": "Read", "hooks": [{ "type": "command", "command": "root-user" }] }]
}\n`;
  const settings = `{
  "theme": "dark",
  "hooks": {
    // keep settings comment
    "PostToolUse": [{ "matcher": "Edit", "hooks": [{ "type": "command", "command": "settings-user" }] }]
  }
}\n`;
  const fixture = await createFixture({ hooks, settings });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const uninstallId = process.platform === 'win32' ? 'AGENT-1' : 'agent-1';
    const result = await runPlugin(fixture, 'uninstall', uninstallId);
    assert.equal(result.code, 0, result.stderr);
    const hooksRaw = await readFile(fixture.configPath, 'utf-8');
    const settingsRaw = await readFile(fixture.settingsPath, 'utf-8');
    const root = parseJsonc(hooksRaw);
    const currentSettings = parseJsonc(settingsRaw);
    assert.match(hooksRaw, /keep root comment/);
    assert.match(settingsRaw, /keep settings comment/);
    assert.deepEqual(root.PreToolUse, [{
      matcher: 'Read',
      hooks: [{ type: 'command', command: 'root-user' }],
    }]);
    assert.deepEqual(currentSettings.hooks.PostToolUse, [{
      matcher: 'Edit',
      hooks: [{ type: 'command', command: 'settings-user' }],
    }]);
    assert.equal(currentSettings.theme, 'dark');
  } finally {
    await fixture.close();
  }
});

test('Droid uninstall deletes only a hooks file marked as Elydora-owned', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    assert.match(await readFile(fixture.configPath, 'utf-8'), /Managed by Elydora/);
    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Droid uninstall preserves mixed groups and lookalike commands', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const root = parseJsonc(await readFile(fixture.configPath, 'utf-8'));
    const managedGroup = root.PreToolUse.find(
      (group) => managedHandler([group], fixture.guardScriptPath),
    );
    const command = managedGroup.hooks[0].command;
    managedGroup.hooks.push({ type: 'command', command: 'user-command' });
    root.PreToolUse.push({
      matcher: '*',
      hooks: [{ type: 'command', command: command.replace('guard.js', 'guard.js.backup'), timeout: 10 }],
    });
    root.PreToolUse.push({
      matcher: '*',
      hooks: [{ type: 'command', command: command.replace('agent-1', 'agent-10'), timeout: 10 }],
    });
    await writeFile(fixture.configPath, `${JSON.stringify(root, null, 2)}\n`);
    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    const remaining = parseJsonc(await readFile(fixture.configPath, 'utf-8'));
    assert(remaining.PreToolUse.some(
      (group) => group.hooks.some((handler) => handler.command === 'user-command'),
    ));
    assert.match(JSON.stringify(remaining), /guard\.js\.backup/);
    assert.match(JSON.stringify(remaining), /agent-10/);
    assert.equal(remaining.PostToolUse, undefined);
  } finally {
    await fixture.close();
  }
});

test('Droid preserves every malformed source before the first write', async () => {
  const cases = [
    { hooks: '{ malformed', target: 'configPath' },
    { hooks: '[]', target: 'configPath' },
    { hooks: '{ "PreToolUse": [], "PreToolUse": [] }', target: 'configPath' },
    { hooks: { PreToolUse: null }, target: 'configPath' },
    { hooks: { PreToolUse: [null] }, target: 'configPath' },
    { hooks: { PreToolUse: [{ matcher: '[', hooks: [] }] }, target: 'configPath' },
    { hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 1 }] }] }, target: 'configPath' },
    { legacyHooks: '{ malformed', target: 'legacyConfigPath' },
    { hooks: { PreToolUse: [] }, settings: '{ malformed', target: 'settingsPath' },
    { settings: { hooks: null }, target: 'settingsPath' },
  ];
  for (const input of cases) {
    const fixture = await createFixture(input);
    try {
      assert.equal(fixture.installResult.code, 1, fixture.installResult.stderr);
      assert.equal(
        await readFile(fixture[input.target], 'utf-8'),
        serialize(input[input.target === 'legacyConfigPath' ? 'legacyHooks'
          : input.target === 'settingsPath' ? 'settings' : 'hooks']),
      );
    } finally {
      await fixture.close();
    }
  }
});

test('Droid rejects missing runtimes before creating a hook source', async () => {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-droid-missing-'));
  const workspaceDir = path.join(homeDir, 'workspace');
  const fixture = { homeDir, workspaceDir };
  await mkdir(workspaceDir);
  try {
    const result = await runPlugin(fixture, 'install', {
      agentName: 'droid',
      agentId: 'agent-1',
      guardScriptPath: path.join(homeDir, '.elydora', 'agent-1', 'guard.js'),
      hookScriptPath: path.join(homeDir, '.elydora', 'agent-1', 'hook.js'),
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /runtime is missing/i);
    await assert.rejects(
      readFile(path.join(homeDir, '.factory', 'hooks.json')),
      { code: 'ENOENT' },
    );
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('Droid atomic transactions leave no staging files', async () => {
  const fixture = await createFixture({
    hooks: { PreToolUse: [] },
    settings: { hooks: { PostToolUse: [] } },
  });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const names = await readdir(fixture.factoryDir, { recursive: true });
    assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false);
  } finally {
    await fixture.close();
  }
});
