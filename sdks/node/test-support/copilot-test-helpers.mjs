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
  readJson,
  registryModuleUrl,
  runNode,
  runProcess,
  startApiServer,
  writeJson,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/copilot.js');
export const contractModuleUrl = distUrl('plugins/copilot-contract.js');
export const installationModuleUrl = distUrl('plugins/copilot-installation.js');
export const ioModuleUrl = distUrl('plugins/copilot-io.js');
const PLUGIN = { exportName: 'copilotPlugin', moduleUrl: pluginModuleUrl };

export function environment(fixture, copilotHome = fixture.copilotHome) {
  return {
    ...homeEnvironment(fixture),
    COPILOT_HOME: copilotHome === null ? undefined : copilotHome,
  };
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('copilot', fixture, overrides);
}

export function runPlugin(fixture, method, argument, copilotHome = fixture.copilotHome) {
  return basePlugin(PLUGIN, fixture, method, argument, { env: environment(fixture, copilotHome) });
}

export function runHook(handler, fixture, input) {
  const command = process.platform === 'win32'
    ? ['powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', handler.powershell]]
    : ['/bin/sh', ['-c', handler.bash]];
  return runProcess(command[0], command[1], environment(fixture), fixture.projectDir, input);
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
  const base = await createFixtureRoot(
    'elydora-copilot-',
    "home with spaces and 'quote %COPILOT%",
    { agentId },
  );
  const { projectDir, rootDir } = base;
  const copilotHome = path.join(rootDir, "custom Copilot 'home");
  const hooksDir = path.join(copilotHome, 'hooks');
  const configPath = path.join(hooksDir, 'elydora-audit.json');
  const legacyPath = path.join(projectDir, '.github', 'hooks', 'hooks.json');
  await Promise.all([
    writeOptionalJson(configPath, userConfig),
    writeOptionalJson(legacyPath, legacyConfig),
    writeOptionalJson(path.join(copilotHome, 'settings.json'), userSettings),
    writeOptionalJson(path.join(copilotHome, 'config.json'), legacyUserConfig),
    writeOptionalJson(path.join(projectDir, '.claude', 'settings.json'), claudeSettings),
    writeOptionalJson(path.join(projectDir, '.claude', 'settings.local.json'), claudeLocalSettings),
    writeOptionalJson(path.join(projectDir, '.github', 'copilot', 'settings.json'), repositorySettings),
    writeOptionalJson(path.join(projectDir, '.github', 'copilot', 'settings.local.json'), localSettings),
  ]);
  return {
    ...base,
    baseUrl,
    configPath,
    copilotHome,
    hooksDir,
    legacyPath,
    install(overrides = {}, copilotHomeOverride = this.copilotHome) {
      return runPlugin(this, 'install', installConfig(this, overrides), copilotHomeOverride);
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
