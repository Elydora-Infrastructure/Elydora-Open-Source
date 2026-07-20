import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type RuntimeContract,
  sameAgentId,
  samePath,
} from './droid-contract.js';
import {
  activeDocument,
  createLegacyHookDocument,
  createOwnedHookDocument,
  createSettingsDocument,
  hookBlock,
  parseDocument,
  type DroidDocument,
  type DroidDocumentKind,
  type DroidSources,
} from './droid-config.js';
import { generateGuardScript } from './guard-template.js';
import { generateHookScript } from './hook-template.js';
import { readDroidPolicy } from './droid-policy.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { parseStrictJsonObject, type JsonObject } from './strict-json.js';

const MAX_SECRET_BYTES = 64 * 1024;
const MAX_CONFIG_BYTES = 512 * 1024;
const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

interface FactoryPaths {
  readonly directory: string;
  readonly root: string;
  readonly legacyDirectory: string;
  readonly legacy: string;
  readonly settings: string;
  readonly localSettings: string;
}

function factoryPaths(): FactoryPaths {
  const directory = path.join(os.homedir(), '.factory');
  const legacyDirectory = path.join(directory, 'hooks');
  return {
    directory,
    root: path.join(directory, 'hooks.json'),
    legacyDirectory,
    legacy: path.join(legacyDirectory, 'hooks.json'),
    settings: path.join(directory, 'settings.json'),
    localSettings: path.join(directory, 'settings.local.json'),
  };
}

async function readDocument(
  filePath: string,
  kind: DroidDocumentKind,
  label: string,
): Promise<DroidDocument | undefined> {
  const snapshot = await readPhysicalFile(filePath, label, MAX_SOURCE_BYTES);
  return snapshot ? parseDocument({
    exists: true,
    filePath,
    kind,
    raw: snapshot.contents,
    snapshot,
  }) : undefined;
}

export async function readSources(): Promise<DroidSources> {
  const paths = factoryPaths();
  await inspectPhysicalDirectory(paths.directory, 'Factory Droid user configuration directory');
  await inspectPhysicalDirectory(paths.legacyDirectory, 'Factory Droid legacy hooks directory');
  const [root, legacy, settings, localSettings, policy] = await Promise.all([
    readDocument(paths.root, 'hooks', 'Factory Droid user hooks'),
    readDocument(paths.legacy, 'legacy', 'Factory Droid legacy hooks'),
    readDocument(paths.settings, 'settings', 'Factory Droid user settings'),
    readDocument(paths.localSettings, 'local-settings', 'Factory Droid local settings'),
    readDroidPolicy(),
  ]);
  return {
    root: root ?? createOwnedHookDocument(paths.root),
    legacy: legacy ?? createLegacyHookDocument(paths.legacy),
    settings: settings ?? createSettingsDocument(paths.settings),
    localSettings: localSettings
      ?? createSettingsDocument(paths.localSettings, 'local-settings'),
    policy,
  };
}

export function requireHooksEnabled(sources: DroidSources): void {
  const blocked = hookBlock(sources);
  if (blocked) {
    throw new Error(
      `Factory Droid user hooks are disabled by ${blocked.field} in ${blocked.label} at ${blocked.filePath}`,
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
    throw new Error(`Elydora runtime identity does not match Factory Droid hooks: ${configPath}`);
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

export function displayConfigPath(sources: DroidSources): string {
  return activeDocument(sources).filePath;
}
