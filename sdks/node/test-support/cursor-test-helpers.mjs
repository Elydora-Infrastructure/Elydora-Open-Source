import assert from 'node:assert/strict';
import path from 'node:path';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  runPlugin as basePlugin,
  runProcess,
  writeOptionalJson,
} from './harness.mjs';

export {
  VALID_PRIVATE_KEY,
  registryModuleUrl,
  runNode,
  startApiServer,
  writeJson,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/cursor.js');
export const ioModuleUrl = distUrl('plugins/cursor-io.js');
export const contractModuleUrl = distUrl('plugins/cursor-contract.js');
export const installationModuleUrl = distUrl('plugins/cursor-installation.js');
const PLUGIN = { exportName: 'cursorPlugin', moduleUrl: pluginModuleUrl };

export function runHook(handler, input, fixture, environment = {}) {
  const executable = process.platform === 'win32' ? 'powershell.exe' : '/bin/sh';
  const args = process.platform === 'win32'
    ? ['-NoProfile', '-NonInteractive', '-Command', handler.command]
    : ['-c', handler.command];
  return runProcess(executable, args, { ...homeEnvironment(fixture), ...environment }, undefined, input);
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('cursor', fixture, overrides);
}

export function runPlugin(fixture, method, argument) {
  return basePlugin(PLUGIN, fixture, method, argument);
}

export async function createFixture({ existingConfig, baseUrl = 'http://127.0.0.1:9' } = {}) {
  const base = await createFixtureRoot('elydora-cursor-', "home with spaces and 'quote");
  const configPath = path.join(base.homeDir, '.cursor', 'hooks.json');
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

export function managedHandler(config, event, scriptName) {
  return config.hooks?.[event]?.find((handler) => handler.command?.includes(scriptName));
}

export function assertNativeHandler(handler) {
  assert.deepEqual(Object.keys(handler).sort(), ['command', 'failClosed', 'timeout']);
  assert.equal(handler.failClosed, true);
  assert.equal(handler.timeout, 10);
  assert.match(handler.command, /node(?:\.exe)?/i);
  if (process.platform === 'win32') {
    assert.match(handler.command, /^& /);
    assert.match(handler.command, /; exit \$LASTEXITCODE$/);
  } else {
    assert.match(handler.command, /^'/);
  }
}

export function legacyHandler(scriptPath) {
  return { command: `node "${scriptPath}"` };
}
