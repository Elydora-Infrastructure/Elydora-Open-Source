import path from 'node:path';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  runPlugin as basePlugin,
  runShell,
  writeOptionalJson,
} from './harness.mjs';

export {
  VALID_PRIVATE_KEY,
  cliPath,
  managedInstallationModuleUrl,
  readSettings,
  registryModuleUrl,
  runNode,
  runProcess,
  startApiServer,
  writeJson,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/augment.js');
export const ioModuleUrl = distUrl('plugins/augment-io.js');
export const installationModuleUrl = distUrl('plugins/augment-installation.js');
export const wrapperExtension = process.platform === 'win32' ? '.cmd' : '.sh';
export const guardWrapperName = `augment-guard${wrapperExtension}`;
export const auditWrapperName = `augment-hook${wrapperExtension}`;
const PLUGIN = { exportName: 'augmentPlugin', moduleUrl: pluginModuleUrl };

export function runHandler(handler, input, fixture) {
  return runShell(handler.command, homeEnvironment(fixture), fixture.projectDir, input);
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('augment', fixture, overrides);
}

export function runPlugin(fixture, method, argument) {
  return basePlugin(PLUGIN, fixture, method, argument);
}

export async function createFixture({ baseUrl = 'http://127.0.0.1:9', settings } = {}) {
  const base = await createFixtureRoot('elydora-augment-', "home with spaces and 'quote %AUGGIE%");
  const settingsPath = path.join(base.homeDir, '.augment', 'settings.json');
  await writeOptionalJson(settingsPath, settings);
  return {
    ...base,
    auditWrapperPath: path.join(base.agentDir, auditWrapperName),
    baseUrl,
    guardWrapperPath: path.join(base.agentDir, guardWrapperName),
    settingsPath,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
  };
}

export function generatedCommand(wrapperPath) {
  if (process.platform === 'win32') return `"${wrapperPath.replaceAll('"', '\\"')}"`;
  return `'${wrapperPath.replaceAll("'", `'"'"'`)}'`;
}

export function managedHandler(settings, event, wrapperPath) {
  const command = generatedCommand(wrapperPath);
  for (const group of settings.hooks?.[event] ?? []) {
    const handler = group.hooks.find((candidate) => candidate.command === command);
    if (handler) return handler;
  }
  return undefined;
}
