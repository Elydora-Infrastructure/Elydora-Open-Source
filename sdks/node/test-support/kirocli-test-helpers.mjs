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

export const pluginModuleUrl = distUrl('plugins/kirocli.js');
export const contractModuleUrl = distUrl('plugins/kirocli-contract.js');
export const installationModuleUrl = distUrl('plugins/kirocli-installation.js');
export const ioModuleUrl = distUrl('plugins/kirocli-io.js');
export const kiroV1ModuleUrl = distUrl('plugins/kiroide-contract.js');
const PLUGIN = { exportName: 'kirocliPlugin', moduleUrl: pluginModuleUrl };

export function runProcess(command, args, env, cwd, input = '', shell = false) {
  return baseProcess(command, args, env, cwd, input, { shell });
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('kirocli', fixture, overrides);
}

export function runPlugin(fixture, method, argument) {
  return basePlugin(PLUGIN, fixture, method, argument);
}

export function managedV2Document(overrides = {}) {
  return {
    name: 'elydora-audit',
    description: 'Kiro CLI with Elydora audit and freeze enforcement',
    tools: ['*'],
    includeMcpJson: true,
    hooks: {},
    ...overrides,
  };
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  existingV2,
  existingV3,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-kirocli-',
    "home with spaces and 'quote %KIRO%",
    { agentId, projectName: 'workspace with spaces', createHome: true },
  );
  const v2Path = path.join(base.homeDir, '.kiro', 'agents', 'elydora-audit.json');
  const v3Path = path.join(base.homeDir, '.kiro', 'hooks', 'elydora-audit.json');
  await writeOptionalJson(v2Path, existingV2);
  await writeOptionalJson(v3Path, existingV3);
  return {
    ...base,
    baseUrl,
    v2Path,
    v3Path,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
  };
}

export function findV3Hook(document, name) {
  return document.hooks.find((hook) => hook.name === name);
}
