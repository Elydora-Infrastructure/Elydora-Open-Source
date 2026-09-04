import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  optionalEnvironment,
  runPlugin as basePlugin,
  runProcess as baseProcess,
  runShell,
} from './harness.mjs';

export { VALID_PRIVATE_KEY, cliPath, registryModuleUrl, startApiServer } from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/grok.js');
export const contractModuleUrl = distUrl('plugins/grok-contract.js');
export const ioModuleUrl = distUrl('plugins/grok-io.js');
export const installationModuleUrl = distUrl('plugins/grok-installation.js');
const PLUGIN = { exportName: 'grokPlugin', moduleUrl: pluginModuleUrl };

export function runProcess(command, args, env, cwd, input = '', unset = []) {
  return baseProcess(command, args, env, cwd, input, { unset });
}

export function runNode(args, env, cwd, input = '', unset = []) {
  return runProcess(process.execPath, args, env, cwd, input, unset);
}

export function runGrokHook(command, input, fixture, environment = {}) {
  return runShell(command, {
    ...homeEnvironment(fixture),
    GROK_HOME: fixture.grokHomeOverride ?? '',
    ...environment,
  }, fixture.projectDir, input);
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('grok', fixture, overrides);
}

export function runPlugin(fixture, method, argument, environment = {}) {
  const home = optionalEnvironment('GROK_HOME', fixture.grokHomeOverride);
  return basePlugin(PLUGIN, fixture, method, argument, {
    env: { ...environment, ...home.env },
    unset: home.unset,
  });
}

async function writeOptional(filePath, contents) {
  if (contents === undefined) return;
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, contents, { encoding: 'utf-8', mode: 0o600 });
}

export async function createFixture({
  baseUrl = 'http://127.0.0.1:9',
  config,
  explicitGrokHome = true,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-grok-',
    "home with spaces and 'quote %GROK_HOOK_EVENT%",
  );
  const grokHome = explicitGrokHome
    ? path.join(base.homeDir, 'custom grok')
    : path.join(base.homeDir, '.grok');
  const configPath = path.join(grokHome, 'hooks', 'elydora-audit.json');
  await writeOptional(configPath, config);
  return {
    ...base,
    baseUrl,
    configPath,
    grokHome,
    grokHomeOverride: explicitGrokHome ? grokHome : undefined,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
  };
}

export async function readGrokConfig(filePath) {
  const raw = await readFile(filePath, 'utf-8');
  return { raw, config: JSON.parse(raw) };
}

export function managedHandler(config, event) {
  for (const group of [...(config.hooks?.[event] ?? [])].reverse()) {
    if (Object.keys(group).length !== 1 || !Array.isArray(group.hooks)) continue;
    const handler = group.hooks.findLast((candidate) => (
      Object.keys(candidate).sort().join('|') === 'command|timeout|type'
      && candidate.type === 'command'
      && candidate.timeout === 10
    ));
    if (handler) return handler;
  }
  return undefined;
}

export function assertManagedHandler(handler) {
  assert(handler);
  assert.deepEqual(Object.keys(handler).sort(), ['command', 'timeout', 'type']);
  assert.equal(handler.type, 'command');
  assert.equal(handler.timeout, 10);
  if (process.platform === 'win32') {
    assert.match(handler.command, /^"[^"\r\n]+powershell\.exe" .* -EncodedCommand /i);
  } else {
    assert.match(handler.command, /^'[^']*node[^']*' /i);
  }
}

export function legacyCommand(scriptPath) {
  if (process.platform === 'win32') return `"${process.execPath}" "${scriptPath}"`;
  const quote = (value) => `'${value.replaceAll("'", `'"'"'`)}'`;
  return `${quote(process.execPath)} ${quote(scriptPath)}`;
}
