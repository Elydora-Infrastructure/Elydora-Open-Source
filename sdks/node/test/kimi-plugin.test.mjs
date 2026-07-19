import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { chmod, mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';
import { parse as parseToml } from '@decimalturn/toml-patch';

const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/kimi.js')).href;
const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
const cliPath = path.resolve('dist/cli.js');

function runNode(args, env, input = '', unset = []) {
  return new Promise((resolve, reject) => {
    const childEnv = { ...process.env, ...env };
    for (const key of unset) delete childEnv[key];
    const child = spawn(process.execPath, args, {
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

async function runPlugin(fixture, method, argument) {
  const script = `
    import { kimiPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await kimiPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  const env = {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    PATH: fixture.binDir,
    ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
    ELYDORA_TEST_METHOD: method,
  };
  if (fixture.explicitKimiHome) env.KIMI_CODE_HOME = fixture.kimiHome;
  return runNode(
    ['--input-type=module', '--eval', script],
    env,
    '',
    fixture.explicitKimiHome ? [] : ['KIMI_CODE_HOME'],
  );
}

async function writeOptional(filePath, contents) {
  if (contents === undefined) return;
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, contents, 'utf-8');
}

async function createFixture({
  guardSource,
  hookSource,
  modernConfig,
  legacyConfig,
  legacyInstalled = true,
  explicitKimiHome = true,
} = {}) {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-kimi-'));
  const binDir = path.join(homeDir, 'bin');
  const kimiHome = explicitKimiHome
    ? path.join(homeDir, 'custom-kimi-code')
    : path.join(homeDir, '.kimi-code');
  const modernPath = path.join(kimiHome, 'config.toml');
  const legacyPath = path.join(homeDir, '.kimi', 'config.toml');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(agentDir, { recursive: true });
  await mkdir(binDir, { recursive: true });
  if (legacyInstalled) {
    const executable = path.join(binDir, process.platform === 'win32' ? 'kimi-cli.cmd' : 'kimi-cli');
    await writeFile(executable, '');
    if (process.platform !== 'win32') await chmod(executable, 0o700);
  }
  await writeFile(
    guardScriptPath,
    guardSource ?? "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n",
  );
  await writeFile(hookScriptPath, hookSource ?? 'process.exit(0);\n');
  await writeFile(path.join(agentDir, 'config.json'), JSON.stringify({
    agent_id: 'agent-1',
    agent_name: 'kimi',
  }));
  await writeOptional(modernPath, modernConfig);
  await writeOptional(legacyPath, legacyConfig);

  const fixture = {
    agentDir,
    binDir,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    kimiHome,
    explicitKimiHome,
    legacyPath,
    modernPath,
    async close() {
      await rm(homeDir, { recursive: true, force: true });
    },
  };
  fixture.installResult = await runPlugin(fixture, 'install', {
    agentName: 'kimi',
    agentId: 'agent-1',
    baseUrl: 'https://api.elydora.com',
    guardScriptPath,
    hookScriptPath,
  });
  return fixture;
}

function managedHook(config, event, scriptName) {
  return config.hooks.find(
    (hook) => hook.event === event && hook.command.includes(scriptName),
  );
}

function assertStrictHook(hook) {
  assert.deepEqual(Object.keys(hook).sort(), ['command', 'event', 'timeout']);
  assert.equal(hook.timeout, 10);
}

test('Kimi is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('kimi'), {
    name: 'Kimi Code',
    configDir: '~/.kimi-code',
    configFile: 'config.toml',
  });

  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-kimi-cli-'));
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: homeDir,
      USERPROFILE: homeDir,
      KIMI_CODE_HOME: path.join(homeDir, '.kimi-code'),
    });
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Kimi Code \(kimi\)/);
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('Kimi install preserves both user configs and is idempotent', async () => {
  const modernConfig = [
    '# modern user config',
    'default_model = "kimi-code/k3"',
    '',
    '[[hooks]]',
    'event = "SessionStart"',
    'command = "existing-modern"',
    'timeout = 30 # keep modern hook',
    '',
  ].join('\n');
  const legacyConfig = [
    '# legacy user config',
    'telemetry = false',
    '',
    '[[hooks]]',
    'event = "SessionEnd"',
    'command = "existing-legacy"',
    '',
  ].join('\n');
  const fixture = await createFixture({ modernConfig, legacyConfig });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    assert.match(fixture.installResult.stdout, /Kimi Code and kimi-cli/i);
    const secondInstall = await runPlugin(fixture, 'install', {
      agentName: 'kimi',
      agentId: 'agent-1',
      baseUrl: 'https://api.elydora.com',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    assert.equal(secondInstall.code, 0, secondInstall.stderr);

    for (const [configPath, comment, existingCommand] of [
      [fixture.modernPath, '# modern user config', 'existing-modern'],
      [fixture.legacyPath, '# legacy user config', 'existing-legacy'],
    ]) {
      const raw = await readFile(configPath, 'utf-8');
      const config = parseToml(raw);
      assert.match(raw, new RegExp(comment));
      assert(config.hooks.some((hook) => hook.command === existingCommand));
      assert.equal(config.hooks.length, 3);
      assertStrictHook(managedHook(config, 'PreToolUse', 'guard.js'));
      assertStrictHook(managedHook(config, 'PostToolUse', 'hook.js'));
    }
    await assert.rejects(
      readFile(path.join(fixture.homeDir, '.kimi-code', 'config.toml')),
      { code: 'ENOENT' },
    );
  } finally {
    await fixture.close();
  }
});

test('Kimi Code install does not create a false legacy migration source', async () => {
  const fixture = await createFixture({ legacyInstalled: false });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    assert.equal(parseToml(await readFile(fixture.modernPath, 'utf-8')).hooks.length, 2);
    await assert.rejects(readFile(fixture.legacyPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Kimi Code treats an empty home override as the official default', async () => {
  const fixture = await createFixture({ explicitKimiHome: false, legacyInstalled: false });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    fixture.explicitKimiHome = true;
    fixture.kimiHome = '';
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);
  } finally {
    await fixture.close();
  }
});

test('Legacy kimi-cli install does not create a premature migration target', async () => {
  const fixture = await createFixture({ explicitKimiHome: false });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    assert.equal(parseToml(await readFile(fixture.legacyPath, 'utf-8')).hooks.length, 2);
    await assert.rejects(readFile(fixture.modernPath), { code: 'ENOENT' });
    await rm(fixture.binDir, { recursive: true });
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);
  } finally {
    await fixture.close();
  }
});

test('Kimi commands block freezes and forward the official payload unchanged', async () => {
  const capturePath = path.join(os.tmpdir(), `elydora-kimi-event-${process.pid}-${Date.now()}.json`);
  const hookSource = `
    const fs = require('node:fs');
    const chunks = [];
    process.stdin.on('data', (chunk) => chunks.push(chunk));
    process.stdin.on('end', () => fs.writeFileSync(${JSON.stringify(capturePath)}, Buffer.concat(chunks)));
  `;
  const fixture = await createFixture({ hookSource });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const config = parseToml(await readFile(fixture.modernPath, 'utf-8'));
    const guard = managedHook(config, 'PreToolUse', 'guard.js');
    const audit = managedHook(config, 'PostToolUse', 'hook.js');
    const prePayload = {
      hook_event_name: 'PreToolUse',
      session_id: 'session-1',
      cwd: fixture.homeDir,
      tool_name: 'Bash',
      tool_input: { command: 'echo test' },
      tool_call_id: 'call-1',
    };
    const guardResult = await runCommand(guard.command, JSON.stringify(prePayload));
    assert.equal(guardResult.code, 2);
    assert.match(guardResult.stderr, /Agent is frozen by Elydora/);

    const postPayload = {
      ...prePayload,
      hook_event_name: 'PostToolUse',
      tool_output: { output: 'test' },
    };
    const auditResult = await runCommand(audit.command, JSON.stringify(postPayload));
    assert.equal(auditResult.code, 0, auditResult.stderr);
    assert.deepEqual(JSON.parse(await readFile(capturePath, 'utf-8')), postPayload);
  } finally {
    await fixture.close();
    await rm(capturePath, { force: true });
  }
});

test('Kimi status accepts either runtime contract and requires both scripts', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    await rm(fixture.modernPath);
    let status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.equal(JSON.parse(status.stdout).installed, true);

    const reinstall = await runPlugin(fixture, 'install', {
      agentName: 'kimi',
      agentId: 'agent-1',
      baseUrl: 'https://api.elydora.com',
      guardScriptPath: fixture.guardScriptPath,
      hookScriptPath: fixture.hookScriptPath,
    });
    assert.equal(reinstall.code, 0, reinstall.stderr);
    await rm(fixture.legacyPath);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, true);

    await rm(fixture.guardScriptPath);
    status = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(status.stdout).installed, false);
  } finally {
    await fixture.close();
  }
});

test('Kimi uninstall preserves user hooks in both contracts', async () => {
  const userHook = [
    '# user hook',
    '[[hooks]]',
    'event = "SessionStart"',
    'command = "existing-command"',
    'timeout = 30 # keep timeout',
    '',
  ].join('\n');
  const fixture = await createFixture({ modernConfig: userHook, legacyConfig: userHook });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    for (const configPath of [fixture.modernPath, fixture.legacyPath]) {
      const raw = await readFile(configPath, 'utf-8');
      const config = parseToml(raw);
      assert.match(raw, /# user hook/);
      assert.match(raw, /# keep timeout/);
      assert.deepEqual(config.hooks.map((hook) => ({ ...hook })), [{
        event: 'SessionStart',
        command: 'existing-command',
        timeout: 30,
      }]);
    }
  } finally {
    await fixture.close();
  }
});

test('Kimi uninstall removes configs created entirely by Elydora', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    await assert.rejects(readFile(fixture.modernPath), { code: 'ENOENT' });
    await assert.rejects(readFile(fixture.legacyPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Kimi parses every config before writing either contract', async () => {
  const modernConfig = '# untouched modern\ndefault_model = "kimi-code/k3"\n';
  const legacyConfig = '[malformed';
  const fixture = await createFixture({ modernConfig, legacyConfig });
  try {
    assert.equal(fixture.installResult.code, 1);
    assert.match(fixture.installResult.stderr, /parse kimi-cli legacy hooks config/i);
    assert.equal(await readFile(fixture.modernPath, 'utf-8'), modernConfig);
    assert.equal(await readFile(fixture.legacyPath, 'utf-8'), legacyConfig);
  } finally {
    await fixture.close();
  }
});

test('Kimi rejects invalid hook shapes without rewriting user config', async () => {
  const modernConfig = [
    '[[hooks]]',
    'event = "PreToolUse"',
    'command = "existing-command"',
    'cwd = "/tmp"',
    '',
  ].join('\n');
  const fixture = await createFixture({ modernConfig });
  try {
    assert.equal(fixture.installResult.code, 1);
    assert.match(fixture.installResult.stderr, /unsupported field "cwd"/i);
    assert.equal(await readFile(fixture.modernPath, 'utf-8'), modernConfig);
    await assert.rejects(readFile(fixture.legacyPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Kimi install rejects missing runtimes before creating configs', async () => {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-kimi-missing-runtime-'));
  const fixture = {
    binDir: path.join(homeDir, 'bin'),
    explicitKimiHome: true,
    homeDir,
    kimiHome: path.join(homeDir, 'custom-kimi-code'),
    modernPath: path.join(homeDir, 'custom-kimi-code', 'config.toml'),
    legacyPath: path.join(homeDir, '.kimi', 'config.toml'),
  };
  try {
    const result = await runPlugin(fixture, 'install', {
      agentName: 'kimi',
      agentId: 'agent-1',
      guardScriptPath: path.join(homeDir, '.elydora', 'agent-1', 'guard.js'),
      hookScriptPath: path.join(homeDir, '.elydora', 'agent-1', 'hook.js'),
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /runtime is missing/i);
    await assert.rejects(readFile(fixture.modernPath), { code: 'ENOENT' });
    await assert.rejects(readFile(fixture.legacyPath), { code: 'ENOENT' });
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('Kimi status surfaces malformed referenced runtime metadata', async () => {
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

test('Kimi atomic writes leave no temporary files behind', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    for (const directory of [path.dirname(fixture.modernPath), path.dirname(fixture.legacyPath)]) {
      assert.equal((await readdir(directory)).some((name) => name.endsWith('.tmp')), false);
    }
  } finally {
    await fixture.close();
  }
});
