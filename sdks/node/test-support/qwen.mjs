import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { parse } from 'jsonc-parser';

export const VALID_PRIVATE_KEY = Buffer.alloc(32).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/qwen.js')).href;
export const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
export const sourcesModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/qwen-sources.js'),
).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/qwen-installation.js'),
).href;
export const configModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/qwen-config.js'),
).href;
export const contractModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/qwen-contract.js'),
).href;
export const cliPath = path.resolve('dist/cli.js');

const QWEN_ENV_KEYS = [
  'QWEN_HOME',
  'QWEN_RUNTIME_DIR',
  'QWEN_CODE_SYSTEM_SETTINGS_PATH',
  'QWEN_CODE_SYSTEM_DEFAULTS_PATH',
  'QWEN_CODE_TRUSTED_FOLDERS_PATH',
];

export function runProcess(command, args, env, cwd, input = '', unset = []) {
  return new Promise((resolve, reject) => {
    const childEnv = { ...process.env, ...env };
    for (const key of unset) delete childEnv[key];
    const child = spawn(command, args, {
      cwd,
      env: childEnv,
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
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

export function runNode(args, env, cwd, input = '', unset = []) {
  return runProcess(process.execPath, args, env, cwd, input, unset);
}

function windowsPowerShell() {
  return path.win32.join(
    process.env.SystemRoot || 'C:\\Windows',
    'System32',
    'WindowsPowerShell',
    'v1.0',
    'powershell.exe',
  );
}

export function runQwenHook(handler, input, fixture, environment = {}) {
  const env = {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    ...environment,
  };
  if (process.platform === 'win32') {
    return runProcess(
      windowsPowerShell(),
      ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', handler.command],
      env,
      fixture.projectDir,
      input,
    );
  }
  return runProcess('/bin/bash', ['-c', handler.command], env, fixture.projectDir, input);
}

export async function writeSettings(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  const contents = typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`;
  await writeFile(filePath, contents, { encoding: 'utf-8', mode: 0o600 });
}

export function parseSettings(raw) {
  const errors = [];
  const value = parse(raw, errors, {
    allowTrailingComma: false,
    disallowComments: false,
  });
  assert.deepEqual(errors, []);
  return value;
}

export async function readSettings(filePath) {
  const raw = await readFile(filePath, 'utf-8');
  return { raw, settings: parseSettings(raw) };
}

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'qwen',
    orgId: 'org-1',
    agentId: 'agent-1',
    privateKey: VALID_PRIVATE_KEY,
    kid: 'kid-1',
    token: 'token-1',
    baseUrl: fixture.baseUrl,
    guardScriptPath: fixture.guardScriptPath,
    hookScriptPath: fixture.hookScriptPath,
    ...overrides,
  };
}

function qwenEnvironment(fixture, environment) {
  const env = {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    QWEN_CODE_SYSTEM_DEFAULTS_PATH: path.join(fixture.rootDir, 'system-defaults.json'),
    QWEN_CODE_SYSTEM_SETTINGS_PATH: path.join(fixture.rootDir, 'system-settings.json'),
    ...environment,
  };
  const unset = QWEN_ENV_KEYS.filter((key) => !Object.hasOwn(env, key));
  return { env, unset };
}

export async function runPlugin(fixture, method, argument, environment = {}) {
  const source = `
    import { qwenPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await qwenPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  const configured = qwenEnvironment(fixture, {
    ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
    ELYDORA_TEST_METHOD: method,
    ...environment,
  });
  return runNode(
    ['--input-type=module', '--eval', source],
    configured.env,
    fixture.projectDir,
    '',
    configured.unset,
  );
}

export async function createFixture({
  baseUrl = 'http://127.0.0.1:9',
  settings,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-qwen-'));
  const homeDir = path.join(
    rootDir,
    "home with spaces and 'quote $QWEN_PROJECT_DIR %QWEN_PROJECT_DIR%",
  );
  const projectDir = path.join(rootDir, 'project with spaces');
  const settingsPath = path.join(homeDir, '.qwen', 'settings.json');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(homeDir, { recursive: true });
  await mkdir(projectDir, { recursive: true });
  if (settings !== undefined) await writeSettings(settingsPath, settings);
  const fixture = {
    agentDir,
    baseUrl,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    projectDir,
    rootDir,
    settingsPath,
    install(overrides = {}, environment = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides), environment);
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
  return fixture;
}

export function managedHandler(settings, event, name) {
  for (const group of settings.hooks?.[event] ?? []) {
    if (Object.keys(group).join('|') !== 'hooks' || !Array.isArray(group.hooks)) continue;
    const handler = group.hooks.find((candidate) => (
      Object.keys(candidate).sort().join('|') === 'command|name|shell|timeout|type'
      && candidate.type === 'command'
      && candidate.name === name
      && candidate.shell === (process.platform === 'win32' ? 'powershell' : 'bash')
      && candidate.timeout === 10_000
    ));
    if (handler) return handler;
  }
  return undefined;
}

export function assertManagedHandler(handler, name) {
  assert(handler);
  assert.deepEqual(Object.keys(handler).sort(), [
    'command',
    'name',
    'shell',
    'timeout',
    'type',
  ]);
  assert.equal(handler.type, 'command');
  assert.equal(handler.name, name);
  assert.equal(handler.shell, process.platform === 'win32' ? 'powershell' : 'bash');
  assert.equal(handler.timeout, 10_000);
}

function quoteShell(value) {
  return process.platform === 'win32'
    ? `'${value.replaceAll("'", "''")}'`
    : `'${value.replaceAll("'", `'"'"'`)}'`;
}

export function generatedCommand(scriptPath) {
  const invocation = `${quoteShell(process.execPath)} ${quoteShell(scriptPath)}`;
  return process.platform === 'win32'
    ? `& ${invocation}; exit $LASTEXITCODE`
    : invocation;
}

export function legacyGroup(scriptPath) {
  return {
    matcher: '*',
    hooks: [{
      type: 'command',
      command: generatedCommand(scriptPath),
      shell: process.platform === 'win32' ? 'powershell' : 'bash',
      timeout: 10_000,
    }],
  };
}

export async function startApiServer({ status = 'active', operationStatus = 201 } = {}) {
  const requests = [];
  const server = http.createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const raw = Buffer.concat(chunks).toString('utf-8');
    requests.push({ headers: request.headers, method: request.method, url: request.url, raw });
    if (request.method === 'GET') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ agent: { status } }));
      return;
    }
    response.writeHead(operationStatus, { 'Content-Type': 'application/json' });
    response.end(operationStatus >= 200 && operationStatus < 300
      ? JSON.stringify({ operation: { accepted: true } })
      : JSON.stringify({ error: { code: 'UPSTREAM_FAILURE', message: 'failed' } }));
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  return {
    baseUrl: `http://127.0.0.1:${address.port}`,
    requests,
    close: () => new Promise((resolve, reject) => {
      server.close((error) => error ? reject(error) : resolve());
    }),
  };
}
