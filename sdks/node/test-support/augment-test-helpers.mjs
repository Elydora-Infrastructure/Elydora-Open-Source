import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const VALID_PRIVATE_KEY = Buffer.alloc(32).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/augment.js')).href;
export const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
export const ioModuleUrl = pathToFileURL(path.resolve('dist/plugins/augment-io.js')).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/augment-installation.js'),
).href;
export const managedInstallationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/managed-installation.js'),
).href;
export const cliPath = path.resolve('dist/cli.js');
export const wrapperExtension = process.platform === 'win32' ? '.cmd' : '.sh';
export const guardWrapperName = `augment-guard${wrapperExtension}`;
export const auditWrapperName = `augment-hook${wrapperExtension}`;

export function runProcess(command, args, env, cwd, input = '') {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      env: { ...process.env, ...env },
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

export function runHandler(handler, input, fixture) {
  return new Promise((resolve, reject) => {
    const child = spawn(handler.command, {
      cwd: fixture.projectDir,
      env: { ...process.env, HOME: fixture.homeDir, USERPROFILE: fixture.homeDir },
      shell: true,
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

export async function writeJson(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  const contents = typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`;
  await writeFile(filePath, contents, { encoding: 'utf-8', mode: 0o600 });
}

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'augment',
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

export async function runPlugin(fixture, method, argument) {
  const source = `
    import { augmentPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await augmentPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    },
    fixture.projectDir,
  );
}

export async function createFixture({ baseUrl = 'http://127.0.0.1:9', settings } = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-augment-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %AUGGIE%");
  const projectDir = path.join(rootDir, 'project with spaces');
  const settingsPath = path.join(homeDir, '.augment', 'settings.json');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(projectDir, { recursive: true });
  if (settings !== undefined) await writeJson(settingsPath, settings);
  const fixture = {
    agentDir,
    auditWrapperPath: path.join(agentDir, auditWrapperName),
    baseUrl,
    guardScriptPath,
    guardWrapperPath: path.join(agentDir, guardWrapperName),
    homeDir,
    hookScriptPath,
    projectDir,
    rootDir,
    settingsPath,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
  return fixture;
}

export async function readSettings(filePath) {
  const raw = await readFile(filePath, 'utf-8');
  return { raw, settings: JSON.parse(raw) };
}

export function generatedCommand(wrapperPath) {
  if (process.platform === 'win32') return `"${wrapperPath.replaceAll('"', '\\"')}"`;
  return `'${wrapperPath.replaceAll("'", `'"'"'`)}'`;
}

export function managedHandler(settings, event, wrapperPath) {
  const command = generatedCommand(wrapperPath);
  for (const group of settings.hooks?.[event] ?? []) {
    const handler = group.hooks.find((candidate) => candidate.command === command);
    if (handler) return handler;
  }
  return undefined;
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
