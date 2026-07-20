import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type ClineHookFile,
  type ClineRuntimeContract,
  parseMetadata,
  sameAgentId,
} from './cline-contract.js';
import { generateGuardScript } from './guard-template.js';
import { generateHookScript } from './hook-template.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { parseStrictJsonObject, type JsonObject } from './strict-json.js';

const MAX_SECRET_BYTES = 64 * 1024;
const MAX_CONFIG_BYTES = 512 * 1024;

export async function readHookFile(filePath: string): Promise<ClineHookFile> {
  const directory = path.dirname(filePath);
  if (!await inspectPhysicalDirectory(directory, 'Cline hooks directory')) {
    return { exists: false, filePath };
  }
  const snapshot = await readPhysicalFile(filePath, 'Cline hook');
  if (!snapshot) return { exists: false, filePath };
  return {
    exists: true,
    filePath,
    source: snapshot.contents,
    metadata: parseMetadata(filePath, snapshot.contents),
  };
}

export function requireAvailableHookFile(file: ClineHookFile): void {
  if (file.exists && !file.metadata) {
    throw new Error(`Cline hook at ${file.filePath} already exists and is owned by another integration`);
  }
}

function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

function requireString(value: unknown, field: string, configPath: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`Elydora runtime config ${field} is invalid: ${configPath}`);
  }
  return value;
}

function validateRuntimeConfig(
  config: JsonObject,
  contract: ClineRuntimeContract,
  configPath: string,
): void {
  const supported = new Set(['org_id', 'agent_id', 'kid', 'base_url', 'token', 'agent_name']);
  const extra = Object.keys(config).find((key) => !supported.has(key));
  if (extra) throw new Error(`Elydora runtime config has unsupported field "${extra}": ${configPath}`);
  requireString(config.org_id, 'org_id', configPath);
  requireString(config.kid, 'kid', configPath);
  const agentId = requireString(config.agent_id, 'agent_id', configPath);
  if (!sameAgentId(agentId, contract.agentId) || config.agent_name !== AGENT_KEY) {
    throw new Error(`Elydora runtime identity does not match Cline hooks: ${configPath}`);
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

function validContractPaths(contract: ClineRuntimeContract): boolean {
  return samePath(path.dirname(contract.agentDirectory), path.join(os.homedir(), '.elydora'))
    && samePath(contract.guardPath, path.join(contract.agentDirectory, GUARD_SCRIPT))
    && samePath(contract.auditPath, path.join(contract.agentDirectory, AUDIT_SCRIPT));
}

export async function runtimeFilesExist(contract: ClineRuntimeContract): Promise<boolean> {
  if (!validContractPaths(contract)) return false;
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  if (!await inspectPhysicalDirectory(runtimeRoot, 'Elydora runtime directory')) return false;
  if (!await inspectPhysicalDirectory(contract.agentDirectory, 'Elydora agent runtime directory')) {
    return false;
  }
  const configPath = path.join(contract.agentDirectory, 'config.json');
  const keyPath = path.join(contract.agentDirectory, 'private.key');
  const [config, key, guard, audit] = await Promise.all([
    readPhysicalFile(configPath, 'Elydora runtime config', MAX_CONFIG_BYTES),
    readPhysicalFile(keyPath, 'Elydora private key', MAX_SECRET_BYTES),
    readPhysicalFile(contract.guardPath, 'Elydora guard runtime'),
    readPhysicalFile(contract.auditPath, 'Elydora audit runtime'),
  ]);
  if (!config || !key || !guard || !audit) return false;
  const parsed = parseStrictJsonObject(config.contents, `Elydora runtime config at ${configPath}`);
  validateRuntimeConfig(parsed, contract, configPath);
  validatePrivateKey(key.contents, keyPath);
  return guard.contents === generateGuardScript(AGENT_KEY, contract.agentId)
    && audit.contents === generateHookScript(
      AGENT_KEY,
      contract.agentId,
      { nativePayload: true },
    );
}
