import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  CONFIG_FILE,
  GUARD_SCRIPT,
  type CopilotDocument,
  type CopilotSources,
  type RuntimeContract,
  createDocument,
  parseDocument,
  sameAgentId,
  samePath,
} from './copilot-contract.js';
import { generateGuardScript } from './guard-template.js';
import { generateHookScript } from './hook-template.js';
import {
  inspectPhysicalDirectory,
  readPhysicalFile,
  type FileSnapshot,
} from './managed-files.js';
import {
  parseStrictJsonObject,
  parseStrictJsoncObject,
  type JsonObject,
} from './strict-json.js';

const MAX_SECRET_BYTES = 64 * 1024;
const MAX_CONFIG_BYTES = 512 * 1024;
const MAX_SETTINGS_BYTES = 2 * 1024 * 1024;

export interface CopilotPaths {
  readonly copilotHome: string;
  readonly userHooksDirectory: string;
  readonly userHookPath: string;
  readonly legacyHookPath: string;
  readonly settingsLayers: readonly SettingsLayer[];
  readonly inspectedDirectories: readonly DirectoryLocation[];
}

interface DirectoryLocation {
  readonly path: string;
  readonly label: string;
}

interface SettingsLayer {
  readonly filePath: string;
  readonly label: string;
  readonly jsonc: boolean;
}

interface ParsedSettingsLayer extends SettingsLayer {
  readonly disableAllHooks?: boolean;
  readonly snapshot?: FileSnapshot;
}

export function resolveCopilotPaths(): CopilotPaths {
  const override = process.env.COPILOT_HOME;
  const copilotHome = override ? override : path.join(os.homedir(), '.copilot');
  const project = process.cwd();
  const github = path.join(project, '.github');
  const githubCopilot = path.join(github, 'copilot');
  const githubHooks = path.join(github, 'hooks');
  const claude = path.join(project, '.claude');
  return {
    copilotHome,
    userHooksDirectory: path.join(copilotHome, 'hooks'),
    userHookPath: path.join(copilotHome, 'hooks', CONFIG_FILE),
    legacyHookPath: path.join(githubHooks, 'hooks.json'),
    settingsLayers: [
      { filePath: path.join(copilotHome, 'config.json'), label: 'legacy Copilot user config', jsonc: false },
      { filePath: path.join(copilotHome, 'settings.json'), label: 'Copilot user settings', jsonc: true },
      { filePath: path.join(claude, 'settings.json'), label: 'Claude repository settings', jsonc: true },
      { filePath: path.join(claude, 'settings.local.json'), label: 'Claude local settings', jsonc: true },
      { filePath: path.join(githubCopilot, 'settings.json'), label: 'Copilot repository settings', jsonc: true },
      { filePath: path.join(githubCopilot, 'settings.local.json'), label: 'Copilot local settings', jsonc: true },
    ],
    inspectedDirectories: [
      { path: project, label: 'Copilot working directory' },
      { path: copilotHome, label: 'COPILOT_HOME' },
      { path: path.join(copilotHome, 'hooks'), label: 'Copilot user hooks directory' },
      { path: github, label: 'GitHub configuration directory' },
      { path: githubHooks, label: 'GitHub repository hooks directory' },
      { path: githubCopilot, label: 'Copilot repository settings directory' },
      { path: claude, label: 'Claude repository settings directory' },
    ],
  };
}

async function inspectDirectories(locations: readonly DirectoryLocation[]): Promise<void> {
  for (const location of locations) {
    await inspectPhysicalDirectory(location.path, location.label);
  }
}

async function readHookDocument(
  filePath: string,
  label: string,
): Promise<CopilotDocument | undefined> {
  const snapshot = await readPhysicalFile(filePath, label, MAX_SETTINGS_BYTES);
  return snapshot ? parseDocument(filePath, snapshot, label) : undefined;
}

function parseSettings(raw: string, layer: SettingsLayer): JsonObject {
  if (raw.trim().length === 0) return {};
  const label = `${layer.label} at ${layer.filePath}`;
  return layer.jsonc
    ? parseStrictJsoncObject(raw, label)
    : parseStrictJsonObject(raw, label);
}

async function readSettingsLayer(layer: SettingsLayer): Promise<ParsedSettingsLayer> {
  const snapshot = await readPhysicalFile(layer.filePath, layer.label, MAX_SETTINGS_BYTES);
  if (!snapshot) return layer;
  const root = parseSettings(snapshot.contents, layer);
  if (root.disableAllHooks !== undefined && typeof root.disableAllHooks !== 'boolean') {
    throw new Error(
      `${layer.label} at ${layer.filePath} field "disableAllHooks" must be a boolean`,
    );
  }
  return {
    ...layer,
    disableAllHooks: root.disableAllHooks as boolean | undefined,
    snapshot,
  };
}

function effectiveDisabledSource(layers: readonly ParsedSettingsLayer[]): string | undefined {
  let disabledBy: string | undefined;
  for (const layer of layers) {
    if (layer.disableAllHooks === true) disabledBy = `${layer.label} at ${layer.filePath}`;
    else if (layer.disableAllHooks === false) disabledBy = undefined;
  }
  return disabledBy;
}

export async function readSources(): Promise<CopilotSources> {
  const paths = resolveCopilotPaths();
  await inspectDirectories(paths.inspectedDirectories);
  const [user, legacy] = await Promise.all([
    readHookDocument(paths.userHookPath, 'GitHub Copilot user hooks'),
    readHookDocument(paths.legacyHookPath, 'GitHub Copilot legacy project hooks'),
  ]);
  const layers = await Promise.all(paths.settingsLayers.map(readSettingsLayer));
  const userDocument = user ?? createDocument(paths.userHookPath);
  const disabledBy = userDocument.hooksDisabled
    ? `GitHub Copilot user hooks at ${paths.userHookPath}`
    : effectiveDisabledSource(layers);
  return {
    user: userDocument,
    legacy,
    disabledBy,
    settingsPreconditions: layers.map((layer) => ({
      filePath: layer.filePath,
      label: layer.label,
      snapshot: layer.snapshot,
    })),
  };
}

export function requireHooksEnabled(sources: CopilotSources): void {
  if (sources.disabledBy) {
    throw new Error(
      `GitHub Copilot hooks are disabled by ${sources.disabledBy}; set disableAllHooks to false before installation`,
    );
  }
}

function requireString(value: unknown, field: string, configPath: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`Elydora runtime config ${field} is invalid: ${configPath}`);
  }
  return value;
}

function validateRuntimeConfig(
  config: JsonObject,
  contract: RuntimeContract,
  configPath: string,
): void {
  const supported = new Set(['org_id', 'agent_id', 'kid', 'base_url', 'token', 'agent_name']);
  const extra = Object.keys(config).find((key) => !supported.has(key));
  if (extra) throw new Error(`Elydora runtime config has unsupported field "${extra}": ${configPath}`);
  requireString(config.org_id, 'org_id', configPath);
  requireString(config.kid, 'kid', configPath);
  const agentId = requireString(config.agent_id, 'agent_id', configPath);
  if (!sameAgentId(agentId, contract.agentId) || config.agent_name !== AGENT_KEY) {
    throw new Error(`Elydora runtime identity does not match Copilot hooks: ${configPath}`);
  }
  if (config.token !== undefined) requireString(config.token, 'token', configPath);
  const rawBaseUrl = requireString(config.base_url, 'base_url', configPath);
  let baseUrl: URL;
  try {
    baseUrl = new URL(rawBaseUrl);
  } catch (error) {
    throw new Error(`Elydora runtime config base_url is invalid: ${configPath}`, {
      cause: error instanceof Error ? error : new Error(String(error)),
    });
  }
  if (!['http:', 'https:'].includes(baseUrl.protocol)
    || !baseUrl.hostname
    || baseUrl.username
    || baseUrl.password
    || baseUrl.search
    || baseUrl.hash) {
    throw new Error(`Elydora runtime config base_url is invalid: ${configPath}`);
  }
}

function validatePrivateKey(contents: string, keyPath: string): void {
  const bytes = Buffer.from(contents, 'base64url');
  if (bytes.length !== 32 || bytes.toString('base64url') !== contents) {
    throw new Error(`Elydora private key is invalid: ${keyPath}`);
  }
}

function validContractPaths(contract: RuntimeContract): boolean {
  const agentDirectory = path.dirname(contract.guardPath);
  return samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))
    && samePath(contract.guardPath, path.join(agentDirectory, GUARD_SCRIPT))
    && samePath(contract.auditPath, path.join(agentDirectory, AUDIT_SCRIPT));
}

export async function runtimeFilesExist(contracts: RuntimeContract[]): Promise<boolean> {
  for (const contract of contracts) {
    if (!validContractPaths(contract)) continue;
    const runtimeRoot = path.join(os.homedir(), '.elydora');
    const agentDirectory = path.dirname(contract.guardPath);
    if (!await inspectPhysicalDirectory(runtimeRoot, 'Elydora runtime directory')) continue;
    if (!await inspectPhysicalDirectory(agentDirectory, 'Elydora agent runtime directory')) continue;
    const configPath = path.join(agentDirectory, 'config.json');
    const keyPath = path.join(agentDirectory, 'private.key');
    const [config, key, guard, audit] = await Promise.all([
      readPhysicalFile(configPath, 'Elydora runtime config', MAX_CONFIG_BYTES),
      readPhysicalFile(keyPath, 'Elydora private key', MAX_SECRET_BYTES),
      readPhysicalFile(contract.guardPath, 'Elydora guard runtime'),
      readPhysicalFile(contract.auditPath, 'Elydora audit runtime'),
    ]);
    if (!config || !key || !guard || !audit) continue;
    const parsed = parseStrictJsonObject(config.contents, `Elydora runtime config at ${configPath}`);
    validateRuntimeConfig(parsed, contract, configPath);
    validatePrivateKey(key.contents, keyPath);
    if (guard.contents === generateGuardScript(AGENT_KEY, contract.agentId)
      && audit.contents === generateHookScript(
        AGENT_KEY,
        contract.agentId,
        { nativePayload: true },
      )) return true;
  }
  return false;
}
