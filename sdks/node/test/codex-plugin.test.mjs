import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/codex.js')).href;
const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;

function runNode(args, env, input = '') {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, args, {
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
    const child = spawn(command, {
      shell: true,
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

function findHandler(settings, event, statusMessage) {
  for (const group of settings.hooks?.[event] ?? []) {
    const handler = group.hooks?.find((item) => item.statusMessage === statusMessage);
    if (handler) return handler;
  }
  return undefined;
}

async function runPlugin(homeDir, method, argument) {
  const script = `
    import { codexPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await codexPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', script],
    {
      HOME: homeDir,
      USERPROFILE: homeDir,
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    },
  );
}

async function createFixture({ guardSource, hookSource, existingSettings } = {}) {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-codex-'));
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const configDir = path.join(homeDir, '.codex');
  await mkdir(agentDir, { recursive: true });
  await mkdir(configDir, { recursive: true });

  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await writeFile(
    guardScriptPath,
    guardSource ?? "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n",
  );
  await writeFile(hookScriptPath, hookSource ?? 'process.exit(0);\n');
  await writeFile(path.join(agentDir, 'config.json'), JSON.stringify({
    agent_id: 'agent-1',
    agent_name: 'codex',
  }));

  const configPath = path.join(configDir, 'hooks.json');
  if (existingSettings !== undefined) {
    await writeFile(configPath, typeof existingSettings === 'string'
      ? existingSettings
      : JSON.stringify(existingSettings, null, 2));
  }

  const installResult = await runPlugin(homeDir, 'install', {
    agentName: 'codex',
    agentId: 'agent-1',
    baseUrl: 'https://api.elydora.com',
    guardScriptPath,
    hookScriptPath,
  });

  return {
    agentDir,
    configPath,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    installResult,
    async close() {
      await rm(homeDir, { recursive: true, force: true });
    },
  };
}

test('Codex is registered as a native hook integration', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('codex'), {
    name: 'OpenAI Codex',
    configDir: '~/.codex',
    configFile: 'hooks.json',
  });
});

test('Codex install preserves existing hooks and is idempotent', async () => {
  const existing = {
    description: 'Workspace hooks',
    hooks: {
      SessionStart: [{ hooks: [{ type: 'command', command: 'existing-command' }] }],
    },
  };
  const fixture = await createFixture({ existingSettings: existing });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    assert.match(fixture.installResult.stdout, /run \/hooks to review and trust/i);
    const secondInstall = await runPlugin(fixture.homeDir, 'install', {
      agentName: 'codex',
      agentId: 'agent-1',
      baseUrl: 'https://api.elydora.com',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    assert.equal(secondInstall.code, 0, secondInstall.stderr);

    const settings = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(settings.description, 'Workspace hooks');
    assert.equal(settings.hooks.SessionStart[0].hooks[0].command, 'existing-command');
    assert.equal(settings.hooks.PreToolUse.length, 1);
    assert.equal(settings.hooks.PostToolUse.length, 1);
    assert.equal(settings.hooks.PreToolUse[0].matcher, '*');
    assert.equal(settings.hooks.PostToolUse[0].matcher, '*');
  } finally {
    await fixture.close();
  }
});

test('Codex hook commands enforce freezes and forward the official event payload', async () => {
  const capturePath = path.join(os.tmpdir(), `elydora-codex-event-${process.pid}-${Date.now()}.json`);
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
    const guard = findHandler(settings, 'PreToolUse', 'Checking Elydora agent state');
    const audit = findHandler(settings, 'PostToolUse', 'Recording Elydora tool use');
    assert(guard);
    assert(audit);

    const commandKey = process.platform === 'win32' ? 'commandWindows' : 'command';
    const guardResult = await runCommand(guard[commandKey], JSON.stringify({
      hook_event_name: 'PreToolUse',
      session_id: 'session-1',
      tool_name: 'Bash',
      tool_input: { command: 'echo test' },
    }));
    assert.equal(guardResult.code, 2);
    assert.match(guardResult.stderr, /Agent is frozen by Elydora/);

    const payload = {
      hook_event_name: 'PostToolUse',
      session_id: 'session-1',
      tool_name: 'Bash',
      tool_use_id: 'call-1',
      tool_input: { command: 'echo test' },
      tool_response: { output: 'test' },
    };
    const auditResult = await runCommand(audit[commandKey], JSON.stringify(payload));
    assert.equal(auditResult.code, 0, auditResult.stderr);
    assert.deepEqual(JSON.parse(await readFile(capturePath, 'utf-8')), payload);
  } finally {
    await fixture.close();
    await rm(capturePath, { force: true });
  }
});

test('Codex status requires both runtime scripts and uninstall preserves other hooks', async () => {
  const fixture = await createFixture({ existingSettings: {
    hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'existing-command' }] }] },
  } });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const status = await runPlugin(fixture.homeDir, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);

    await rm(fixture.guardScriptPath);
    const degradedStatus = await runPlugin(fixture.homeDir, 'status', null);
    assert.equal(JSON.parse(degradedStatus.stdout).installed, false);

    const uninstall = await runPlugin(fixture.homeDir, 'uninstall', 'agent-1');
    assert.equal(uninstall.code, 0, uninstall.stderr);
    const settings = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.equal(settings.hooks.PreToolUse.length, 1);
    assert.equal(settings.hooks.PreToolUse[0].hooks[0].command, 'existing-command');
    assert.deepEqual(settings.hooks.PostToolUse, []);
  } finally {
    await fixture.close();
  }
});

test('Codex install preserves user handlers that reuse Elydora status text', async () => {
  const userGuard = {
    type: 'command',
    command: 'user-guard',
    statusMessage: 'Checking Elydora agent state',
  };
  const userAudit = {
    type: 'command',
    command: 'user-audit',
    statusMessage: 'Recording Elydora tool use',
  };
  const fixture = await createFixture({ existingSettings: {
    hooks: {
      PreToolUse: [{ matcher: 'Bash', hooks: [userGuard] }],
      PostToolUse: [{ matcher: 'Bash', hooks: [userAudit] }],
    },
  } });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const settings = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    assert.deepEqual(settings.hooks.PreToolUse[0].hooks[0], userGuard);
    assert.deepEqual(settings.hooks.PostToolUse[0].hooks[0], userAudit);
    assert.equal(settings.hooks.PreToolUse.length, 2);
    assert.equal(settings.hooks.PostToolUse.length, 2);
  } finally {
    await fixture.close();
  }
});

test('Codex install rejects missing runtimes before writing hooks config', async () => {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-codex-missing-runtime-'));
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const configPath = path.join(homeDir, '.codex', 'hooks.json');
  try {
    const install = await runPlugin(homeDir, 'install', {
      agentName: 'codex',
      agentId: 'agent-1',
      baseUrl: 'https://api.elydora.com',
      guardScriptPath: path.join(agentDir, 'guard.js'),
      hookScriptPath: path.join(agentDir, 'hook.js'),
    });
    assert.equal(install.code, 1);
    assert.match(install.stderr, /runtime is missing/i);
    await assert.rejects(readFile(configPath), { code: 'ENOENT' });
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('Codex status surfaces malformed referenced runtime metadata', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    await writeFile(path.join(fixture.agentDir, 'config.json'), '{ malformed');
    const status = await runPlugin(fixture.homeDir, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /parse Elydora runtime config/i);
  } finally {
    await fixture.close();
  }
});

test('Codex status surfaces malformed hook matcher groups', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const settings = JSON.parse(await readFile(fixture.configPath, 'utf-8'));
    settings.hooks.PreToolUse[0].hooks = null;
    await writeFile(fixture.configPath, JSON.stringify(settings, null, 2));
    const status = await runPlugin(fixture.homeDir, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /matcher group must contain a hooks array/i);
  } finally {
    await fixture.close();
  }
});

test('Codex uninstall removes a hooks config owned entirely by Elydora', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const uninstall = await runPlugin(fixture.homeDir, 'uninstall', 'agent-1');
    assert.equal(uninstall.code, 0, uninstall.stderr);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Codex install preserves invalid contract shapes before writing', async () => {
  for (const existingSettings of [
    { hooks: null },
    { hooks: { PreToolUse: null } },
    { hooks: { PreToolUse: [{ hooks: null }] } },
  ]) {
    const fixture = await createFixture({ existingSettings });
    try {
      assert.equal(fixture.installResult.code, 1);
      assert.deepEqual(
        JSON.parse(await readFile(fixture.configPath, 'utf-8')),
        existingSettings,
      );
    } finally {
      await fixture.close();
    }
  }
});

test('Codex install preserves malformed config for recovery', async () => {
  const fixture = await createFixture({ existingSettings: '{ malformed' });
  try {
    assert.equal(fixture.installResult.code, 1);
    assert.match(fixture.installResult.stderr, /parse Codex hooks config/i);
    assert.equal(await readFile(fixture.configPath, 'utf-8'), '{ malformed');
  } finally {
    await fixture.close();
  }
});
