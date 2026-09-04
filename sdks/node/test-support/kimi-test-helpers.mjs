import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { parse as parseToml } from '@decimalturn/toml-patch';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  optionalEnvironment,
  runNode as baseNode,
  runPlugin as basePlugin,
  runShell,
} from './harness.mjs';

export { VALID_PRIVATE_KEY, cliPath, registryModuleUrl, startApiServer } from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/kimi.js');
export const contractModuleUrl = distUrl('plugins/kimi-contract.js');
export const ioModuleUrl = distUrl('plugins/kimi-io.js');
export const installationModuleUrl = distUrl('plugins/kimi-installation.js');
const PLUGIN = { exportName: 'kimiPlugin', moduleUrl: pluginModuleUrl };

export function runNode(args, env, cwd, input = '', unset = []) {
  return baseNode(args, env, cwd, input, { unset });
}

export function runKimiHook(command, input, fixture, environment = {}) {
  return runShell(command, {
    ...homeEnvironment(fixture),
    KIMI_CODE_HOME: fixture.kimiHomeOverride ?? '',
    ...environment,
  }, fixture.projectDir, input);
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('kimi', fixture, overrides);
}

export function runPlugin(fixture, method, argument, environment = {}) {
  const home = optionalEnvironment('KIMI_CODE_HOME', fixture.kimiHomeOverride);
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
  stableConfig,
  legacyConfig,
  stableDetected = true,
  legacyDetected = true,
  explicitKimiHome = true,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-kimi-',
    "home with spaces and 'quote %ELYDORA_HOOK_PATH%",
  );
  const kimiHome = explicitKimiHome
    ? path.join(base.homeDir, 'custom kimi-code')
    : path.join(base.homeDir, '.kimi-code');
  const stablePath = path.join(kimiHome, 'config.toml');
  const legacyHome = path.join(base.homeDir, '.kimi');
  const legacyPath = path.join(legacyHome, 'config.toml');
  if (stableDetected && !explicitKimiHome) await mkdir(kimiHome, { recursive: true });
  if (legacyDetected) await mkdir(legacyHome, { recursive: true });
  await writeOptional(stablePath, stableConfig);
  await writeOptional(legacyPath, legacyConfig);
  return {
    ...base,
    baseUrl,
    kimiHome,
    kimiHomeOverride: explicitKimiHome ? kimiHome : undefined,
    legacyHome,
    legacyPath,
    stablePath,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
  };
}

export async function readKimiConfig(filePath) {
  const raw = await readFile(filePath, 'utf-8');
  return { raw, config: parseToml(raw) };
}

export function managedHook(config, event) {
  return config.hooks?.findLast((hook) => hook.event === event && hook.timeout === 10);
}

export function assertManagedHook(hook, event) {
  assert(hook);
  assert.deepEqual(Object.keys(hook).sort(), ['command', 'event', 'timeout']);
  assert.equal(hook.event, event);
  assert.equal(hook.timeout, 10);
  if (process.platform === 'win32') {
    assert.match(hook.command, /^"[^"\r\n]+powershell\.exe" .* -EncodedCommand /i);
  } else {
    assert.match(hook.command, /^'[^']*node[^']*' /i);
  }
}

export function legacyCommand(scriptPath) {
  if (process.platform === 'win32') return `"${process.execPath}" "${scriptPath}"`;
  const quote = (value) => `'${value.replaceAll("'", `'"'"'`)}'`;
  return `${quote(process.execPath)} ${quote(scriptPath)}`;
}
