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
  cliPath,
  homeEnvironment as environment,
  readJson as readSettings,
  registryModuleUrl,
  runNode,
  runProcess,
  startApiServer,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/letta.js');
export const configModuleUrl = distUrl('plugins/letta-config.js');
export const contractModuleUrl = distUrl('plugins/letta-contract.js');
export const installationModuleUrl = distUrl('plugins/letta-installation.js');
export const sourcesModuleUrl = distUrl('plugins/letta-sources.js');
const PLUGIN = { exportName: 'lettaPlugin', moduleUrl: pluginModuleUrl };

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('letta', fixture, overrides);
}

export function runPlugin(fixture, method, argument) {
  return basePlugin(PLUGIN, fixture, method, argument);
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  globalSettings,
  projectSettings,
  localSettings,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-letta-',
    "home with spaces and 'quote %HOME%",
    { agentId, projectName: 'workspace with spaces', createHome: true },
  );
  const globalPath = path.join(base.homeDir, '.letta', 'settings.json');
  const projectPath = path.join(base.projectDir, '.letta', 'settings.json');
  const localPath = path.join(base.projectDir, '.letta', 'settings.local.json');
  await writeOptionalJson(globalPath, globalSettings);
  await writeOptionalJson(projectPath, projectSettings);
  await writeOptionalJson(localPath, localSettings);
  return {
    ...base,
    baseUrl,
    globalPath,
    localPath,
    projectPath,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
  };
}

export function managedHandler(settings, event) {
  return settings.hooks[event].find((group) => (
    group.matcher === '*'
    && group.hooks.length === 1
    && group.hooks[0].timeout === 10_000
  ))?.hooks[0];
}

export function runLettaHook(handler, input, fixture) {
  const shell = process.platform === 'win32' ? 'powershell.exe' : '/bin/sh';
  const args = process.platform === 'win32'
    ? ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', handler.command]
    : ['-c', handler.command];
  return runProcess(shell, args, homeEnvironment(fixture), fixture.projectDir, input);
}
