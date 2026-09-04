import os from 'node:os';
import path from 'node:path';
import { generateGuardScript, type GuardScriptOptions } from './guard-template.js';
import { generateHookScript, type HookScriptOptions } from './hook-template.js';
import { MAX_CONFIG_BYTES, MAX_SECRET_BYTES, sameAgentId, samePath } from './common.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { parseStrictJsonObject, type JsonObject } from './strict-json.js';

const GUARD_SCRIPT = 'guard.js';
const AUDIT_SCRIPT = 'hook.js';

export interface ManagedRuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface ManagedRuntimeStatusOptions {
  readonly guardOptions?: GuardScriptOptions;
  readonly auditOptions?: HookScriptOptions;
}

function requireString(value: unknown, field: string, configPath: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`Elydora runtime config ${field} is invalid: ${configPath}`);
  }
  return value;
}

function validateRuntimeConfig(
  config: JsonObject,
  contract: ManagedRuntimeContract,
  configPath: string,
  agentKey: string,
): void {
  const supported = new Set(['org_id', 'agent_id', 'kid', 'base_url', 'token', 'agent_name']);
  const extra = Object.keys(config).find((key) => !supported.has(key));
  if (extra) throw new Error(`Elydora runtime config has unsupported field "${extra}": ${configPath}`);
  requireString(config.org_id, 'org_id', configPath);
  requireString(config.kid, 'kid', configPath);
  const agentId = requireString(config.agent_id, 'agent_id', configPath);
  if (!sameAgentId(agentId, contract.agentId) || config.agent_name !== agentKey) {
    throw new Error(`Elydora runtime identity does not match ${agentKey} hooks: ${configPath}`);
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

function validContractPaths(contract: ManagedRuntimeContract): boolean {
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  const agentDirectory = path.dirname(contract.guardPath);
  return samePath(path.dirname(agentDirectory), runtimeRoot)
    && sameAgentId(path.basename(agentDirectory), contract.agentId)
    && samePath(contract.guardPath, path.join(agentDirectory, GUARD_SCRIPT))
    && samePath(contract.auditPath, path.join(agentDirectory, AUDIT_SCRIPT));
}

export async function managedRuntimeFilesExist(
  contract: ManagedRuntimeContract,
  agentKey: string,
  options: ManagedRuntimeStatusOptions = {},
): Promise<boolean> {
  if (!validContractPaths(contract)) return false;
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  const agentDirectory = path.dirname(contract.guardPath);
  if (!await inspectPhysicalDirectory(runtimeRoot, 'Elydora runtime directory')) return false;
  if (!await inspectPhysicalDirectory(agentDirectory, 'Elydora agent runtime directory')) return false;
  const configPath = path.join(agentDirectory, 'config.json');
  const keyPath = path.join(agentDirectory, 'private.key');
  const [config, key, guard, audit] = await Promise.all([
    readPhysicalFile(configPath, 'Elydora runtime config', MAX_CONFIG_BYTES),
    readPhysicalFile(keyPath, 'Elydora private key', MAX_SECRET_BYTES),
    readPhysicalFile(contract.guardPath, 'Elydora guard runtime'),
    readPhysicalFile(contract.auditPath, 'Elydora audit runtime'),
  ]);
  if (!config || !key || !guard || !audit) return false;
  validateRuntimeConfig(
    parseStrictJsonObject(config.contents, `Elydora runtime config at ${configPath}`),
    contract,
    configPath,
    agentKey,
  );
  validatePrivateKey(key.contents, keyPath);
  return guard.contents === generateGuardScript(agentKey, contract.agentId, options.guardOptions)
    && audit.contents === generateHookScript(
      agentKey,
      contract.agentId,
      options.auditOptions ?? { nativePayload: true },
    );
}

// Presence-only check: identity mismatch reports false; parse failures still throw.
export async function managedRuntimePresent(
  contract: ManagedRuntimeContract,
  agentKey: string,
): Promise<boolean> {
  const agentDirectory = path.dirname(contract.guardPath);
  const configPath = path.join(agentDirectory, 'config.json');
  const snapshot = await readPhysicalFile(configPath, 'Elydora runtime config', MAX_CONFIG_BYTES);
  if (!snapshot) return false;
  const config = parseStrictJsonObject(snapshot.contents, `Elydora runtime config at ${configPath}`);
  if (config.agent_name !== agentKey || !sameAgentId(config.agent_id, contract.agentId)) {
    return false;
  }
  const files = await Promise.all([
    readPhysicalFile(contract.guardPath, 'Elydora guard runtime'),
    readPhysicalFile(contract.auditPath, 'Elydora audit runtime'),
    readPhysicalFile(path.join(agentDirectory, 'private.key'), 'Elydora private key', MAX_SECRET_BYTES),
  ]);
  return files.every(Boolean);
}
