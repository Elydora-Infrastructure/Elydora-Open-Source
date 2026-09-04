import path from 'node:path';
import {
  createFixtureRoot,
  distUrl,
  installConfig as baseInstallConfig,
  runPlugin as basePlugin,
  runProcess as baseProcess,
  writeOptionalJson,
} from './harness.mjs';

export {
  VALID_PRIVATE_KEY,
  cliPath,
  homeEnvironment as environment,
  readJson,
  registryModuleUrl,
  runNode,
  runShell,
  startApiServer,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/kiroide.js');
export const contractModuleUrl = distUrl('plugins/kiroide-contract.js');
export const installationModuleUrl = distUrl('plugins/kiroide-installation.js');
export const ioModuleUrl = distUrl('plugins/kiroide-io.js');
const PLUGIN = { exportName: 'kiroidePlugin', moduleUrl: pluginModuleUrl };

export function runProcess(command, args, env, cwd, input = '', shell = false) {
  return baseProcess(command, args, env, cwd, input, { shell });
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('kiroide', fixture, overrides);
}

export function runPlugin(fixture, method, argument) {
  return basePlugin(PLUGIN, fixture, method, argument);
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  existingConfig,
  existingLegacy,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-kiroide-',
    "home with spaces and 'quote %KIRO%",
    { agentId, projectName: 'workspace with spaces' },
  );
  const hooksDir = path.join(base.projectDir, '.kiro', 'hooks');
  const configPath = path.join(hooksDir, 'elydora-audit.json');
  const legacyConfigPath = path.join(base.homeDir, '.kiro', 'hooks', 'elydora-audit.kiro.hook');
  await writeOptionalJson(configPath, existingConfig);
  await writeOptionalJson(legacyConfigPath, existingLegacy);
  return {
    ...base,
    baseUrl,
    configPath,
    hooksDir,
    legacyConfigPath,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
  };
}

export function findHook(document, name) {
  return document.hooks.find((hook) => hook.name === name);
}
