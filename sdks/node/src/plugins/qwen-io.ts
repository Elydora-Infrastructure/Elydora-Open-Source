import os from 'node:os';
import path from 'node:path';
import { generateGuardScript } from './guard-template.js';
import { generateHookScript } from './hook-template.js';
import {
  AGENT_KEY,
  type QwenRuntimeContract,
} from './qwen-contract.js';
import { sameQwenAgentId, sameQwenPath } from './qwen-command.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { parseStrictJsonObject, type JsonObject } from './strict-json.js';

function requireNonEmptyString(value: unknown, field: string, configPath: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`Elydora runtime config ${field} is invalid: ${configPath}`);
  }
  return value;
}

function validateRuntimeConfig(
  config: JsonObject,
  contract: QwenRuntimeContract,
  configPath: string,
): void {
  const supported = new Set(['org_id', 'agent_id', 'kid', 'base_url', 'token', 'agent_name']);
  const extra = Object.keys(config).find((key) => !supported.has(key));
  if (extra) {
    throw new Error(`Elydora runtime config has unsupported field "${extra}": ${configPath}`);
  }
  requireNonEmptyString(config.org_id, 'org_id', configPath);
  requireNonEmptyString(config.kid, 'kid', configPath);
  const agentId = requireNonEmptyString(config.agent_id, 'agent_id', configPath);
  if (!sameQwenAgentId(agentId, contract.agentId) || config.agent_name !== AGENT_KEY) {
    throw new Error(`Elydora runtime identity does not match Qwen Code hooks: ${configPath}`);
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

async function runtimeContractExists(contract: QwenRuntimeContract): Promise<boolean> {
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  const agentDirectory = path.dirname(contract.guardPath);
  if (!sameQwenPath(path.dirname(agentDirectory), runtimeRoot)
    || !sameQwenPath(contract.auditPath, path.join(agentDirectory, 'hook.js'))) return false;
  if (!await inspectPhysicalDirectory(runtimeRoot, 'Elydora runtime directory')) return false;
  if (!await inspectPhysicalDirectory(agentDirectory, 'Elydora agent runtime directory')) {
    return false;
  }
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
  return guard.contents === generateGuardScript(AGENT_KEY, contract.agentId)
    && audit.contents === generateHookScript(
      AGENT_KEY,
      contract.agentId,
      { nativePayload: true },
    );
}

export async function qwenRuntimeFilesExist(
  contracts: readonly QwenRuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    if (await runtimeContractExists(contract)) return true;
  }
  return false;
}
