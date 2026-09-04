import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { parse } from 'jsonc-parser';

export const VALID_PRIVATE_KEY = Buffer.alloc(32).toString('base64url');
export const cliPath = path.resolve('dist/cli.js');

export function distUrl(relativePath) {
  return pathToFileURL(path.resolve('dist', relativePath)).href;
}

export const registryModuleUrl = distUrl('plugins/registry.js');
export const managedInstallationModuleUrl = distUrl('plugins/managed-installation.js');

export function runProcess(command, args, env, cwd, input = '', { unset = [], shell = false } = {}) {
  return new Promise((resolve, reject) => {
    const childEnvironment = { ...process.env, ...env };
    for (const [name, value] of Object.entries(env)) {
      if (value === undefined) delete childEnvironment[name];
    }
    for (const name of unset) delete childEnvironment[name];
    const child = spawn(command, args, {
      cwd,
      env: childEnvironment,
      shell,
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

export function runNode(args, env, cwd, input = '', options = {}) {
  return runProcess(process.execPath, args, env, cwd, input, options);
}

export function runShell(command, env, cwd, input = '') {
  return runProcess(command, [], env, cwd, input, { shell: true });
}

export function windowsPowerShellPath() {
  return path.win32.join(
    process.env.SystemRoot || 'C:\\Windows',
    'System32',
    'WindowsPowerShell',
    'v1.0',
    'powershell.exe',
  );
}

export function homeEnvironment(fixture) {
  return { HOME: fixture.homeDir, USERPROFILE: fixture.homeDir };
}

// Sets NAME when a value is configured, otherwise removes it from the child environment.
export function optionalEnvironment(name, value) {
  return value === undefined ? { env: {}, unset: [name] } : { env: { [name]: value }, unset: [] };
}

export function installConfig(agentName, fixture, overrides = {}) {
  return {
    agentName,
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

export function runPlugin(
  plugin,
  fixture,
  method,
  argument,
  { env = {}, unset = [], cwd = fixture.projectDir } = {},
) {
  const source = `
    import { ${plugin.exportName} } from ${JSON.stringify(plugin.moduleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await ${plugin.exportName}[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      ...homeEnvironment(fixture),
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
      ...env,
    },
    cwd,
    '',
    { unset },
  );
}

export async function createFixtureRoot(
  prefix,
  homeName,
  { agentId = 'agent-1', projectName = 'project with spaces', createHome = false } = {},
) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), prefix));
  const homeDir = path.join(rootDir, homeName);
  const projectDir = path.join(rootDir, projectName);
  const agentDir = path.join(homeDir, '.elydora', agentId);
  if (createHome) await mkdir(homeDir, { recursive: true });
  await mkdir(projectDir, { recursive: true });
  return {
    agentDir,
    agentId,
    guardScriptPath: path.join(agentDir, 'guard.js'),
    homeDir,
    hookScriptPath: path.join(agentDir, 'hook.js'),
    projectDir,
    rootDir,
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
}

export async function writeJson(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  const contents = typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`;
  await writeFile(filePath, contents, { encoding: 'utf-8', mode: 0o600 });
}

export async function writeOptionalJson(filePath, value) {
  if (value !== undefined) await writeJson(filePath, value);
}

export async function readJson(filePath) {
  return JSON.parse(await readFile(filePath, 'utf-8'));
}

export async function readSettings(filePath) {
  const raw = await readFile(filePath, 'utf-8');
  return { raw, settings: JSON.parse(raw) };
}

export function parseJsonc(raw) {
  const errors = [];
  const value = parse(raw, errors, { allowTrailingComma: false, disallowComments: false });
  assert.deepEqual(errors, []);
  return value;
}

export async function readJsoncSettings(filePath) {
  const raw = await readFile(filePath, 'utf-8');
  return { raw, settings: parseJsonc(raw) };
}

export async function startApiServer({ status: initialStatus = 'active', operationStatus: initialOperationStatus = 201 } = {}) {
  const requests = [];
  let status = initialStatus;
  let operationStatus = initialOperationStatus;
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
    setStatus(value) { status = value; },
    setOperationStatus(value) { operationStatus = value; },
    close: () => new Promise((resolve, reject) => {
      server.close((error) => error ? reject(error) : resolve());
    }),
  };
}
