import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { parse } from 'jsonc-parser';

export const VALID_PRIVATE_KEY = Buffer.alloc(32, 13).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/droid.js')).href;
export const configModuleUrl = pathToFileURL(path.resolve('dist/plugins/droid-config.js')).href;
export const contractModuleUrl = pathToFileURL(path.resolve('dist/plugins/droid-contract.js')).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/droid-installation.js'),
).href;
export const ioModuleUrl = pathToFileURL(path.resolve('dist/plugins/droid-io.js')).href;
export const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
export const cliPath = path.resolve('dist/cli.js');

export function runProcess(command, args, env, cwd, input = '', shell = false) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      env: { ...process.env, ...env },
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

export function runNode(args, env, cwd, input = '') {
  return runProcess(process.execPath, args, env, cwd, input);
}

export function environment(fixture) {
  return { HOME: fixture.homeDir, USERPROFILE: fixture.homeDir };
}

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'droid',
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
    import { droidPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await droidPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      ...environment(fixture),
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    },
    fixture.workspaceDir,
  );
}

export function runHook(command, fixture, input) {
  if (process.platform === 'win32') {
    return runProcess(
      'powershell.exe',
      ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', command],
      environment(fixture),
      fixture.workspaceDir,
      input,
    );
  }
  return runProcess('/bin/sh', ['-c', command], environment(fixture), fixture.workspaceDir, input);
}

export async function writeConfig(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  const source = typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`;
  await writeFile(filePath, source, { mode: 0o600 });
}

async function writeOptional(filePath, value) {
  if (value !== undefined) await writeConfig(filePath, value);
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  rootConfig,
  legacyConfig,
  settings,
  localSettings,
  projectSettings,
  projectLocalSettings,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-droid-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %DROID%");
  const workspaceDir = path.join(rootDir, 'workspace with spaces');
  const factoryDir = path.join(homeDir, '.factory');
  const rootPath = path.join(factoryDir, 'hooks.json');
  const legacyPath = path.join(factoryDir, 'hooks', 'hooks.json');
  const settingsPath = path.join(factoryDir, 'settings.json');
  const localSettingsPath = path.join(factoryDir, 'settings.local.json');
  const agentDir = path.join(homeDir, '.elydora', agentId);
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  const projectFactoryDir = path.join(workspaceDir, '.factory');
  const projectSettingsPath = path.join(projectFactoryDir, 'settings.json');
  const projectLocalSettingsPath = path.join(projectFactoryDir, 'settings.local.json');
  await mkdir(path.join(workspaceDir, '.git'), { recursive: true });
  await Promise.all([
    writeOptional(rootPath, rootConfig),
    writeOptional(legacyPath, legacyConfig),
    writeOptional(settingsPath, settings),
    writeOptional(localSettingsPath, localSettings),
    writeOptional(projectSettingsPath, projectSettings),
    writeOptional(projectLocalSettingsPath, projectLocalSettings),
  ]);
  return {
    agentDir,
    agentId,
    baseUrl,
    factoryDir,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    legacyPath,
    localSettingsPath,
    projectLocalSettingsPath,
    projectSettingsPath,
    rootDir,
    rootPath,
    settingsPath,
    workspaceDir,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
}

export function readJsoncSource(source) {
  const errors = [];
  const value = parse(source, errors, { allowTrailingComma: true });
  if (errors.length > 0) throw new Error(`Unexpected JSONC parse errors: ${JSON.stringify(errors)}`);
  return value;
}

export async function readJsonc(filePath) {
  return readJsoncSource(await readFile(filePath, 'utf-8'));
}

export function managedGroup(hooks, event, scriptName) {
  return hooks?.[event]?.find((group) => group.hooks?.some(
    (handler) => handler.command?.includes(scriptName),
  ));
}

export function managedHandler(hooks, event, scriptName) {
  return managedGroup(hooks, event, scriptName)?.hooks.find(
    (handler) => handler.command?.includes(scriptName),
  );
}

export function assertNativeGroup(group) {
  if (!group) throw new Error('Managed group is missing');
  if (JSON.stringify(Object.keys(group).sort()) !== JSON.stringify(['hooks', 'matcher'])) {
    throw new Error(`Unexpected group fields: ${Object.keys(group).join(', ')}`);
  }
  if (group.matcher !== '*' || group.hooks.length !== 1) {
    throw new Error(`Unexpected group contract: ${JSON.stringify(group)}`);
  }
  const handler = group.hooks[0];
  if (JSON.stringify(Object.keys(handler).sort()) !== JSON.stringify(['command', 'timeout', 'type'])) {
    throw new Error(`Unexpected handler fields: ${Object.keys(handler).join(', ')}`);
  }
  if (handler.type !== 'command' || handler.timeout !== 10) {
    throw new Error(`Unexpected handler contract: ${JSON.stringify(handler)}`);
  }
}

export async function assertMissing(filePath) {
  await assert.rejects(readFile(filePath), { code: 'ENOENT' });
}

export async function assertNoTransactionFiles(fixture) {
  const names = await readdir(fixture.rootDir, { recursive: true });
  const leaked = names.filter((name) => /\.(tmp|rollback)$/.test(name));
  if (leaked.length > 0) throw new Error(`Leaked transaction files: ${leaked.join(', ')}`);
}

export async function startApiServer() {
  const requests = [];
  const server = http.createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const raw = Buffer.concat(chunks).toString('utf-8');
    requests.push({ method: request.method, url: request.url, raw });
    response.writeHead(request.method === 'POST' ? 201 : 200, {
      'Content-Type': 'application/json',
    });
    response.end(request.method === 'POST'
      ? '{"operation":{"accepted":true}}'
      : '{"agent":{"status":"active"}}');
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
