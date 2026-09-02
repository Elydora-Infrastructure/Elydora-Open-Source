import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const VALID_PRIVATE_KEY = Buffer.alloc(32, 23).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/letta.js')).href;
export const configModuleUrl = pathToFileURL(path.resolve('dist/plugins/letta-config.js')).href;
export const contractModuleUrl = pathToFileURL(path.resolve('dist/plugins/letta-contract.js')).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/letta-installation.js'),
).href;
export const sourcesModuleUrl = pathToFileURL(path.resolve('dist/plugins/letta-sources.js')).href;
export const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
export const cliPath = path.resolve('dist/cli.js');

export function runProcess(command, args, env, cwd, input = '') {
  return new Promise((resolve, reject) => {
    const childEnvironment = { ...process.env, ...env };
    for (const [name, value] of Object.entries(env)) {
      if (value === undefined) delete childEnvironment[name];
    }
    const child = spawn(command, args, {
      cwd,
      env: childEnvironment,
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

export function runNode(args, env, cwd, input = '') {
  return runProcess(process.execPath, args, env, cwd, input);
}

export function environment(fixture) {
  return {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
  };
}

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'letta',
    orgId: 'org-1',
    agentId: fixture.agentId,
    privateKey: VALID_PRIVATE_KEY,
    kid: 'kid-1',
    token: 'token-1',
    baseUrl: fixture.baseUrl,
    guardScriptPath: fixture.guardScriptPath,
    hookScriptPath: fixture.hookScriptPath,
    ...overrides,
  };
}

export async function runPlugin(fixture, method, argument) {
  const source = `
    import { lettaPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await lettaPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      ...environment(fixture),
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    },
    fixture.projectDir,
  );
}

async function writeOptional(filePath, value) {
  if (value === undefined) return;
  await mkdir(path.dirname(filePath), { recursive: true });
  const source = typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`;
  await writeFile(filePath, source, { mode: 0o600 });
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  globalSettings,
  projectSettings,
  localSettings,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-letta-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %HOME%");
  const projectDir = path.join(rootDir, 'workspace with spaces');
  const globalPath = path.join(homeDir, '.letta', 'settings.json');
  const projectPath = path.join(projectDir, '.letta', 'settings.json');
  const localPath = path.join(projectDir, '.letta', 'settings.local.json');
  const agentDir = path.join(homeDir, '.elydora', agentId);
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(homeDir, { recursive: true });
  await mkdir(projectDir, { recursive: true });
  await writeOptional(globalPath, globalSettings);
  await writeOptional(projectPath, projectSettings);
  await writeOptional(localPath, localSettings);
  return {
    agentDir,
    agentId,
    baseUrl,
    globalPath,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    localPath,
    projectDir,
    projectPath,
    rootDir,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
}

export async function readSettings(filePath) {
  return JSON.parse(await readFile(filePath, 'utf-8'));
}

export function managedHandler(settings, event) {
  return settings.hooks[event].find((group) => (
    group.matcher === '*'
    && group.hooks.length === 1
    && group.hooks[0].timeout === 10_000
  ))?.hooks[0];
}

export function runLettaHook(handler, input, fixture) {
  const shell = process.platform === 'win32' ? 'powershell.exe' : '/bin/sh';
  const args = process.platform === 'win32'
    ? ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', handler.command]
    : ['-c', handler.command];
  return runProcess(shell, args, environment(fixture), fixture.projectDir, input);
}

export async function startApiServer(initialStatus = 'active') {
  const requests = [];
  let status = initialStatus;
  let operationStatus = 201;
  const server = http.createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const raw = Buffer.concat(chunks).toString('utf-8');
    requests.push({ method: request.method, url: request.url, raw });
    if (request.method === 'GET') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ agent: { status } }));
      return;
    }
    response.writeHead(operationStatus, { 'Content-Type': 'application/json' });
    response.end(operationStatus < 400
      ? '{"operation":{"accepted":true}}'
      : '{"error":{"code":"AUDIT_UNAVAILABLE"}}');
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  return {
    baseUrl: `http://127.0.0.1:${address.port}`,
    requests,
    setStatus(value) { status = value; },
    setOperationStatus(value) { operationStatus = value; },
    close: () => new Promise((resolve, reject) => {
      server.close((error) => error ? reject(error) : resolve());
    }),
  };
}
