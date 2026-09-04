import assert from 'node:assert/strict';
import { mkdir, readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import { parse } from 'jsonc-parser';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  runPlugin as basePlugin,
  runProcess as baseProcess,
  writeOptionalJson,
} from './harness.mjs';

export {
  VALID_PRIVATE_KEY,
  cliPath,
  homeEnvironment as environment,
  registryModuleUrl,
  runNode,
  startApiServer,
  writeJson as writeConfig,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/droid.js');
export const configModuleUrl = distUrl('plugins/droid-config.js');
export const contractModuleUrl = distUrl('plugins/droid-contract.js');
export const installationModuleUrl = distUrl('plugins/droid-installation.js');
export const ioModuleUrl = distUrl('plugins/droid-io.js');
const PLUGIN = { exportName: 'droidPlugin', moduleUrl: pluginModuleUrl };

export function runProcess(command, args, env, cwd, input = '', shell = false) {
  return baseProcess(command, args, env, cwd, input, { shell });
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('droid', fixture, overrides);
}

export function runPlugin(fixture, method, argument) {
  return basePlugin(PLUGIN, fixture, method, argument, { cwd: fixture.workspaceDir });
}

export function runHook(command, fixture, input) {
  const env = homeEnvironment(fixture);
  if (process.platform === 'win32') {
    return runProcess(
      'powershell.exe',
      ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', command],
      env,
      fixture.workspaceDir,
      input,
    );
  }
  return runProcess('/bin/sh', ['-c', command], env, fixture.workspaceDir, input);
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  rootConfig,
  legacyConfig,
  settings,
  localSettings,
  projectSettings,
  projectLocalSettings,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-droid-',
    "home with spaces and 'quote %DROID%",
    { agentId, projectName: 'workspace with spaces' },
  );
  const workspaceDir = base.projectDir;
  const factoryDir = path.join(base.homeDir, '.factory');
  const rootPath = path.join(factoryDir, 'hooks.json');
  const legacyPath = path.join(factoryDir, 'hooks', 'hooks.json');
  const settingsPath = path.join(factoryDir, 'settings.json');
  const localSettingsPath = path.join(factoryDir, 'settings.local.json');
  const projectFactoryDir = path.join(workspaceDir, '.factory');
  const projectSettingsPath = path.join(projectFactoryDir, 'settings.json');
  const projectLocalSettingsPath = path.join(projectFactoryDir, 'settings.local.json');
  await mkdir(path.join(workspaceDir, '.git'), { recursive: true });
  await Promise.all([
    writeOptionalJson(rootPath, rootConfig),
    writeOptionalJson(legacyPath, legacyConfig),
    writeOptionalJson(settingsPath, settings),
    writeOptionalJson(localSettingsPath, localSettings),
    writeOptionalJson(projectSettingsPath, projectSettings),
    writeOptionalJson(projectLocalSettingsPath, projectLocalSettings),
  ]);
  const { projectDir: _projectDir, ...fixture } = base;
  return {
    ...fixture,
    baseUrl,
    factoryDir,
    legacyPath,
    localSettingsPath,
    projectLocalSettingsPath,
    projectSettingsPath,
    rootPath,
    settingsPath,
    workspaceDir,
    install(overrides = {}) {
      return runPlugin(this, 'install', installConfig(this, overrides));
    },
  };
}

export function readJsoncSource(source) {
  const errors = [];
  const value = parse(source, errors, { allowTrailingComma: true });
  if (errors.length > 0) throw new Error(`Unexpected JSONC parse errors: ${JSON.stringify(errors)}`);
  return value;
}

export async function readJsonc(filePath) {
  return readJsoncSource(await readFile(filePath, 'utf-8'));
}

export function managedGroup(hooks, event, scriptName) {
  return hooks?.[event]?.find((group) => group.hooks?.some(
    (handler) => handler.command?.includes(scriptName),
  ));
}

export function managedHandler(hooks, event, scriptName) {
  return managedGroup(hooks, event, scriptName)?.hooks.find(
    (handler) => handler.command?.includes(scriptName),
  );
}

export function assertNativeGroup(group) {
  if (!group) throw new Error('Managed group is missing');
  if (JSON.stringify(Object.keys(group).sort()) !== JSON.stringify(['hooks', 'matcher'])) {
    throw new Error(`Unexpected group fields: ${Object.keys(group).join(', ')}`);
  }
  if (group.matcher !== '*' || group.hooks.length !== 1) {
    throw new Error(`Unexpected group contract: ${JSON.stringify(group)}`);
  }
  const handler = group.hooks[0];
  if (JSON.stringify(Object.keys(handler).sort()) !== JSON.stringify(['command', 'timeout', 'type'])) {
    throw new Error(`Unexpected handler fields: ${Object.keys(handler).join(', ')}`);
  }
  if (handler.type !== 'command' || handler.timeout !== 10) {
    throw new Error(`Unexpected handler contract: ${JSON.stringify(handler)}`);
  }
}

export async function assertMissing(filePath) {
  await assert.rejects(readFile(filePath), { code: 'ENOENT' });
}

export async function assertNoTransactionFiles(fixture) {
  const names = await readdir(fixture.rootDir, { recursive: true });
  const leaked = names.filter((name) => /\.(tmp|rollback)$/.test(name));
  if (leaked.length > 0) throw new Error(`Leaked transaction files: ${leaked.join(', ')}`);
}
