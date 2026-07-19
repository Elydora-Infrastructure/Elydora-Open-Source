import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/grok.js')).href;
const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
const cliPath = path.resolve('dist/cli.js');

function runNode(args, env, input = '', unset = [], cwd) {
  return new Promise((resolve, reject) => {
    const childEnv = { ...process.env, ...env };
    for (const key of unset) delete childEnv[key];
    const child = spawn(process.execPath, args, {
      cwd,
      env: childEnv,
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

function buildCommand(scriptPath) {
  const quote = process.platform === 'win32' ? quoteWindows : quotePosix;
  return `${quote(process.execPath)} ${quote(scriptPath)}`;
}

async function runPlugin(fixture, method, argument) {
  const script = `
    import { grokPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await grokPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  const env = {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
    ELYDORA_TEST_METHOD: method,
  };
  if (fixture.explicitGrokHome) env.GROK_HOME = fixture.grokHome;
  return runNode(
    ['--input-type=module', '--eval', script],
    env,
    '',
    fixture.explicitGrokHome ? [] : ['GROK_HOME'],
    fixture.homeDir,
  );
}

async function createFixture({
  config,
  explicitGrokHome = true,
  guardSource,
  hookSource,
} = {}) {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-grok-'));
  const grokHome = explicitGrokHome ? path.join(homeDir, 'custom-grok') : path.join(homeDir, '.grok');
  const configPath = path.join(grokHome, 'hooks', 'elydora-audit.json');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(agentDir, { recursive: true });
  await writeFile(
    guardScriptPath,
    guardSource ?? "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n",
  );
  await writeFile(hookScriptPath, hookSource ?? 'process.exit(0);\n');
  await writeFile(path.join(agentDir, 'config.json'), JSON.stringify({
    agent_id: 'agent-1',
    agent_name: 'grok',
  }));
  if (config !== undefined) {
    await mkdir(path.dirname(configPath), { recursive: true });
    await writeFile(configPath, typeof config === 'string' ? config : JSON.stringify(config, null, 2));
  }
  const fixture = {
    agentDir,
    configPath,
    explicitGrokHome,
    grokHome,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    async close() {
      await rm(homeDir, { recursive: true, force: true });
    },
  };
  fixture.installResult = await runPlugin(fixture, 'install', {
    agentName: 'grok',
    agentId: 'agent-1',
    baseUrl: 'https://api.elydora.com',
    guardScriptPath,
    hookScriptPath,
  });
  return fixture;
}

function managedHandler(config, event, scriptName) {
  for (const group of config.hooks?.[event] ?? []) {
    if (Object.hasOwn(group, 'matcher')) continue;
    const handler = group.hooks.find((candidate) => candidate.command?.includes(scriptName));
    if (handler) return handler;
  }
  return undefined;
}

function assertStrictHandler(handler) {
  assert.deepEqual(Object.keys(handler).sort(), ['command', 'timeout', 'type']);
  assert.equal(handler.type, 'command');
  assert.equal(handler.timeout, 10);
}

test('Grok Build is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('grok'), {
    name: 'Grok Build',
    configDir: '~/.grok/hooks',
    configFile: 'elydora-audit.json',
  });
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-grok-cli-'));
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: homeDir,
      USERPROFILE: homeDir,
      GROK_HOME: path.join(homeDir, 'custom-grok'),
    }, '', [], homeDir);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Grok Build \(grok\)/);
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('Grok install preserves native user config and is idempotent', async () => {
  const existing = {
    schemaVersion: 1,
    hooks: {
      SessionStart: [{
        matcher: 'startup',
        hooks: [{ type: 'http', url: 'https://example.test/hook', timeout: 5, headers: { x: 'keep' } }],
        label: 'keep group metadata',
      }],
      PreToolUse: [{
        matcher: 'Bash',
        hooks: [{ type: 'command', command: 'existing-command', timeout: 5 }],
      }],
    },
  };
  const fixture = await createFixture({ config: existing });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    assert.match(fixture.installResult.stdout, /global PreToolUse and PostToolUse hooks installed/i);
    const second = await runPlugin(fixture, 'install', {
      agentName: 'grok',
      agentId: 'agent-1',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    assert.equal(second.code, 0, second.stderr);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(config.schemaVersion, 1);
    assert.deepEqual(config.hooks.SessionStart, existing.hooks.SessionStart);
    assert.deepEqual(config.hooks.PreToolUse[0], existing.hooks.PreToolUse[0]);
    assert.equal(config.hooks.PreToolUse.length, 2);
    assert.equal(config.hooks.PostToolUse.length, 1);
    assert.deepEqual(Object.keys(config.hooks.PreToolUse[1]), ['hooks']);
    assertStrictHandler(managedHandler(config, 'PreToolUse', 'guard.js'));
    assertStrictHandler(managedHandler(config, 'PostToolUse', 'hook.js'));
    await assert.rejects(readFile(path.join(fixture.homeDir, '.grok', 'hooks', 'elydora-audit.json')), {
      code: 'ENOENT',
    });
    await assert.rejects(readFile(path.join(fixture.homeDir, '.claude', 'settings.json')), { code: 'ENOENT' });
    await assert.rejects(readFile(path.join(fixture.homeDir, '.cursor', 'hooks.json')), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Grok treats an empty home override as the official default', async () => {
  const fixture = await createFixture({ explicitGrokHome: false });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    fixture.explicitGrokHome = true;
    fixture.grokHome = '';
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);
  } finally {
    await fixture.close();
  }
});

test('Grok commands block freezes and forward the official payload byte-for-byte', async () => {
  const capturePath = path.join(os.tmpdir(), `elydora-grok-event-${process.pid}-${Date.now()}.json`);
  const hookSource = `
    const fs = require('node:fs');
    const chunks = [];
    process.stdin.on('data', (chunk) => chunks.push(chunk));
    process.stdin.on('end', () => fs.writeFileSync(${JSON.stringify(capturePath)}, Buffer.concat(chunks)));
  `;
  const fixture = await createFixture({ hookSource });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const guard = managedHandler(config, 'PreToolUse', 'guard.js');
    const audit = managedHandler(config, 'PostToolUse', 'hook.js');
    const prePayload = JSON.stringify({
      hookEventName: 'PreToolUse',
      sessionId: 'session-1',
      cwd: fixture.homeDir,
      workspaceRoot: fixture.homeDir,
      toolName: 'Bash',
      toolInput: { command: 'echo test' },
    });
    const guardResult = await runCommand(guard.command, prePayload);
    assert.equal(guardResult.code, 2);
    assert.match(guardResult.stderr, /Agent is frozen by Elydora/);
    const postPayload = JSON.stringify({
      hookEventName: 'PostToolUse',
      sessionId: 'session-1',
      cwd: fixture.homeDir,
      workspaceRoot: fixture.homeDir,
      toolName: 'Bash',
      toolInput: { command: 'echo test' },
      toolResult: { output: 'test' },
    });
    const auditResult = await runCommand(audit.command, postPayload);
    assert.equal(auditResult.code, 0, auditResult.stderr);
    assert.equal(await readFile(capturePath, 'utf-8'), postPayload);
  } finally {
    await fixture.close();
    await rm(capturePath, { force: true });
  }
});

test('Grok status requires a complete hook pair and both runtime files', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, true);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    delete config.hooks.PostToolUse;
    await writeFile(fixture.configPath, JSON.stringify(config, null, 2));
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).hookConfigured, false);
    await runPlugin(fixture, 'install', {
      agentName: 'grok',
      agentId: 'agent-1',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    await rm(fixture.guardScriptPath);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);
  } finally {
    await fixture.close();
  }
});

test('Grok uninstall removes exact ownership and preserves mixed user handlers', async () => {
  const fixture = await createFixture({ config: { owner: 'user', hooks: { Notification: [] } } });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    config.hooks.PreToolUse.at(-1).hooks.push({ type: 'command', command: 'user-command', timeout: 10 });
    config.hooks.PreToolUse.push({ hooks: [{
      type: 'command',
      command: buildCommand(path.join(fixture.agentDir, 'guard.js.backup')),
      timeout: 10,
    }] });
    config.hooks.PreToolUse.push({ hooks: [{
      type: 'command',
      command: buildCommand(path.join(fixture.homeDir, '.elydora', 'agent-10', 'guard.js')),
      timeout: 10,
    }] });
    await writeFile(fixture.configPath, JSON.stringify(config, null, 2));
    const uninstallId = process.platform === 'win32' ? 'AGENT-1' : 'agent-1';
    const result = await runPlugin(fixture, 'uninstall', uninstallId);
    assert.equal(result.code, 0, result.stderr);
    const remaining = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(remaining.owner, 'user');
    assert.equal(remaining.hooks.PreToolUse.length, 3);
    assert.equal(remaining.hooks.PreToolUse[0].hooks[0].command, 'user-command');
    assert(remaining.hooks.PreToolUse.some((group) => group.hooks[0].command.includes('guard.js.backup')));
    assert(remaining.hooks.PreToolUse.some((group) => group.hooks[0].command.includes('agent-10')));
    assert.equal(remaining.hooks.PostToolUse, undefined);
    assert.deepEqual(remaining.hooks.Notification, []);
  } finally {
    await fixture.close();
  }
});

test('Grok install replaces stale Elydora handlers for every agent', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    for (const [event, scriptName] of [['PreToolUse', 'guard.js'], ['PostToolUse', 'hook.js']]) {
      config.hooks[event].push({ hooks: [{
        type: 'command',
        command: buildCommand(path.join(fixture.homeDir, '.elydora', 'agent-old', scriptName)),
        timeout: 10,
      }] });
    }
    await writeFile(fixture.configPath, JSON.stringify(config, null, 2));
    const result = await runPlugin(fixture, 'install', {
      agentName: 'grok',
      agentId: 'agent-1',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    assert.equal(result.code, 0, result.stderr);
    const current = await readFile(fixture.configPath, 'utf-8');
    assert.doesNotMatch(current, /agent-old/);
    assert.equal(JSON.parse(current).hooks.PreToolUse.length, 1);
    assert.equal(JSON.parse(current).hooks.PostToolUse.length, 1);
  } finally {
    await fixture.close();
  }
});

test('Grok uninstall preserves an untouched empty native event', async () => {
  const fixture = await createFixture({ config: { owner: 'user' } });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    config.hooks.PreToolUse = [];
    await writeFile(fixture.configPath, JSON.stringify(config, null, 2));
    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    const remaining = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.deepEqual(remaining.hooks.PreToolUse, []);
    assert.equal(remaining.hooks.PostToolUse, undefined);
  } finally {
    await fixture.close();
  }
});

test('Grok uninstall removes a config owned entirely by Elydora', async () => {
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

test('Grok preserves malformed JSON for recovery', async () => {
  const fixture = await createFixture({ config: '{ malformed' });
  try {
    assert.equal(fixture.installResult.code, 1);
    assert.match(fixture.installResult.stderr, /parse Grok hooks config/i);
    assert.equal(await readFile(fixture.configPath, 'utf-8'), '{ malformed');
  } finally {
    await fixture.close();
  }
});

test('Grok rejects invalid native hook shapes before writing', async () => {
  const cases = [
    { hooks: null },
    { hooks: { PreToolUse: null } },
    { hooks: { PreToolUse: [null] } },
    { hooks: { PreToolUse: [{ matcher: 1, hooks: [] }] } },
    { hooks: { PreToolUse: [{ hooks: null }] } },
    { hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: '' }] }] } },
    { hooks: { PreToolUse: [{ hooks: [{ type: 'file', command: 'x' }] }] } },
    { hooks: { PreToolUse: [{ hooks: [{ type: 'http', url: '' }] }] } },
    { hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'x', timeout: 0 }] }] } },
  ];
  for (const existing of cases) {
    const fixture = await createFixture({ config: existing });
    try {
      assert.equal(fixture.installResult.code, 1);
      assert.deepEqual(JSON.parse(await readFile(fixture.configPath, 'utf-8')), existing);
    } finally {
      await fixture.close();
    }
  }
});

test('Grok install rejects missing runtimes before creating config', async () => {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-grok-missing-runtime-'));
  const fixture = {
    explicitGrokHome: true,
    grokHome: path.join(homeDir, 'custom-grok'),
    homeDir,
  };
  const configPath = path.join(fixture.grokHome, 'hooks', 'elydora-audit.json');
  try {
    const result = await runPlugin(fixture, 'install', {
      agentName: 'grok',
      agentId: 'agent-1',
      guardScriptPath: path.join(homeDir, '.elydora', 'agent-1', 'guard.js'),
      hookScriptPath: path.join(homeDir, '.elydora', 'agent-1', 'hook.js'),
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /runtime is missing/i);
    await assert.rejects(readFile(configPath), { code: 'ENOENT' });
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('Grok status surfaces malformed referenced runtime metadata', async () => {
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

test('Grok atomic writes leave no temporary files', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    assert.equal((await readdir(path.dirname(fixture.configPath))).some((name) => name.endsWith('.tmp')), false);
  } finally {
    await fixture.close();
  }
});
