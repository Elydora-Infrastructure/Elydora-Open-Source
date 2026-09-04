import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  runPlugin as basePlugin,
} from './harness.mjs';

export {
  VALID_PRIVATE_KEY,
  cliPath,
  readJson,
  registryModuleUrl,
  runNode,
  runProcess,
  startApiServer,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/opencode.js');
export const contractModuleUrl = distUrl('plugins/opencode-contract.js');
export const installationModuleUrl = distUrl('plugins/opencode-installation.js');
export const ioModuleUrl = distUrl('plugins/opencode-io.js');
const PLUGIN = { exportName: 'opencodePlugin', moduleUrl: pluginModuleUrl };

export function environment(fixture) {
  return { ...homeEnvironment(fixture), XDG_CONFIG_HOME: fixture.configRoot };
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('opencode', fixture, overrides);
}

export function runPlugin(fixture, method, argument) {
  return basePlugin(PLUGIN, fixture, method, argument, { env: environment(fixture) });
}

async function writeOptional(filePath, value) {
  if (value === undefined) return;
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, value, { mode: 0o600 });
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  existingPlugin,
  existingLegacy,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-opencode-',
    "home with spaces and 'quote %OPEN%",
    { agentId, projectName: 'workspace with spaces', createHome: true },
  );
  const configRoot = path.join(base.rootDir, 'xdg config');
  const pluginsDir = path.join(configRoot, 'opencode', 'plugins');
  const pluginPath = path.join(pluginsDir, 'elydora-audit.js');
  const legacyPluginPath = path.join(pluginsDir, 'elydora-audit.mjs');
  await writeOptional(pluginPath, existingPlugin);
  await writeOptional(legacyPluginPath, existingLegacy);
  return {
    ...base,
    baseUrl,
    configRoot,
    legacyPluginPath,
    pluginPath,
    pluginsDir,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
    async loadPlugin() {
      const module = await import(`${pathToFileURL(this.pluginPath).href}?test=${Date.now()}`);
      return module.ElydoraAuditPlugin({});
    },
  };
}
