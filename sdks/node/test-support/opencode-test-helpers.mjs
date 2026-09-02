import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const VALID_PRIVATE_KEY = Buffer.alloc(32, 19).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/opencode.js')).href;
export const contractModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/opencode-contract.js'),
).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/opencode-installation.js'),
).href;
export const ioModuleUrl = pathToFileURL(path.resolve('dist/plugins/opencode-io.js')).href;
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
    XDG_CONFIG_HOME: fixture.configRoot,
  };
}

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'opencode',
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
    import { opencodePlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await opencodePlugin[process.env.ELYDORA_TEST_METHOD](argument);
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
  await writeFile(filePath, value, { mode: 0o600 });
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  existingPlugin,
  existingLegacy,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-opencode-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %OPEN%");
  const configRoot = path.join(rootDir, 'xdg config');
  const projectDir = path.join(rootDir, 'workspace with spaces');
  const pluginsDir = path.join(configRoot, 'opencode', 'plugins');
  const pluginPath = path.join(pluginsDir, 'elydora-audit.js');
  const legacyPluginPath = path.join(pluginsDir, 'elydora-audit.mjs');
  const agentDir = path.join(homeDir, '.elydora', agentId);
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(homeDir, { recursive: true });
  await mkdir(projectDir, { recursive: true });
  await writeOptional(pluginPath, existingPlugin);
  await writeOptional(legacyPluginPath, existingLegacy);
  return {
    agentDir,
    agentId,
    baseUrl,
    configRoot,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    legacyPluginPath,
    pluginPath,
    pluginsDir,
    projectDir,
    rootDir,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
    async loadPlugin() {
      const module = await import(`${pathToFileURL(this.pluginPath).href}?test=${Date.now()}`);
      return module.ElydoraAuditPlugin({});
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
}

export async function readJson(filePath) {
  return JSON.parse(await readFile(filePath, 'utf-8'));
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
    setStatus(value) {
      status = value;
    },
    setOperationStatus(value) {
      operationStatus = value;
    },
    close: () => new Promise((resolve, reject) => {
      server.close((error) => error ? reject(error) : resolve());
    }),
  };
}
