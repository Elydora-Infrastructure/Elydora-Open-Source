import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const VALID_PRIVATE_KEY = Buffer.alloc(32, 7).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/cline.js')).href;
export const contractModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/cline-contract.js'),
).href;
export const ioModuleUrl = pathToFileURL(path.resolve('dist/plugins/cline-io.js')).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/cline-installation.js'),
).href;
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

export function environment(fixture, clineDir = fixture.clineDir) {
  return {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    CLINE_DIR: clineDir === null ? undefined : clineDir,
  };
}

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'cline',
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

export async function runPlugin(fixture, method, argument, clineDir = fixture.clineDir) {
  const source = `
    import { clinePlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await clinePlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      ...environment(fixture, clineDir),
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    },
    fixture.projectDir,
  );
}

async function writeOptional(filePath, contents) {
  if (contents === undefined) return;
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, contents, { mode: 0o600 });
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  existingAudit,
  existingGuard,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-cline-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %CLINE%");
  const projectDir = path.join(rootDir, 'project with spaces');
  const clineDir = path.join(rootDir, 'custom-cline-home');
  const hooksDir = path.join(clineDir, 'hooks');
  const guardWrapperPath = path.join(hooksDir, 'PreToolUse.mjs');
  const auditWrapperPath = path.join(hooksDir, 'PostToolUse.mjs');
  const agentDir = path.join(homeDir, '.elydora', agentId);
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(projectDir, { recursive: true });
  await writeOptional(guardWrapperPath, existingGuard);
  await writeOptional(auditWrapperPath, existingAudit);
  const fixture = {
    agentDir,
    agentId,
    auditWrapperPath,
    baseUrl,
    clineDir,
    guardScriptPath,
    guardWrapperPath,
    homeDir,
    hookScriptPath,
    hooksDir,
    projectDir,
    rootDir,
    install(overrides = {}, clineDirOverride = this.clineDir) {
      return runPlugin(this, 'install', installConfig(this, overrides), clineDirOverride);
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
  return fixture;
}

export async function readJson(filePath) {
  return JSON.parse(await readFile(filePath, 'utf-8'));
}

export async function startApiServer() {
  const requests = [];
  const server = http.createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const raw = Buffer.concat(chunks).toString('utf-8');
    requests.push({ method: request.method, url: request.url, raw });
    response.writeHead(201, { 'Content-Type': 'application/json' });
    response.end('{"operation":{"accepted":true}}');
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
