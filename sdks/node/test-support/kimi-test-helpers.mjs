import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { parse as parseToml } from '@decimalturn/toml-patch';

export const VALID_PRIVATE_KEY = Buffer.alloc(32).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/kimi.js')).href;
export const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
export const contractModuleUrl = pathToFileURL(path.resolve('dist/plugins/kimi-contract.js')).href;
export const ioModuleUrl = pathToFileURL(path.resolve('dist/plugins/kimi-io.js')).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/kimi-installation.js'),
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

export function runKimiHook(command, input, fixture, environment = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, {
      shell: true,
      cwd: fixture.projectDir,
      env: {
        ...process.env,
        HOME: fixture.homeDir,
        USERPROFILE: fixture.homeDir,
        KIMI_CODE_HOME: fixture.kimiHomeOverride ?? '',
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
    agentName: 'kimi',
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
    import { kimiPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await kimiPlugin[process.env.ELYDORA_TEST_METHOD](argument);
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
  if (fixture.kimiHomeOverride === undefined) unset.push('KIMI_CODE_HOME');
  else env.KIMI_CODE_HOME = fixture.kimiHomeOverride;
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
  stableConfig,
  legacyConfig,
  stableDetected = true,
  legacyDetected = true,
  explicitKimiHome = true,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-kimi-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %ELYDORA_HOOK_PATH%");
  const projectDir = path.join(rootDir, 'project with spaces');
  const kimiHome = explicitKimiHome
    ? path.join(homeDir, 'custom kimi-code')
    : path.join(homeDir, '.kimi-code');
  const stablePath = path.join(kimiHome, 'config.toml');
  const legacyHome = path.join(homeDir, '.kimi');
  const legacyPath = path.join(legacyHome, 'config.toml');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(projectDir, { recursive: true });
  if (stableDetected && !explicitKimiHome) await mkdir(kimiHome, { recursive: true });
  if (legacyDetected) await mkdir(legacyHome, { recursive: true });
  await writeOptional(stablePath, stableConfig);
  await writeOptional(legacyPath, legacyConfig);
  const fixture = {
    agentDir,
    baseUrl,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    kimiHome,
    kimiHomeOverride: explicitKimiHome ? kimiHome : undefined,
    legacyHome,
    legacyPath,
    projectDir,
    rootDir,
    stablePath,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
  return fixture;
}

export function readKimiConfig(filePath) {
  return import('node:fs/promises')
    .then(({ readFile }) => readFile(filePath, 'utf-8'))
    .then((raw) => ({ raw, config: parseToml(raw) }));
}

export function managedHook(config, event) {
  return config.hooks?.findLast((hook) => hook.event === event && hook.timeout === 10);
}

export function assertManagedHook(hook, event) {
  assert(hook);
  assert.deepEqual(Object.keys(hook).sort(), ['command', 'event', 'timeout']);
  assert.equal(hook.event, event);
  assert.equal(hook.timeout, 10);
  if (process.platform === 'win32') {
    assert.match(hook.command, /^"[^"\r\n]+powershell\.exe" .* -EncodedCommand /i);
  } else {
    assert.match(hook.command, /^'[^']*node[^']*' /i);
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
    requests.push({
      headers: request.headers,
      method: request.method,
      url: request.url,
      raw,
    });
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
