import assert from 'node:assert/strict';
import path from 'node:path';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  runPlugin as basePlugin,
  runProcess as baseProcess,
  windowsPowerShellPath,
  writeOptionalJson,
} from './harness.mjs';

export {
  VALID_PRIVATE_KEY,
  cliPath,
  parseJsonc as parseSettings,
  readJsoncSettings as readSettings,
  registryModuleUrl,
  startApiServer,
  writeJson as writeSettings,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/qwen.js');
export const sourcesModuleUrl = distUrl('plugins/qwen-sources.js');
export const installationModuleUrl = distUrl('plugins/qwen-installation.js');
export const configModuleUrl = distUrl('plugins/qwen-config.js');
export const contractModuleUrl = distUrl('plugins/qwen-contract.js');
const PLUGIN = { exportName: 'qwenPlugin', moduleUrl: pluginModuleUrl };

const QWEN_ENV_KEYS = [
  'QWEN_HOME',
  'QWEN_RUNTIME_DIR',
  'QWEN_CODE_SYSTEM_SETTINGS_PATH',
  'QWEN_CODE_SYSTEM_DEFAULTS_PATH',
  'QWEN_CODE_TRUSTED_FOLDERS_PATH',
];

export function runProcess(command, args, env, cwd, input = '', unset = []) {
  return baseProcess(command, args, env, cwd, input, { unset });
}

export function runNode(args, env, cwd, input = '', unset = []) {
  return runProcess(process.execPath, args, env, cwd, input, unset);
}

export function runQwenHook(handler, input, fixture, environment = {}) {
  const env = { ...homeEnvironment(fixture), ...environment };
  if (process.platform === 'win32') {
    return runProcess(
      windowsPowerShellPath(),
      ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', handler.command],
      env,
      fixture.projectDir,
      input,
    );
  }
  return runProcess('/bin/bash', ['-c', handler.command], env, fixture.projectDir, input);
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('qwen', fixture, overrides);
}

export function runPlugin(fixture, method, argument, environment = {}) {
  const env = {
    QWEN_CODE_SYSTEM_DEFAULTS_PATH: path.join(fixture.rootDir, 'system-defaults.json'),
    QWEN_CODE_SYSTEM_SETTINGS_PATH: path.join(fixture.rootDir, 'system-settings.json'),
    ...environment,
  };
  const unset = QWEN_ENV_KEYS.filter((key) => !Object.hasOwn(env, key));
  return basePlugin(PLUGIN, fixture, method, argument, { env, unset });
}

export async function createFixture({ baseUrl = 'http://127.0.0.1:9', settings } = {}) {
  const base = await createFixtureRoot(
    'elydora-qwen-',
    "home with spaces and 'quote $QWEN_PROJECT_DIR %QWEN_PROJECT_DIR%",
    { createHome: true },
  );
  const settingsPath = path.join(base.homeDir, '.qwen', 'settings.json');
  await writeOptionalJson(settingsPath, settings);
  return {
    ...base,
    baseUrl,
    settingsPath,
    install(overrides = {}, environment = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides), environment);
    },
  };
}

export function managedHandler(settings, event, name) {
  for (const group of settings.hooks?.[event] ?? []) {
    if (Object.keys(group).join('|') !== 'hooks' || !Array.isArray(group.hooks)) continue;
    const handler = group.hooks.find((candidate) => (
      Object.keys(candidate).sort().join('|') === 'command|name|shell|timeout|type'
      && candidate.type === 'command'
      && candidate.name === name
      && candidate.shell === (process.platform === 'win32' ? 'powershell' : 'bash')
      && candidate.timeout === 10_000
    ));
    if (handler) return handler;
  }
  return undefined;
}

export function assertManagedHandler(handler, name) {
  assert(handler);
  assert.deepEqual(Object.keys(handler).sort(), [
    'command',
    'name',
    'shell',
    'timeout',
    'type',
  ]);
  assert.equal(handler.type, 'command');
  assert.equal(handler.name, name);
  assert.equal(handler.shell, process.platform === 'win32' ? 'powershell' : 'bash');
  assert.equal(handler.timeout, 10_000);
}

function quoteShell(value) {
  return process.platform === 'win32'
    ? `'${value.replaceAll("'", "''")}'`
    : `'${value.replaceAll("'", `'"'"'`)}'`;
}

export function generatedCommand(scriptPath) {
  const invocation = `${quoteShell(process.execPath)} ${quoteShell(scriptPath)}`;
  return process.platform === 'win32'
    ? `& ${invocation}; exit $LASTEXITCODE`
    : invocation;
}

export function legacyGroup(scriptPath) {
  return {
    matcher: '*',
    hooks: [{
      type: 'command',
      command: generatedCommand(scriptPath),
      shell: process.platform === 'win32' ? 'powershell' : 'bash',
      timeout: 10_000,
    }],
  };
}
