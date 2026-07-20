import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  AUDIT_WRAPPER,
  GUARD_SCRIPT,
  GUARD_WRAPPER,
  buildWrapper,
  createAugmentDocument,
  parseAugmentDocument,
  type AugmentDocument,
  type RenderedAugmentDocument,
  type RuntimeContract,
} from './augment-contract.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import {
  commitManagedTransaction,
  prepareManagedFileChange,
} from './managed-transaction.js';
import { parseStrictJsonObject, type JsonObject } from './strict-json.js';

const MAX_SECRET_BYTES = 64 * 1024;
const MAX_CONFIG_BYTES = 512 * 1024;

export function resolveConfigPath(): string {
  return path.join(os.homedir(), '.augment', 'settings.json');
}

export async function readConfig(): Promise<AugmentDocument> {
  const configPath = resolveConfigPath();
  await inspectPhysicalDirectory(path.dirname(configPath), 'Auggie configuration directory');
  const snapshot = await readPhysicalFile(configPath, 'Auggie user settings', MAX_CONFIG_BYTES);
  return snapshot
    ? parseAugmentDocument(configPath, snapshot.contents)
    : createAugmentDocument(configPath);
}

export async function writeAugmentDocument(rendered: RenderedAugmentDocument): Promise<void> {
  if (!rendered.changed) return;
  const change = await prepareManagedFileChange({
    filePath: rendered.document.configPath,
    label: 'Auggie user settings',
    next: rendered.next,
    mode: 0o600,
    maximumBytes: MAX_CONFIG_BYTES,
    expectedSource: rendered.document.raw,
    verifyExpectedSource: true,
  });
  if (!change) return;
  await commitManagedTransaction({
    displayName: 'Augment Code CLI',
    operation: 'uninstall',
    directories: [{
      path: path.dirname(rendered.document.configPath),
      label: 'Auggie configuration directory',
    }],
    changes: [change],
  });
}

function sameAgentId(left: string, right: string): boolean {
  return process.platform === 'win32'
    ? left.toLowerCase() === right.toLowerCase()
    : left === right;
}

function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

function requireNonEmptyString(value: unknown, field: string, configPath: string): string {
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
  requireNonEmptyString(config.org_id, 'org_id', configPath);
  requireNonEmptyString(config.kid, 'kid', configPath);
  const agentId = requireNonEmptyString(config.agent_id, 'agent_id', configPath);
  if (!sameAgentId(agentId, contract.agentId) || config.agent_name !== AGENT_KEY) {
    throw new Error(`Elydora runtime identity does not match Auggie hooks: ${configPath}`);
  }
  if (config.token !== undefined) requireNonEmptyString(config.token, 'token', configPath);
  const rawBaseUrl = requireNonEmptyString(config.base_url, 'base_url', configPath);
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

function validContractPaths(contract: RuntimeContract, agentDirectory: string): boolean {
  return samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))
    && samePath(contract.guardPath, path.join(agentDirectory, GUARD_SCRIPT))
    && samePath(contract.auditPath, path.join(agentDirectory, AUDIT_SCRIPT))
    && samePath(contract.guardWrapperPath, path.join(agentDirectory, GUARD_WRAPPER))
    && samePath(contract.auditWrapperPath, path.join(agentDirectory, AUDIT_WRAPPER));
}

async function runtimeContractExists(contract: RuntimeContract): Promise<boolean> {
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  const agentDirectory = path.dirname(contract.guardPath);
  if (!validContractPaths(contract, agentDirectory)) return false;
  if (!await inspectPhysicalDirectory(runtimeRoot, 'Elydora runtime directory')) return false;
  if (!await inspectPhysicalDirectory(agentDirectory, 'Elydora agent runtime directory')) {
    return false;
  }
  const configPath = path.join(agentDirectory, 'config.json');
  const keyPath = path.join(agentDirectory, 'private.key');
  const [config, key, guard, audit, guardWrapper, auditWrapper] = await Promise.all([
    readPhysicalFile(configPath, 'Elydora runtime config', MAX_CONFIG_BYTES),
    readPhysicalFile(keyPath, 'Elydora private key', MAX_SECRET_BYTES),
    readPhysicalFile(contract.guardPath, 'Elydora guard runtime'),
    readPhysicalFile(contract.auditPath, 'Elydora audit runtime'),
    readPhysicalFile(contract.guardWrapperPath, 'Auggie guard wrapper'),
    readPhysicalFile(contract.auditWrapperPath, 'Auggie audit wrapper'),
  ]);
  if (!config || !key || !guard || !audit || !guardWrapper || !auditWrapper) return false;
  const parsed = parseStrictJsonObject(config.contents, `Elydora runtime config at ${configPath}`);
  validateRuntimeConfig(parsed, contract, configPath);
  validatePrivateKey(key.contents, keyPath);
  return guard.contents.length > 0
    && audit.contents.length > 0
    && guardWrapper.contents === buildWrapper(contract.guardPath)
    && auditWrapper.contents === buildWrapper(contract.auditPath);
}

export async function augmentRuntimeFilesExist(
  contracts: readonly RuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    if (await runtimeContractExists(contract)) return true;
  }
  return false;
}
