import fsp from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  LEGACY_EVENTS,
  STABLE_EVENTS,
  createKimiDocument,
  parseKimiDocument,
  type KimiConfigDocument,
  type KimiContract,
  type KimiRuntimeContract,
  type TomlObject,
} from './kimi-contract.js';
import { sameKimiAgentId, sameKimiPath } from './kimi-command.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { parseStrictJsonObject } from './strict-json.js';

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function hasCode(error: unknown, code: string): boolean {
  return typeof error === 'object' && error !== null && 'code' in error && error.code === code;
}

async function pathEntryExists(filePath: string, label: string): Promise<boolean> {
  try {
    await fsp.lstat(filePath);
    return true;
  } catch (error) {
    if (hasCode(error, 'ENOENT') || hasCode(error, 'ENOTDIR')) return false;
    throw new Error(`Inspect ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

function stableContract(configPath: string): KimiContract {
  return {
    generation: 'stable',
    runtimeName: 'Kimi Code',
    label: 'Kimi Code hooks config',
    directoryLabel: 'Kimi Code home directory',
    configPath,
    events: STABLE_EVENTS,
  };
}

function legacyContract(configPath: string): KimiContract {
  return {
    generation: 'legacy',
    runtimeName: 'kimi-cli',
    label: 'kimi-cli legacy hooks config',
    directoryLabel: 'kimi-cli legacy home directory',
    configPath,
    events: LEGACY_EVENTS,
  };
}

export async function resolveKimiContracts(): Promise<KimiContract[]> {
  const home = os.homedir();
  const configuredHome = process.env.KIMI_CODE_HOME;
  const explicitHome = configuredHome === undefined || configuredHome === ''
    ? undefined
    : path.resolve(configuredHome);
  const stableHome = explicitHome ?? path.join(home, '.kimi-code');
  const legacyHome = path.join(home, '.kimi');
  const stable = stableContract(path.join(stableHome, 'config.toml'));
  const legacy = legacyContract(path.join(legacyHome, 'config.toml'));
  if (sameKimiPath(stable.configPath, legacy.configPath)) return [stable];

  const stableDetected = explicitHome !== undefined
    || await pathEntryExists(stableHome, 'Kimi Code home');
  const legacyDetected = await pathEntryExists(legacyHome, 'kimi-cli legacy home');
  if (legacyDetected && !stableDetected) return [legacy];
  return legacyDetected ? [stable, legacy] : [stable];
}

async function readKimiDocument(contract: KimiContract): Promise<KimiConfigDocument> {
  await inspectPhysicalDirectory(path.dirname(contract.configPath), contract.directoryLabel);
  const snapshot = await readPhysicalFile(contract.configPath, contract.label);
  return snapshot
    ? parseKimiDocument(contract, snapshot.contents)
    : createKimiDocument(contract);
}

export async function readKimiDocuments(): Promise<KimiConfigDocument[]> {
  const contracts = await resolveKimiContracts();
  const documents: KimiConfigDocument[] = [];
  for (const contract of contracts) documents.push(await readKimiDocument(contract));
  return documents;
}

function requireNonEmptyString(value: unknown, field: string, configPath: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`Elydora runtime config ${field} is invalid: ${configPath}`);
  }
  return value;
}

function validateRuntimeConfig(
  config: TomlObject,
  contract: KimiRuntimeContract,
  configPath: string,
): void {
  const supported = new Set(['org_id', 'agent_id', 'kid', 'base_url', 'token', 'agent_name']);
  const extra = Object.keys(config).find((key) => !supported.has(key));
  if (extra) throw new Error(`Elydora runtime config has unsupported field "${extra}": ${configPath}`);
  requireNonEmptyString(config.org_id, 'org_id', configPath);
  requireNonEmptyString(config.kid, 'kid', configPath);
  const agentId = requireNonEmptyString(config.agent_id, 'agent_id', configPath);
  if (!sameKimiAgentId(agentId, contract.agentId) || config.agent_name !== AGENT_KEY) {
    throw new Error(`Elydora runtime identity does not match Kimi hooks: ${configPath}`);
  }
  if (config.token !== undefined) requireNonEmptyString(config.token, 'token', configPath);
  const rawBaseUrl = requireNonEmptyString(config.base_url, 'base_url', configPath);
  let baseUrl: URL;
  try {
    baseUrl = new URL(rawBaseUrl);
  } catch (error) {
    throw new Error(`Elydora runtime config base_url is invalid: ${configPath}`, {
      cause: asError(error),
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

async function runtimeContractExists(contract: KimiRuntimeContract): Promise<boolean> {
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  const agentDirectory = path.dirname(contract.guardPath);
  if (!sameKimiPath(path.dirname(agentDirectory), runtimeRoot)
    || !sameKimiPath(contract.auditPath, path.join(agentDirectory, 'hook.js'))) return false;
  if (!await inspectPhysicalDirectory(runtimeRoot, 'Elydora runtime directory')) return false;
  if (!await inspectPhysicalDirectory(agentDirectory, 'Elydora agent runtime directory')) return false;

  const configPath = path.join(agentDirectory, 'config.json');
  const keyPath = path.join(agentDirectory, 'private.key');
  const [config, key, guard, audit] = await Promise.all([
    readPhysicalFile(configPath, 'Elydora runtime config'),
    readPhysicalFile(keyPath, 'Elydora private key'),
    readPhysicalFile(contract.guardPath, 'Elydora guard runtime'),
    readPhysicalFile(contract.auditPath, 'Elydora audit runtime'),
  ]);
  if (!config || !key || !guard || !audit) return false;
  const parsed = parseStrictJsonObject(config.contents, `Elydora runtime config at ${configPath}`);
  validateRuntimeConfig(parsed, contract, configPath);
  validatePrivateKey(key.contents, keyPath);
  return guard.contents.length > 0 && audit.contents.length > 0;
}

export async function kimiRuntimeFilesExist(
  contracts: readonly KimiRuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    if (await runtimeContractExists(contract)) return true;
  }
  return false;
}
