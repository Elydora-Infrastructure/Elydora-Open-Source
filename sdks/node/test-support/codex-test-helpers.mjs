import assert from 'node:assert/strict';
import path from 'node:path';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  runNode as baseNode,
  runPlugin as basePlugin,
  runProcess,
  runShell,
  writeOptionalJson,
} from './harness.mjs';

export { VALID_PRIVATE_KEY, registryModuleUrl, startApiServer, writeJson } from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/codex.js');
export const ioModuleUrl = distUrl('plugins/codex-io.js');
export const installationModuleUrl = distUrl('plugins/codex-installation.js');
const PLUGIN = { exportName: 'codexPlugin', moduleUrl: pluginModuleUrl };

export function runNode(args, env, cwd, input = '') {
  return baseNode(args, { CODEX_HOME: '', ...env }, cwd, input);
}

export function runHook(handler, input, fixture, environment = {}) {
  const command = process.platform === 'win32' ? handler.commandWindows : handler.command;
  const env = { CODEX_HOME: '', ...homeEnvironment(fixture), ...environment };
  return process.platform === 'win32'
    ? runShell(command, env, fixture.projectDir, input)
    : runProcess('/bin/sh', ['-c', command], env, fixture.projectDir, input);
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('codex', fixture, overrides);
}

export function runPlugin(fixture, method, argument, environment = {}) {
  return basePlugin(PLUGIN, fixture, method, argument, { env: { CODEX_HOME: '', ...environment } });
}

export async function createFixture({ existingConfig, baseUrl = 'http://127.0.0.1:9' } = {}) {
  const base = await createFixtureRoot(
    'elydora-codex-',
    "home with spaces and 'quote %ELYDORA_HOOK_PATH%",
  );
  const configPath = path.join(base.homeDir, '.codex', 'hooks.json');
  await writeOptionalJson(configPath, existingConfig);
  return {
    ...base,
    baseUrl,
    configPath,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
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
