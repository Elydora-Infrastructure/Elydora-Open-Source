import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/kirocli.js')).href;
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

async function runPlugin(homeDir, method, argument) {
  const script = `
    import { kirocliPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await kirocliPlugin[process.env.ELYDORA_TEST_METHOD](argument);
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

async function writeJsonOrText(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, typeof value === 'string' ? value : JSON.stringify(value, null, 2));
}

async function createFixture({ existingV2, existingV3, guardSource, hookSource } = {}) {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-kirocli-'));
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const v2Path = path.join(homeDir, '.kiro', 'agents', 'elydora-audit.json');
  const v3Path = path.join(homeDir, '.kiro', 'hooks', 'elydora-audit.json');
  await mkdir(agentDir, { recursive: true });

  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  const runtimeConfigPath = path.join(agentDir, 'config.json');
  await writeFile(
    guardScriptPath,
    guardSource ?? "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n",
  );
  await writeFile(hookScriptPath, hookSource ?? 'process.exit(0);\n');
  await writeFile(runtimeConfigPath, JSON.stringify({
    agent_id: 'agent-1',
    agent_name: 'kirocli',
  }));

  if (existingV2 !== undefined) await writeJsonOrText(v2Path, existingV2);
  if (existingV3 !== undefined) await writeJsonOrText(v3Path, existingV3);

  const config = {
    agentName: 'kirocli',
    agentId: 'agent-1',
    baseUrl: 'https://api.elydora.com',
    guardScriptPath,
    hookScriptPath,
  };
  const installResult = await runPlugin(homeDir, 'install', config);

  return {
    config,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    installResult,
    runtimeConfigPath,
    v2Path,
    v3Path,
    async close() {
      await rm(homeDir, { recursive: true, force: true });
    },
  };
}

function findV3Hook(config, name) {
  return config.hooks.find((hook) => hook.name === name);
}

test('Kiro CLI registry points at the v3 global hook contract', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('kirocli'), {
    name: 'Kiro CLI',
    configDir: '~/.kiro/hooks',
    configFile: 'elydora-audit.json',
  });
});

test('Kiro CLI install preserves user hooks and writes idempotent v2 and v3 contracts', async () => {
  const fixture = await createFixture({
    existingV2: {
      description: 'User Kiro agent',
      tools: ['read'],
      hooks: {
        agentSpawn: [{ command: 'existing-spawn' }],
        preToolUse: [{ matcher: 'read', command: 'existing-v2' }],
      },
    },
    existingV3: {
      version: 'v1',
      hooks: [{
        name: 'existing-v3',
        trigger: 'SessionStart',
        action: { type: 'command', command: 'existing-command' },
      }],
    },
  });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    assert.match(fixture.installResult.stdout, /--agent elydora-audit/);
    assert.match(fixture.installResult.stdout, /--v3/);
    const secondInstall = await runPlugin(fixture.homeDir, 'install', fixture.config);
    assert.equal(secondInstall.code, 0, secondInstall.stderr);

    const v2 = JSON.parse(await readFile(fixture.v2Path, 'utf-8'));
    assert.equal(v2.description, 'User Kiro agent');
    assert.deepEqual(v2.tools, ['read']);
    assert.equal(v2.hooks.agentSpawn[0].command, 'existing-spawn');
    assert.equal(v2.hooks.preToolUse.length, 2);
    assert.equal(v2.hooks.postToolUse.length, 1);
    assert.equal(v2.hooks.preToolUse[1].matcher, '*');
    assert.equal(typeof v2.hooks.preToolUse[1].command, 'string');

    const v3 = JSON.parse(await readFile(fixture.v3Path, 'utf-8'));
    assert.equal(v3.version, 'v1');
    assert.equal(v3.hooks.length, 3);
    assert.equal(v3.hooks[0].name, 'existing-v3');
    assert.deepEqual(findV3Hook(v3, 'elydora-guard'), {
      name: 'elydora-guard',
      description: 'Block tool use when the Elydora agent is frozen',
      trigger: 'PreToolUse',
      matcher: '.*',
      action: { type: 'command', command: findV3Hook(v3, 'elydora-guard').action.command },
      timeout: 5,
      enabled: true,
    });
    assert.equal(findV3Hook(v3, 'elydora-audit').trigger, 'PostToolUse');
  } finally {
    await fixture.close();
  }
});

test('Kiro CLI hook commands enforce freezes and forward the official event payload', async () => {
  const capturePath = path.join(os.tmpdir(), `elydora-kiro-event-${process.pid}-${Date.now()}.json`);
  const fixture = await createFixture({
    hookSource: `
      const fs = require('node:fs');
      const chunks = [];
      process.stdin.on('data', (chunk) => chunks.push(chunk));
      process.stdin.on('end', () => fs.writeFileSync(${JSON.stringify(capturePath)}, Buffer.concat(chunks)));
    `,
  });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const v3 = JSON.parse(await readFile(fixture.v3Path, 'utf-8'));
    const payload = {
      hook_event_name: 'PreToolUse',
      cwd: fixture.homeDir,
      session_id: 'session-1',
      tool_name: 'execute_bash',
      tool_input: { command: 'echo test' },
    };
    const guardResult = await runCommand(findV3Hook(v3, 'elydora-guard').action.command, JSON.stringify(payload));
    assert.equal(guardResult.code, 2);
    assert.match(guardResult.stderr, /Agent is frozen by Elydora/);

    payload.hook_event_name = 'PostToolUse';
    payload.tool_response = { success: true, result: 'test' };
    const auditResult = await runCommand(findV3Hook(v3, 'elydora-audit').action.command, JSON.stringify(payload));
    assert.equal(auditResult.code, 0, auditResult.stderr);
    assert.deepEqual(JSON.parse(await readFile(capturePath, 'utf-8')), payload);
  } finally {
    await fixture.close();
    await rm(capturePath, { force: true });
  }
});

test('Kiro CLI status accepts either contract and requires both runtime scripts', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const status = await runPlugin(fixture.homeDir, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);

    await rm(fixture.v3Path);
    const v2Only = await runPlugin(fixture.homeDir, 'status', null);
    const v2Status = JSON.parse(v2Only.stdout);
    assert.equal(v2Status.installed, true);
    assert.equal(v2Status.configPath, fixture.v2Path);

    const reinstall = await runPlugin(fixture.homeDir, 'install', fixture.config);
    assert.equal(reinstall.code, 0, reinstall.stderr);
    await rm(fixture.v2Path);
    const v3Only = await runPlugin(fixture.homeDir, 'status', null);
    const v3Status = JSON.parse(v3Only.stdout);
    assert.equal(v3Status.installed, true);
    assert.equal(v3Status.configPath, fixture.v3Path);

    await rm(fixture.guardScriptPath);
    const degraded = await runPlugin(fixture.homeDir, 'status', null);
    assert.equal(JSON.parse(degraded.stdout).installed, false);
  } finally {
    await fixture.close();
  }
});

test('Kiro CLI status surfaces malformed referenced runtime metadata', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    await writeFile(fixture.runtimeConfigPath, '{ malformed');
    const status = await runPlugin(fixture.homeDir, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /parse Elydora runtime config/i);
  } finally {
    await fixture.close();
  }
});

test('Kiro CLI uninstall preserves unrelated v2 and v3 hooks', async () => {
  const fixture = await createFixture({
    existingV2: { hooks: { preToolUse: [{ matcher: 'read', command: 'existing-v2' }] } },
    existingV3: {
      version: 'v1',
      hooks: [{
        name: 'existing-v3',
        trigger: 'SessionStart',
        action: { type: 'command', command: 'existing-command' },
      }],
    },
  });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const result = await runPlugin(fixture.homeDir, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    const v2 = JSON.parse(await readFile(fixture.v2Path, 'utf-8'));
    assert.deepEqual(v2.hooks.preToolUse, [{ matcher: 'read', command: 'existing-v2' }]);
    assert.deepEqual(v2.hooks.postToolUse, []);
    const v3 = JSON.parse(await readFile(fixture.v3Path, 'utf-8'));
    assert.equal(v3.hooks.length, 1);
    assert.equal(v3.hooks[0].name, 'existing-v3');
  } finally {
    await fixture.close();
  }
});

test('Kiro CLI uninstall removes configs owned entirely by Elydora', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const result = await runPlugin(fixture.homeDir, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    await assert.rejects(readFile(fixture.v2Path), { code: 'ENOENT' });
    await assert.rejects(readFile(fixture.v3Path), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Kiro CLI install preserves malformed configs for recovery', async () => {
  const fixture = await createFixture({ existingV3: '{ malformed' });
  try {
    assert.equal(fixture.installResult.code, 1);
    assert.match(fixture.installResult.stderr, /parse Kiro CLI v3 hooks config/i);
    assert.equal(await readFile(fixture.v3Path, 'utf-8'), '{ malformed');
  } finally {
    await fixture.close();
  }

  const v2Fixture = await createFixture({ existingV2: '{ malformed' });
  try {
    assert.equal(v2Fixture.installResult.code, 1);
    assert.match(v2Fixture.installResult.stderr, /parse Kiro CLI v2 agent config/i);
    assert.equal(await readFile(v2Fixture.v2Path, 'utf-8'), '{ malformed');
  } finally {
    await v2Fixture.close();
  }
});
