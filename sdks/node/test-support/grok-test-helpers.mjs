import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const VALID_PRIVATE_KEY = Buffer.alloc(32).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/grok.js')).href;
export const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
export const contractModuleUrl = pathToFileURL(path.resolve('dist/plugins/grok-contract.js')).href;
export const ioModuleUrl = pathToFileURL(path.resolve('dist/plugins/grok-io.js')).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/grok-installation.js'),
).href;
export const cliPath = path.resolve('dist/cli.js');

export function runNode(args, env, cwd, input = '', unset = []) {
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

export function runGrokHook(command, input, fixture, environment = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, {
      shell: true,
      cwd: fixture.projectDir,
      env: {
        ...process.env,
        HOME: fixture.homeDir,
        USERPROFILE: fixture.homeDir,
        GROK_HOME: fixture.grokHomeOverride ?? '',
        ...environment,
      },
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

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'grok',
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

export async function runPlugin(fixture, method, argument, environment = {}) {
  const source = `
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
    ...environment,
  };
  const unset = [];
  if (fixture.grokHomeOverride === undefined) unset.push('GROK_HOME');
  else env.GROK_HOME = fixture.grokHomeOverride;
  return runNode(
    ['--input-type=module', '--eval', source],
    env,
    fixture.projectDir,
    '',
    unset,
  );
}

async function writeOptional(filePath, contents) {
  if (contents === undefined) return;
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, contents, { encoding: 'utf-8', mode: 0o600 });
}

export async function createFixture({
  baseUrl = 'http://127.0.0.1:9',
  config,
  explicitGrokHome = true,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-grok-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %GROK_HOOK_EVENT%");
  const projectDir = path.join(rootDir, 'project with spaces');
  const grokHome = explicitGrokHome
    ? path.join(homeDir, 'custom grok')
    : path.join(homeDir, '.grok');
  const configPath = path.join(grokHome, 'hooks', 'elydora-audit.json');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(projectDir, { recursive: true });
  await writeOptional(configPath, config);
  const fixture = {
    agentDir,
    baseUrl,
    configPath,
    grokHome,
    grokHomeOverride: explicitGrokHome ? grokHome : undefined,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    projectDir,
    rootDir,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
  return fixture;
}

export async function readGrokConfig(filePath) {
  const raw = await readFile(filePath, 'utf-8');
  return { raw, config: JSON.parse(raw) };
}

export function managedHandler(config, event) {
  for (const group of [...(config.hooks?.[event] ?? [])].reverse()) {
    if (Object.keys(group).length !== 1 || !Array.isArray(group.hooks)) continue;
    const handler = group.hooks.findLast((candidate) => (
      Object.keys(candidate).sort().join('|') === 'command|timeout|type'
      && candidate.type === 'command'
      && candidate.timeout === 10
    ));
    if (handler) return handler;
  }
  return undefined;
}

export function assertManagedHandler(handler) {
  assert(handler);
  assert.deepEqual(Object.keys(handler).sort(), ['command', 'timeout', 'type']);
  assert.equal(handler.type, 'command');
  assert.equal(handler.timeout, 10);
  if (process.platform === 'win32') {
    assert.match(handler.command, /^"[^"\r\n]+powershell\.exe" .* -EncodedCommand /i);
  } else {
    assert.match(handler.command, /^'[^']*node[^']*' /i);
  }
}

export function legacyCommand(scriptPath) {
  if (process.platform === 'win32') return `"${process.execPath}" "${scriptPath}"`;
  const quote = (value) => `'${value.replaceAll("'", `'"'"'`)}'`;
  return `${quote(process.execPath)} ${quote(scriptPath)}`;
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
