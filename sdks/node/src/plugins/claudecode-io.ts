import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  CONFIG_FILE,
  createClaudeDocument,
  parseClaudeDocument,
  type ClaudeDocument,
  type ClaudeRuntimeContract,
  type RenderedClaudeDocument,
} from './claudecode-contract.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import {
  commitManagedTransaction,
  prepareManagedFileChange,
} from './managed-transaction.js';
import { parseStrictJsonObject, type JsonObject } from './strict-json.js';

export function claudeConfigDirectory(): string {
  const configured = process.env.CLAUDE_CONFIG_DIR;
  return configured === undefined
    ? path.join(os.homedir(), '.claude')
    : path.resolve(configured);
}

export function claudeSettingsPath(): string {
  return path.join(claudeConfigDirectory(), CONFIG_FILE);
}

export async function readClaudeDocument(): Promise<ClaudeDocument> {
  const filePath = claudeSettingsPath();
  await inspectPhysicalDirectory(path.dirname(filePath), 'Claude Code configuration directory');
  const snapshot = await readPhysicalFile(filePath, 'Claude Code user settings');
  return snapshot
    ? parseClaudeDocument(filePath, snapshot.contents)
    : createClaudeDocument(filePath);
}

export async function writeClaudeDocument(rendered: RenderedClaudeDocument): Promise<void> {
  if (!rendered.changed) return;
  const change = await prepareManagedFileChange({
    filePath: rendered.document.filePath,
    label: 'Claude Code user settings',
    next: rendered.next,
    mode: 0o600,
    expectedSource: rendered.document.raw,
    verifyExpectedSource: true,
  });
  if (!change) return;
  await commitManagedTransaction({
    displayName: 'Claude Code',
    operation: 'uninstall',
    directories: [{
      path: path.dirname(rendered.document.filePath),
      label: 'Claude Code configuration directory',
    }],
    changes: [change],
  });
}

function requireNonEmptyString(value: unknown, field: string, configPath: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`Elydora runtime config ${field} is invalid: ${configPath}`);
  }
  return value;
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

function validateRuntimeConfig(
  config: JsonObject,
  contract: ClaudeRuntimeContract,
  configPath: string,
): void {
  const supported = new Set(['org_id', 'agent_id', 'kid', 'base_url', 'token', 'agent_name']);
  const extra = Object.keys(config).find((key) => !supported.has(key));
  if (extra) throw new Error(`Elydora runtime config has unsupported field "${extra}": ${configPath}`);
  requireNonEmptyString(config.org_id, 'org_id', configPath);
  requireNonEmptyString(config.kid, 'kid', configPath);
  const agentId = requireNonEmptyString(config.agent_id, 'agent_id', configPath);
  if (!sameAgentId(agentId, contract.agentId) || config.agent_name !== AGENT_KEY) {
    throw new Error(`Elydora runtime identity does not match Claude Code hooks: ${configPath}`);
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

async function runtimeContractExists(contract: ClaudeRuntimeContract): Promise<boolean> {
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  const agentDirectory = path.dirname(contract.guardPath);
  if (!samePath(path.dirname(agentDirectory), runtimeRoot)
    || !samePath(contract.auditPath, path.join(agentDirectory, 'hook.js'))) return false;
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

export async function claudeRuntimeFilesExist(
  contracts: readonly ClaudeRuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    if (await runtimeContractExists(contract)) return true;
  }
  return false;
}
