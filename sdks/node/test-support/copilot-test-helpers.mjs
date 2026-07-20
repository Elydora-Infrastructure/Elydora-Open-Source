import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const VALID_PRIVATE_KEY = Buffer.alloc(32, 11).toString('base64url');
export const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/copilot.js')).href;
export const contractModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/copilot-contract.js'),
).href;
export const installationModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/copilot-installation.js'),
).href;
export const ioModuleUrl = pathToFileURL(path.resolve('dist/plugins/copilot-io.js')).href;
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

export function environment(fixture, copilotHome = fixture.copilotHome) {
  return {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    COPILOT_HOME: copilotHome === null ? undefined : copilotHome,
  };
}

export function installConfig(fixture, overrides = {}) {
  return {
    agentName: 'copilot',
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

export async function runPlugin(fixture, method, argument, copilotHome = fixture.copilotHome) {
  const source = `
    import { copilotPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await copilotPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      ...environment(fixture, copilotHome),
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    },
    fixture.projectDir,
  );
}

export function runHook(handler, fixture, input) {
  const command = process.platform === 'win32'
    ? ['powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', handler.powershell]]
    : ['/bin/sh', ['-c', handler.bash]];
  return runProcess(command[0], command[1], environment(fixture), fixture.projectDir, input);
}

export async function writeJson(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  const source = typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`;
  await writeFile(filePath, source, { mode: 0o600 });
}

async function writeOptional(filePath, value) {
  if (value !== undefined) await writeJson(filePath, value);
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  userConfig,
  legacyConfig,
  userSettings,
  legacyUserConfig,
  claudeSettings,
  claudeLocalSettings,
  repositorySettings,
  localSettings,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-copilot-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote %COPILOT%");
  const projectDir = path.join(rootDir, 'project with spaces');
  const copilotHome = path.join(rootDir, "custom Copilot 'home");
  const hooksDir = path.join(copilotHome, 'hooks');
  const configPath = path.join(hooksDir, 'elydora-audit.json');
  const legacyPath = path.join(projectDir, '.github', 'hooks', 'hooks.json');
  const agentDir = path.join(homeDir, '.elydora', agentId);
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(projectDir, { recursive: true });
  await Promise.all([
    writeOptional(configPath, userConfig),
    writeOptional(legacyPath, legacyConfig),
    writeOptional(path.join(copilotHome, 'settings.json'), userSettings),
    writeOptional(path.join(copilotHome, 'config.json'), legacyUserConfig),
    writeOptional(path.join(projectDir, '.claude', 'settings.json'), claudeSettings),
    writeOptional(path.join(projectDir, '.claude', 'settings.local.json'), claudeLocalSettings),
    writeOptional(path.join(projectDir, '.github', 'copilot', 'settings.json'), repositorySettings),
    writeOptional(path.join(projectDir, '.github', 'copilot', 'settings.local.json'), localSettings),
  ]);
  return {
    agentDir,
    agentId,
    baseUrl,
    configPath,
    copilotHome,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    hooksDir,
    legacyPath,
    projectDir,
    rootDir,
    install(overrides = {}, copilotHomeOverride = this.copilotHome) {
      return runPlugin(this, 'install', installConfig(this, overrides), copilotHomeOverride);
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
}

export function managedHandler(config, event, scriptName) {
  return config.hooks?.[event]?.find(
    (handler) => handler.bash?.includes(scriptName) || handler.powershell?.includes(scriptName),
  );
}

export function assertNativeHandler(handler) {
  if (!handler) throw new Error('Managed handler is missing');
  const keys = ['bash', 'powershell', 'timeoutSec', 'type'];
  if (JSON.stringify(Object.keys(handler).sort()) !== JSON.stringify(keys)) {
    throw new Error(`Unexpected handler fields: ${Object.keys(handler).join(', ')}`);
  }
  if (handler.type !== 'command' || handler.timeoutSec !== 10) {
    throw new Error(`Unexpected handler contract: ${JSON.stringify(handler)}`);
  }
}

export function legacyManagedConfig(fixture, extraHooks = {}) {
  return {
    version: 1,
    hooks: {
      preToolUse: [{
        type: 'command',
        bash: `node "${fixture.guardScriptPath}"`,
        powershell: `node "${fixture.guardScriptPath}"`,
        timeoutSec: 5,
      }],
      postToolUse: [{
        type: 'command',
        bash: `node "${fixture.hookScriptPath}"`,
        powershell: `node "${fixture.hookScriptPath}"`,
        timeoutSec: 5,
      }],
      ...extraHooks,
    },
  };
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
