import assert from 'node:assert/strict';
import path from 'node:path';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  optionalEnvironment,
  runPlugin as basePlugin,
  runProcess as baseProcess,
  writeOptionalJson,
} from './harness.mjs';

export {
  VALID_PRIVATE_KEY,
  cliPath,
  readSettings,
  registryModuleUrl,
  startApiServer,
  writeJson,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/claudecode.js');
export const ioModuleUrl = distUrl('plugins/claudecode-io.js');
export const installationModuleUrl = distUrl('plugins/claudecode-installation.js');
const PLUGIN = { exportName: 'claudecodePlugin', moduleUrl: pluginModuleUrl };

export function runProcess(command, args, env, cwd, input = '', unset = []) {
  return baseProcess(command, args, env, cwd, input, { unset });
}

export function runNode(args, env, cwd, input = '', unset = []) {
  return runProcess(process.execPath, args, env, cwd, input, unset);
}

export function runClaudeHook(handler, input, fixture, environment = {}) {
  return runProcess(
    handler.command,
    handler.args,
    {
      ...homeEnvironment(fixture),
      CLAUDE_CONFIG_DIR: fixture.claudeConfigOverride ?? '',
      ...environment,
    },
    fixture.projectDir,
    input,
  );
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('claudecode', fixture, overrides);
}

export function runPlugin(fixture, method, argument, environment = {}) {
  const configDir = optionalEnvironment('CLAUDE_CONFIG_DIR', fixture.claudeConfigOverride);
  return basePlugin(PLUGIN, fixture, method, argument, {
    env: { ...environment, ...configDir.env },
    unset: configDir.unset,
  });
}

export async function createFixture({
  baseUrl = 'http://127.0.0.1:9',
  settings,
  explicitClaudeConfig = true,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-claude-',
    "home with spaces and 'quote %CLAUDE_HOOK_EVENT%",
  );
  const claudeConfigDir = explicitClaudeConfig
    ? path.join(base.homeDir, 'custom claude')
    : path.join(base.homeDir, '.claude');
  const settingsPath = path.join(claudeConfigDir, 'settings.json');
  await writeOptionalJson(settingsPath, settings);
  return {
    ...base,
    baseUrl,
    claudeConfigDir,
    claudeConfigOverride: explicitClaudeConfig ? claudeConfigDir : undefined,
    settingsPath,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
  };
}

export function managedHandler(settings, event) {
  for (const group of settings.hooks?.[event] ?? []) {
    if (Object.keys(group).join('|') !== 'hooks' || !Array.isArray(group.hooks)) continue;
    const handler = group.hooks.find((candidate) => (
      Object.keys(candidate).sort().join('|') === 'args|command|statusMessage|timeout|type'
      && candidate.type === 'command'
      && candidate.timeout === 10
    ));
    if (handler) return handler;
  }
  return undefined;
}

export function assertManagedHandler(handler, scriptPath, statusMessage) {
  assert(handler);
  assert.deepEqual(
    Object.keys(handler).sort(),
    ['args', 'command', 'statusMessage', 'timeout', 'type'],
  );
  assert.equal(handler.type, 'command');
  assert.equal(handler.command, process.execPath);
  assert.deepEqual(handler.args, [scriptPath]);
  assert.equal(handler.timeout, 10);
  assert.equal(handler.statusMessage, statusMessage);
}

export function legacyHandler(scriptPath) {
  return { type: 'command', command: `node "${scriptPath}"` };
}
