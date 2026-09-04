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

export const pluginModuleUrl = distUrl('plugins/gemini.js');
export const ioModuleUrl = distUrl('plugins/gemini-io.js');
export const installationModuleUrl = distUrl('plugins/gemini-installation.js');
const PLUGIN = { exportName: 'geminiPlugin', moduleUrl: pluginModuleUrl };

export function runProcess(command, args, env, cwd, input = '', unset = []) {
  return baseProcess(command, args, env, cwd, input, { unset });
}

export function runNode(args, env, cwd, input = '', unset = []) {
  return runProcess(process.execPath, args, env, cwd, input, unset);
}

export function runGeminiHook(handler, input, fixture, environment = {}) {
  const env = {
    ...homeEnvironment(fixture),
    GEMINI_CLI_HOME: fixture.geminiCliHomeOverride ?? '',
    ...environment,
  };
  if (process.platform === 'win32') {
    return runProcess(
      windowsPowerShellPath(),
      [
        '-NoLogo',
        '-NoProfile',
        '-NonInteractive',
        '-Command',
        `${handler.command}; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }`,
      ],
      env,
      fixture.projectDir,
      input,
    );
  }
  return runProcess('/bin/bash', ['-c', handler.command], env, fixture.projectDir, input);
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('gemini', fixture, overrides);
}

export function runPlugin(fixture, method, argument, environment = {}) {
  const home = optionalEnvironment('GEMINI_CLI_HOME', fixture.geminiCliHomeOverride);
  return basePlugin(PLUGIN, fixture, method, argument, {
    env: { ...environment, ...home.env },
    unset: home.unset,
  });
}

export async function createFixture({
  baseUrl = 'http://127.0.0.1:9',
  settings,
  explicitGeminiHome = true,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-gemini-',
    "home with spaces and 'quote $GEMINI_CWD %GEMINI_CWD%",
  );
  const geminiCliHome = base.homeDir;
  const settingsPath = path.join(geminiCliHome, '.gemini', 'settings.json');
  await writeOptionalJson(settingsPath, settings);
  return {
    ...base,
    baseUrl,
    geminiCliHome,
    geminiCliHomeOverride: explicitGeminiHome ? geminiCliHome : undefined,
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
      Object.keys(candidate).sort().join('|') === 'command|name|timeout|type'
      && candidate.type === 'command'
      && candidate.name === name
      && candidate.timeout === 10_000
    ));
    if (handler) return handler;
  }
  return undefined;
}

export function assertManagedHandler(handler, name) {
  assert(handler);
  assert.deepEqual(Object.keys(handler).sort(), ['command', 'name', 'timeout', 'type']);
  assert.equal(handler.type, 'command');
  assert.equal(handler.name, name);
  assert.equal(handler.timeout, 10_000);
  assert.equal(typeof handler.command, 'string');
}

export function legacyHandler(scriptPath) {
  return { type: 'command', command: `node "${scriptPath}"` };
}
