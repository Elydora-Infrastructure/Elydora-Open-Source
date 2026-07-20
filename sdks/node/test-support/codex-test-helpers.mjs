import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const VALID_PRIVATE_KEY = Buffer.alloc(32).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/codex.js')).href;
export const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
export const ioModuleUrl = pathToFileURL(path.resolve('dist/plugins/codex-io.js')).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/codex-installation.js'),
).href;

export function runNode(args, env, cwd, input = '') {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, args, {
      cwd,
      env: { ...process.env, CODEX_HOME: '', ...env },
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

export function runHook(handler, input, fixture, environment = {}) {
  const command = process.platform === 'win32' ? handler.commandWindows : handler.command;
  return new Promise((resolve, reject) => {
    const child = process.platform === 'win32'
      ? spawn(command, {
        shell: true,
        cwd: fixture.projectDir,
        env: {
          ...process.env,
          CODEX_HOME: '',
          HOME: fixture.homeDir,
          USERPROFILE: fixture.homeDir,
          ...environment,
        },
        stdio: ['pipe', 'pipe', 'pipe'],
      })
      : spawn('/bin/sh', ['-c', command], {
        cwd: fixture.projectDir,
        env: {
          ...process.env,
          CODEX_HOME: '',
          HOME: fixture.homeDir,
          USERPROFILE: fixture.homeDir,
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

export async function writeJson(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  const contents = typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`;
  await writeFile(filePath, contents, { encoding: 'utf-8', mode: 0o600 });
}

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'codex',
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
    import { codexPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await codexPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
      ...environment,
    },
    fixture.projectDir,
  );
}

export async function createFixture({ existingConfig, baseUrl = 'http://127.0.0.1:9' } = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-codex-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %ELYDORA_HOOK_PATH%");
  const projectDir = path.join(rootDir, 'project with spaces');
  const configPath = path.join(homeDir, '.codex', 'hooks.json');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(projectDir, { recursive: true });
  if (existingConfig !== undefined) await writeJson(configPath, existingConfig);
  return {
    agentDir,
    baseUrl,
    configPath,
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
}

export function managedHandler(config, event, statusMessage) {
  for (const group of config.hooks?.[event] ?? []) {
    const handler = group.hooks?.find((item) => item.statusMessage === statusMessage);
    if (handler) return { group, handler };
  }
  return undefined;
}

export function assertNativeHandler(value, statusMessage) {
  assert(value);
  assert.deepEqual(Object.keys(value.group).sort(), ['hooks', 'matcher']);
  assert.equal(value.group.matcher, '*');
  assert.deepEqual(
    Object.keys(value.handler).sort(),
    ['command', 'commandWindows', 'statusMessage', 'timeout', 'type'],
  );
  assert.equal(value.handler.type, 'command');
  assert.equal(value.handler.timeout, 10);
  assert.equal(value.handler.statusMessage, statusMessage);
  assert.match(value.handler.command, /node(?:\.exe)?/i);
  assert.match(value.handler.commandWindows, /^"[^"]+powershell\.exe" .* -EncodedCommand /i);
}

export function legacyHandler(scriptPath, statusMessage) {
  const quotePosix = (value) => `'${value.replaceAll("'", `'"'"'`)}'`;
  return {
    type: 'command',
    command: `${quotePosix(process.execPath)} ${quotePosix(scriptPath)}`,
    commandWindows: `"${process.execPath}" "${scriptPath}"`,
    timeout: 10,
    statusMessage,
  };
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
