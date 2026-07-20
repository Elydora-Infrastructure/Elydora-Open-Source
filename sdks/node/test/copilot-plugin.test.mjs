import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/copilot.js')).href;
const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;

function runNode(args, env, cwd, input = '') {
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

function runHook(handler, input) {
  const command = process.platform === 'win32'
    ? ['powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', handler.powershell]]
    : ['/bin/sh', ['-c', handler.bash]];
  return new Promise((resolve, reject) => {
    const child = spawn(command[0], command[1], { stdio: ['pipe', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.once('error', reject);
    child.once('close', (code) => resolve({ code, stdout, stderr }));
    child.stdin.end(input);
  });
}

async function writeJson(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, typeof value === 'string' ? value : JSON.stringify(value, null, 2));
}

async function runPlugin(fixture, method, argument) {
  const source = `
    import { copilotPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await copilotPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      COPILOT_HOME: fixture.copilotHome,
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    },
    fixture.projectDir,
  );
}

async function createFixture({
  userConfig,
  legacyConfig,
  guardSource,
  hookSource,
  createRuntimes = true,
} = {}) {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-copilot-'));
  const projectDir = path.join(homeDir, 'project');
  const copilotHome = path.join(homeDir, 'custom-copilot');
  const configPath = path.join(copilotHome, 'hooks', 'elydora-audit.json');
  const legacyPath = path.join(projectDir, '.github', 'hooks', 'hooks.json');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(projectDir, { recursive: true });
  if (createRuntimes) {
    await mkdir(agentDir, { recursive: true });
    await writeFile(
      guardScriptPath,
      guardSource ?? "process.stdin.resume(); process.stdin.once('end', () => { process.stderr.write('Agent is frozen by Elydora.'); process.exit(2); });\n",
    );
    await writeFile(hookScriptPath, hookSource ?? 'process.stdin.resume();\n');
    await writeFile(path.join(agentDir, 'config.json'), JSON.stringify({
      agent_id: 'agent-1',
      agent_name: 'copilot',
    }));
  }
  if (userConfig !== undefined) await writeJson(configPath, userConfig);
  if (legacyConfig !== undefined) await writeJson(legacyPath, legacyConfig);
  return {
    agentDir,
    configPath,
    copilotHome,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    legacyPath,
    projectDir,
    async install() {
      return runPlugin(this, 'install', {
        agentName: 'copilot',
        agentId: 'agent-1',
        guardScriptPath,
        hookScriptPath,
      });
    },
    async close() {
      await rm(homeDir, { recursive: true, force: true });
    },
  };
}

function managedHandler(config, event, scriptName) {
  return config.hooks?.[event]?.find(
    (handler) => handler.bash?.includes(scriptName) || handler.powershell?.includes(scriptName),
  );
}

function assertNativeHandler(handler) {
  assert.deepEqual(Object.keys(handler).sort(), ['bash', 'powershell', 'timeoutSec', 'type']);
  assert.equal(handler.type, 'command');
  assert.equal(handler.timeoutSec, 10);
  assert.match(handler.bash, /node(?:\.exe)?/i);
  assert.match(handler.powershell, /^& /);
}

function legacyManagedConfig(fixture, extraHooks = {}) {
  return {
    version: 1,
    hooks: {
      preToolUse: [{
        type: 'command',
        bash: `node "${fixture.guardScriptPath}"`,
        powershell: `node "${fixture.guardScriptPath}"`,
        timeoutSec: 5,
      }],
      postToolUse: [{
        type: 'command',
        bash: `node "${fixture.hookScriptPath}"`,
        powershell: `node "${fixture.hookScriptPath}"`,
        timeoutSec: 5,
      }],
      ...extraHooks,
    },
  };
}

test('GitHub Copilot CLI is registered with the native user hook file', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('copilot'), {
    name: 'GitHub Copilot CLI',
    configDir: '~/.copilot/hooks',
    configFile: 'elydora-audit.json',
  });
});

test('Copilot install preserves user hooks, migrates legacy entries, and is idempotent', async () => {
  const fixture = await createFixture({
    userConfig: {
      version: 1,
      disableAllHooks: false,
      hooks: {
        sessionStart: [{ type: 'command', command: 'user-session-hook' }],
        preToolUse: [{ type: 'command', command: 'user-pre-hook' }],
      },
    },
  });
  try {
    await writeJson(fixture.legacyPath, legacyManagedConfig(fixture, {
      notification: [{ type: 'command', command: 'user-notification-hook' }],
    }));

    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);

    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(config.version, 1);
    assert.equal(config.disableAllHooks, false);
    assert.deepEqual(config.hooks.sessionStart, [{ type: 'command', command: 'user-session-hook' }]);
    assert.deepEqual(config.hooks.preToolUse[0], { type: 'command', command: 'user-pre-hook' });
    assert.equal(config.hooks.preToolUse.length, 2);
    assert.equal(config.hooks.postToolUse.length, 1);
    assertNativeHandler(managedHandler(config, 'preToolUse', 'guard.js'));
    assertNativeHandler(managedHandler(config, 'postToolUse', 'hook.js'));

    const legacy = JSON.parse(await readFile(fixture.legacyPath, 'utf-8'));
    assert.deepEqual(legacy, {
      version: 1,
      hooks: {
        notification: [{ type: 'command', command: 'user-notification-hook' }],
      },
    });
  } finally {
    await fixture.close();
  }
});

test('Copilot migration removes a legacy file owned entirely by Elydora', async () => {
  const fixture = await createFixture();
  try {
    await writeJson(fixture.legacyPath, legacyManagedConfig(fixture));
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    await assert.rejects(readFile(fixture.legacyPath), { code: 'ENOENT' });
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assertNativeHandler(managedHandler(config, 'preToolUse', 'guard.js'));
  } finally {
    await fixture.close();
  }
});

test('Copilot commands block freezes and forward the native payload byte-for-byte', async () => {
  const capturePath = path.join(os.tmpdir(), `elydora-copilot-event-${process.pid}-${Date.now()}.json`);
  const fixture = await createFixture({
    hookSource: `
      const fs = require('node:fs');
      const chunks = [];
      process.stdin.on('data', (chunk) => chunks.push(chunk));
      process.stdin.on('end', () => fs.writeFileSync(${JSON.stringify(capturePath)}, Buffer.concat(chunks)));
    `,
  });
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    const guard = managedHandler(config, 'preToolUse', 'guard.js');
    const audit = managedHandler(config, 'postToolUse', 'hook.js');
    const prePayload = JSON.stringify({
      sessionId: 'session-1',
      timestamp: Date.now(),
      cwd: fixture.projectDir,
      toolName: 'powershell',
      toolArgs: { command: 'Get-ChildItem' },
    });
    const guardResult = await runHook(guard, prePayload);
    assert.equal(guardResult.code, 2, guardResult.stderr);
    assert.match(guardResult.stderr, /Agent is frozen by Elydora/);

    const postPayload = JSON.stringify({
      sessionId: 'session-1',
      timestamp: Date.now(),
      cwd: fixture.projectDir,
      toolName: 'powershell',
      toolArgs: { command: 'Get-ChildItem' },
      toolResult: { output: 'ok' },
    });
    const auditResult = await runHook(audit, postPayload);
    assert.equal(auditResult.code, 0, auditResult.stderr);
    assert.equal(await readFile(capturePath, 'utf-8'), postPayload);
  } finally {
    await fixture.close();
    await rm(capturePath, { force: true });
  }
});

test('Copilot treats an empty home override as the official default', async () => {
  const fixture = await createFixture();
  try {
    fixture.copilotHome = '';
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    const defaultPath = path.join(fixture.homeDir, '.copilot', 'hooks', 'elydora-audit.json');
    const config = JSON.parse(await readFile(defaultPath, 'utf-8'));
    assertNativeHandler(managedHandler(config, 'preToolUse', 'guard.js'));
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).configPath, defaultPath);
  } finally {
    await fixture.close();
  }
});

test('Copilot status requires a complete pair, matching runtime identity, and both scripts', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);

    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    delete config.hooks.postToolUse;
    await writeJson(fixture.configPath, config);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).hookConfigured, false);

    assert.equal((await fixture.install()).code, 0);
    await writeFile(path.join(fixture.agentDir, 'config.json'), '{ malformed');
    status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /parse Elydora runtime config/i);
  } finally {
    await fixture.close();
  }
});

test('Copilot uninstall removes exact ownership and preserves user entries', async () => {
  const fixture = await createFixture({
    userConfig: { version: 1, hooks: { notification: [{ type: 'command', command: 'keep' }] } },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    const config = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    config.hooks.preToolUse.push({
      type: 'command',
      bash: `'${process.execPath}' '${path.join(fixture.homeDir, '.elydora', 'agent-10', 'guard.js')}'`,
      powershell: 'user-decoy',
      timeoutSec: 10,
    });
    await writeJson(fixture.configPath, config);

    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    const remaining = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.deepEqual(remaining.hooks.notification, [{ type: 'command', command: 'keep' }]);
    assert.equal(remaining.hooks.postToolUse, undefined);
    assert.equal(remaining.hooks.preToolUse.length, 1);
    assert.match(remaining.hooks.preToolUse[0].bash, /agent-10/);
  } finally {
    await fixture.close();
  }
});

test('Copilot uninstall leaves absent hook sources absent', async () => {
  const fixture = await createFixture();
  try {
    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
    await assert.rejects(readFile(fixture.legacyPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Copilot preserves malformed and invalid configs for recovery', async () => {
  for (const existing of [
    '{ malformed',
    { hooks: {} },
    { version: 2, hooks: {} },
    { version: 1, hooks: null },
    { version: 1, hooks: { preToolUse: null } },
    { version: 1, hooks: { preToolUse: [null] } },
  ]) {
    const fixture = await createFixture({ userConfig: existing });
    try {
      const before = await readFile(fixture.configPath, 'utf-8');
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.equal(await readFile(fixture.configPath, 'utf-8'), before);
    } finally {
      await fixture.close();
    }
  }
});

test('Copilot rejects missing runtimes before creating hook config', async () => {
  const fixture = await createFixture({ createRuntimes: false });
  try {
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /runtime is missing/i);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Copilot atomic writes leave no transaction files', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const files = await readdir(path.dirname(fixture.configPath));
    assert.equal(files.some((name) => name.endsWith('.tmp') || name.endsWith('.rollback')), false);
  } finally {
    await fixture.close();
  }
});
