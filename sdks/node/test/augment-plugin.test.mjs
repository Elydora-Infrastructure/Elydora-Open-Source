import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/augment.js')).href;
const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
const cliPath = path.resolve('dist/cli.js');
const wrapperExtension = process.platform === 'win32' ? '.cmd' : '.sh';
const guardWrapperName = `augment-guard${wrapperExtension}`;
const auditWrapperName = `augment-hook${wrapperExtension}`;

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

function quotePosix(value) {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quoteWindows(value) {
  return `"${value.replaceAll('"', '\\"')}"`;
}

function generatedCommand(wrapperPath) {
  return process.platform === 'win32' ? quoteWindows(wrapperPath) : quotePosix(wrapperPath);
}

async function runPlugin(fixture, method, argument) {
  const script = `
    import { augmentPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await augmentPlugin[process.env.ELYDORA_TEST_METHOD](argument);
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

async function createFixture({ existingSettings, guardSource, hookSource } = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-augment-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote");
  const workspaceDir = path.join(homeDir, 'workspace');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const configPath = path.join(homeDir, '.augment', 'settings.json');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  const guardWrapperPath = path.join(agentDir, guardWrapperName);
  const auditWrapperPath = path.join(agentDir, auditWrapperName);
  await mkdir(workspaceDir, { recursive: true });
  await mkdir(agentDir, { recursive: true });
  await writeFile(
    guardScriptPath,
    guardSource ?? "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n",
  );
  await writeFile(hookScriptPath, hookSource ?? 'process.exit(0);\n');
  await writeFile(path.join(agentDir, 'config.json'), JSON.stringify({
    agent_id: 'agent-1',
    agent_name: 'augment',
  }));
  if (existingSettings !== undefined) {
    await mkdir(path.dirname(configPath), { recursive: true });
    await writeFile(configPath, typeof existingSettings === 'string'
      ? existingSettings
      : JSON.stringify(existingSettings, null, 2));
  }
  const fixture = {
    agentDir,
    auditWrapperPath,
    configPath,
    guardScriptPath,
    guardWrapperPath,
    homeDir,
    hookScriptPath,
    workspaceDir,
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
  fixture.installResult = await runPlugin(fixture, 'install', {
    agentName: 'augment',
    agentId: 'agent-1',
    baseUrl: 'https://api.elydora.com',
    guardScriptPath,
    hookScriptPath,
  });
  return fixture;
}

function managedHandler(settings, event, wrapperPath) {
  const command = generatedCommand(wrapperPath);
  for (const group of settings.hooks?.[event] ?? []) {
    const handler = group.hooks.find((candidate) => candidate.command === command);
    if (handler) return handler;
  }
  return undefined;
}

test('Augment Code CLI is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('augment'), {
    name: 'Augment Code CLI',
    configDir: '~/.augment',
    configFile: 'settings.json',
  });
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-augment-cli-'));
  const workspaceDir = path.join(homeDir, 'workspace');
  await mkdir(workspaceDir);
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: homeDir,
      USERPROFILE: homeDir,
    }, '', workspaceDir);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Augment Code CLI \(augment\)/);
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('Auggie install preserves user settings and is idempotent', async () => {
  const existing = {
    telemetryEnabled: false,
    hooks: {
      SessionStart: [{
        hooks: [{ type: 'command', command: 'existing-command', args: ['one'], timeout: 5_000 }],
        metadata: { includeUserContext: true },
        label: 'keep group metadata',
      }],
      PreToolUse: [{
        matcher: 'launch-process',
        hooks: [{ type: 'command', command: 'user-command' }],
      }],
    },
  };
  const fixture = await createFixture({ existingSettings: existing });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const second = await runPlugin(fixture, 'install', {
      agentName: 'augment',
      agentId: 'agent-1',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    assert.equal(second.code, 0, second.stderr);
    const settings = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(settings.telemetryEnabled, false);
    assert.deepEqual(settings.hooks.SessionStart, existing.hooks.SessionStart);
    assert.deepEqual(settings.hooks.PreToolUse[0], existing.hooks.PreToolUse[0]);
    assert.equal(settings.hooks.PreToolUse.length, 2);
    assert.equal(settings.hooks.PostToolUse.length, 1);
    assert.equal(settings.hooks.PreToolUse[1].matcher, '.*');
    for (const [event, wrapperPath] of [
      ['PreToolUse', fixture.guardWrapperPath],
      ['PostToolUse', fixture.auditWrapperPath],
    ]) {
      const handler = managedHandler(settings, event, wrapperPath);
      assert.deepEqual(Object.keys(handler).sort(), ['command', 'timeout', 'type']);
      assert.equal(handler.type, 'command');
      assert.equal(handler.timeout, 10_000);
    }
    const guardWrapper = await readFile(fixture.guardWrapperPath, 'utf-8');
    const auditWrapper = await readFile(fixture.auditWrapperPath, 'utf-8');
    assert.match(guardWrapper, new RegExp(path.basename(fixture.guardScriptPath).replace('.', '\\.')));
    assert.match(auditWrapper, new RegExp(path.basename(fixture.hookScriptPath).replace('.', '\\.')));
    if (process.platform !== 'win32') {
      assert.equal((await stat(fixture.guardWrapperPath)).mode & 0o111, 0o100);
      assert.match(guardWrapper, /^#!\/bin\/sh\nexec /);
    } else {
      assert.match(guardWrapper, /^@echo off\r?\n/);
      assert.match(guardWrapper, /exit \/b %errorlevel%/);
    }
    await assert.rejects(
      readFile(path.join(fixture.workspaceDir, '.augment', 'settings.json')),
      { code: 'ENOENT' },
    );
    await assert.rejects(
      readFile(path.join(fixture.workspaceDir, '.augment', 'settings.local.json')),
      { code: 'ENOENT' },
    );
  } finally {
    await fixture.close();
  }
});

test('Auggie wrappers block freezes and forward official input byte-for-byte', async () => {
  const capturePath = path.join(os.tmpdir(), `elydora-augment-event-${process.pid}-${Date.now()}.json`);
  const hookSource = `
    const fs = require('node:fs');
    const chunks = [];
    process.stdin.on('data', (chunk) => chunks.push(chunk));
    process.stdin.on('end', () => fs.writeFileSync(${JSON.stringify(capturePath)}, Buffer.concat(chunks)));
  `;
  const fixture = await createFixture({ hookSource });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const settings = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const guard = managedHandler(settings, 'PreToolUse', fixture.guardWrapperPath);
    const audit = managedHandler(settings, 'PostToolUse', fixture.auditWrapperPath);
    const prePayload = JSON.stringify({
      hook_event_name: 'PreToolUse',
      conversation_id: 'conversation-1',
      workspace_roots: [fixture.workspaceDir],
      tool_name: 'launch-process',
      tool_input: { command: 'echo test' },
      is_mcp_tool: false,
    });
    const guardResult = await runCommand(guard.command, prePayload);
    assert.equal(guardResult.code, 2);
    assert.match(guardResult.stderr, /Agent is frozen by Elydora/);
    const postPayload = JSON.stringify({
      hook_event_name: 'PostToolUse',
      conversation_id: 'conversation-1',
      workspace_roots: [fixture.workspaceDir],
      tool_name: 'launch-process',
      tool_input: { command: 'echo test' },
      tool_output: 'test',
      is_mcp_tool: false,
    });
    const auditResult = await runCommand(audit.command, postPayload);
    assert.equal(auditResult.code, 0, auditResult.stderr);
    assert.equal(await readFile(capturePath, 'utf-8'), postPayload);
  } finally {
    await fixture.close();
    await rm(capturePath, { force: true });
  }
});

test('Auggie status requires a complete pair, core runtimes, and wrappers', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, true);
    await rm(fixture.guardWrapperPath);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);
    await runPlugin(fixture, 'install', {
      agentName: 'augment',
      agentId: 'agent-1',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    await rm(fixture.hookScriptPath);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);
  } finally {
    await fixture.close();
  }
});

test('Auggie uninstall removes exact ownership and preserves user groups', async () => {
  const fixture = await createFixture({ existingSettings: {
    owner: 'user',
    hooks: { Notification: [] },
  } });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const settings = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    settings.hooks.PreToolUse.unshift({ hooks: [], label: 'keep empty group' });
    settings.hooks.PreToolUse[1].hooks.push({ type: 'command', command: 'user-command' });
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
    await writeFile(fixture.configPath, JSON.stringify(settings, null, 2));
    const uninstallId = process.platform === 'win32' ? 'AGENT-1' : 'agent-1';
    const result = await runPlugin(fixture, 'uninstall', uninstallId);
    assert.equal(result.code, 0, result.stderr);
    const remaining = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(remaining.owner, 'user');
    assert.deepEqual(remaining.hooks.Notification, []);
    assert.equal(remaining.hooks.PreToolUse.length, 4);
    assert.deepEqual(remaining.hooks.PreToolUse[0], { hooks: [], label: 'keep empty group' });
    assert.equal(remaining.hooks.PreToolUse[1].hooks[0].command, 'user-command');
    assert.match(JSON.stringify(remaining), /augment-guard.*backup/);
    assert.match(JSON.stringify(remaining), /agent-10/);
    assert.equal(remaining.hooks.PostToolUse, undefined);
  } finally {
    await fixture.close();
  }
});

test('Auggie install replaces stale Elydora handlers and preserves empty groups', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const settings = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
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
    await writeFile(fixture.configPath, JSON.stringify(settings, null, 2));
    const result = await runPlugin(fixture, 'install', {
      agentName: 'augment',
      agentId: 'agent-1',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    assert.equal(result.code, 0, result.stderr);
    const current = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.doesNotMatch(JSON.stringify(current), /agent-old/);
    assert.deepEqual(current.hooks.PreToolUse[0], { hooks: [], label: 'keep empty group' });
    assert.equal(current.hooks.PreToolUse.length, 2);
    assert.equal(current.hooks.PostToolUse.length, 1);
  } finally {
    await fixture.close();
  }
});

test('Auggie uninstall removes settings owned entirely by Elydora', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Auggie rejects malformed settings and invalid hook contracts before writes', async () => {
  const cases = [
    '{ malformed',
    'null',
    '[]',
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
  for (const existingSettings of cases) {
    const fixture = await createFixture({ existingSettings });
    try {
      assert.equal(fixture.installResult.code, 1, `${JSON.stringify(existingSettings)}\n${fixture.installResult.stderr}`);
      const expected = typeof existingSettings === 'string'
        ? existingSettings
        : JSON.stringify(existingSettings, null, 2);
      assert.equal(await readFile(fixture.configPath, 'utf-8'), expected);
      await assert.rejects(readFile(fixture.guardWrapperPath), { code: 'ENOENT' });
      await assert.rejects(readFile(fixture.auditWrapperPath), { code: 'ENOENT' });
    } finally {
      await fixture.close();
    }
  }
});

test('Auggie install rejects missing runtimes before creating files', async () => {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-augment-missing-'));
  const workspaceDir = path.join(homeDir, 'workspace');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const fixture = { homeDir, workspaceDir };
  await mkdir(workspaceDir);
  try {
    const result = await runPlugin(fixture, 'install', {
      agentName: 'augment',
      agentId: 'agent-1',
      guardScriptPath: path.join(agentDir, 'guard.js'),
      hookScriptPath: path.join(agentDir, 'hook.js'),
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /runtime is missing/i);
    await assert.rejects(readFile(path.join(homeDir, '.augment', 'settings.json')), { code: 'ENOENT' });
    await assert.rejects(readFile(path.join(agentDir, guardWrapperName)), { code: 'ENOENT' });
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('Auggie status surfaces malformed referenced runtime metadata', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    await writeFile(path.join(fixture.agentDir, 'config.json'), '{ malformed');
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /parse Elydora runtime config/i);
  } finally {
    await fixture.close();
  }
});

test('Auggie atomic writes leave no temporary files', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    for (const directory of [fixture.agentDir, path.dirname(fixture.configPath)]) {
      assert.equal((await readdir(directory)).some((name) => name.endsWith('.tmp')), false);
    }
  } finally {
    await fixture.close();
  }
});
