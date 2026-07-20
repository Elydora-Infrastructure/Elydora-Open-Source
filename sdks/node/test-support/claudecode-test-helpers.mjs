import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const VALID_PRIVATE_KEY = Buffer.alloc(32).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/claudecode.js')).href;
export const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
export const ioModuleUrl = pathToFileURL(path.resolve('dist/plugins/claudecode-io.js')).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/claudecode-installation.js'),
).href;
export const cliPath = path.resolve('dist/cli.js');

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

export function runClaudeHook(handler, input, fixture, environment = {}) {
  return runProcess(
    handler.command,
    handler.args,
    {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      CLAUDE_CONFIG_DIR: fixture.claudeConfigOverride ?? '',
      ...environment,
    },
    fixture.projectDir,
    input,
  );
}

export async function writeJson(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  const contents = typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`;
  await writeFile(filePath, contents, { encoding: 'utf-8', mode: 0o600 });
}

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'claudecode',
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
    import { claudecodePlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await claudecodePlugin[process.env.ELYDORA_TEST_METHOD](argument);
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
  if (fixture.claudeConfigOverride === undefined) unset.push('CLAUDE_CONFIG_DIR');
  else env.CLAUDE_CONFIG_DIR = fixture.claudeConfigOverride;
  return runNode(
    ['--input-type=module', '--eval', source],
    env,
    fixture.projectDir,
    '',
    unset,
  );
}

export async function createFixture({
  baseUrl = 'http://127.0.0.1:9',
  settings,
  explicitClaudeConfig = true,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-claude-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %CLAUDE_HOOK_EVENT%");
  const projectDir = path.join(rootDir, 'project with spaces');
  const claudeConfigDir = explicitClaudeConfig
    ? path.join(homeDir, 'custom claude')
    : path.join(homeDir, '.claude');
  const settingsPath = path.join(claudeConfigDir, 'settings.json');
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(projectDir, { recursive: true });
  if (settings !== undefined) await writeJson(settingsPath, settings);
  const fixture = {
    agentDir,
    baseUrl,
    claudeConfigDir,
    claudeConfigOverride: explicitClaudeConfig ? claudeConfigDir : undefined,
    guardScriptPath,
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

export function managedHandler(settings, event) {
  for (const group of settings.hooks?.[event] ?? []) {
    if (Object.keys(group).join('|') !== 'hooks' || !Array.isArray(group.hooks)) continue;
    const handler = group.hooks.find((candidate) => (
      Object.keys(candidate).sort().join('|') === 'args|command|statusMessage|timeout|type'
      && candidate.type === 'command'
      && candidate.timeout === 10
    ));
    if (handler) return handler;
  }
  return undefined;
}

export function assertManagedHandler(handler, scriptPath, statusMessage) {
  assert(handler);
  assert.deepEqual(
    Object.keys(handler).sort(),
    ['args', 'command', 'statusMessage', 'timeout', 'type'],
  );
  assert.equal(handler.type, 'command');
  assert.equal(handler.command, process.execPath);
  assert.deepEqual(handler.args, [scriptPath]);
  assert.equal(handler.timeout, 10);
  assert.equal(handler.statusMessage, statusMessage);
}

export function legacyHandler(scriptPath) {
  return { type: 'command', command: `node "${scriptPath}"` };
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
