import os from 'node:os';
import path from 'node:path';
import { ensurePrivateDirectory, resolvePrivateChildDirectory } from '../runtime-paths.js';
import type { InstallConfig } from './base.js';
import { generateGuardScript, type GuardScriptOptions } from './guard-template.js';
import { generateHookScript, type HookScriptOptions } from './hook-template.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import {
  commitManagedTransaction,
  prepareManagedFileChange,
  type ManagedDirectory,
  type ManagedFileChange,
  type PreparedManagedTransaction,
  type RenameFile,
} from './managed-transaction.js';
import { parseStrictJsonObject } from './strict-json.js';

const MAX_SECRET_BYTES = 64 * 1024;
const MAX_CONFIG_BYTES = 512 * 1024;

export interface ManagedRuntimePaths {
  readonly runtimeRoot: string;
  readonly agentDirectory: string;
  readonly configPath: string;
  readonly keyPath: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface ManagedHookLocation {
  readonly directoryLabel: string;
  readonly filePath: string;
}

export interface ManagedHookSource extends ManagedHookLocation {
  readonly label: string;
  readonly expectedSource?: string;
  readonly source: string;
}

export interface ManagedInstallationSpec {
  readonly agentKey: string;
  readonly displayName: string;
  readonly hookSources: readonly ManagedHookSource[];
  readonly config: InstallConfig;
  readonly guardOptions?: GuardScriptOptions;
  readonly auditOptions?: HookScriptOptions;
}

export interface ManagedPreflightSpec {
  readonly agentKey: string;
  readonly hookLocations: readonly ManagedHookLocation[];
  readonly config: InstallConfig;
}

export interface PreparedManagedInstallation {
  readonly paths: ManagedRuntimePaths;
  readonly transaction: PreparedManagedTransaction;
}

export type { RenameFile };

export function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

function validateInstallConfig(config: InstallConfig, agentKey: string): void {
  for (const [field, value] of [
    ['agentName', config.agentName],
    ['orgId', config.orgId],
    ['agentId', config.agentId],
    ['kid', config.kid],
    ['privateKey', config.privateKey],
    ['baseUrl', config.baseUrl],
    ['guardScriptPath', config.guardScriptPath],
    ['hookScriptPath', config.hookScriptPath],
  ] as const) {
    if (typeof value !== 'string' || !value) throw new Error(`${field} is required`);
  }
  if (config.token !== undefined && (typeof config.token !== 'string' || !config.token)) {
    throw new Error('token must be a non-empty string when provided');
  }
  if (config.agentName !== agentKey) {
    throw new Error(`${agentKey} installation requires agentName ${agentKey}`);
  }
  const seed = Buffer.from(config.privateKey, 'base64url');
  if (seed.length !== 32 || seed.toString('base64url') !== config.privateKey) {
    throw new Error('privateKey must be a canonical 32-byte base64url value');
  }
  const baseUrl = new URL(config.baseUrl);
  if (!['http:', 'https:'].includes(baseUrl.protocol) || !baseUrl.hostname) {
    throw new Error('baseUrl must use HTTP or HTTPS');
  }
  if (baseUrl.username || baseUrl.password || baseUrl.search || baseUrl.hash) {
    throw new Error('baseUrl must exclude credentials, query parameters, and fragments');
  }
}

export function managedRuntimePaths(
  config: InstallConfig,
  agentKey: string,
  guardScript: string,
  auditScript: string,
): ManagedRuntimePaths {
  validateInstallConfig(config, agentKey);
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  const agentDirectory = resolvePrivateChildDirectory(runtimeRoot, config.agentId);
  const guardPath = path.join(agentDirectory, guardScript);
  const auditPath = path.join(agentDirectory, auditScript);
  if (!samePath(config.guardScriptPath, guardPath)) {
    throw new Error(`Elydora guard runtime must use the managed agent directory: ${guardPath}`);
  }
  if (!samePath(config.hookScriptPath, auditPath)) {
    throw new Error(`Elydora audit runtime must use the managed agent directory: ${auditPath}`);
  }
  return {
    runtimeRoot,
    agentDirectory,
    configPath: path.join(agentDirectory, 'config.json'),
    keyPath: path.join(agentDirectory, 'private.key'),
    guardPath,
    auditPath,
  };
}

function sameAgentId(left: unknown, right: string): boolean {
  return typeof left === 'string' && (process.platform === 'win32'
    ? left.toLowerCase() === right.toLowerCase()
    : left === right);
}

async function validateRuntimeIdentity(
  paths: ManagedRuntimePaths,
  agentKey: string,
): Promise<void> {
  if (!await inspectPhysicalDirectory(paths.runtimeRoot, 'Elydora runtime directory')) return;
  if (!await inspectPhysicalDirectory(paths.agentDirectory, 'Elydora agent runtime directory')) return;
  const config = await readPhysicalFile(paths.configPath, 'Elydora runtime config', MAX_CONFIG_BYTES);
  const artifacts = await Promise.all([
    readPhysicalFile(paths.keyPath, 'Elydora private key', MAX_SECRET_BYTES),
    readPhysicalFile(paths.guardPath, 'Elydora guard runtime'),
    readPhysicalFile(paths.auditPath, 'Elydora audit runtime'),
    readPhysicalFile(path.join(paths.agentDirectory, 'chain-state.json'), 'Elydora chain state'),
    readPhysicalFile(path.join(paths.agentDirectory, 'status-cache.json'), 'Elydora status cache'),
    readPhysicalFile(path.join(paths.agentDirectory, 'error.log'), 'Elydora error log'),
  ]);
  if (!config) {
    if (artifacts.some(Boolean)) {
      throw new Error(
        `Elydora runtime identity cannot be verified without config.json: ${paths.agentDirectory}`,
      );
    }
    return;
  }
  const label = `Elydora runtime config at ${paths.configPath}`;
  const value = parseStrictJsonObject(config.contents, label);
  if (value.agent_name !== agentKey
    || !sameAgentId(value.agent_id, path.basename(paths.agentDirectory))) {
    throw new Error(
      `Elydora runtime config identity does not match ${agentKey} agent ${path.basename(paths.agentDirectory)}: ${paths.configPath}`,
    );
  }
}

export async function preflightManagedInstallation(
  spec: ManagedPreflightSpec,
  guardScript: string,
  auditScript: string,
): Promise<ManagedRuntimePaths> {
  const paths = managedRuntimePaths(spec.config, spec.agentKey, guardScript, auditScript);
  for (const location of spec.hookLocations) {
    await inspectPhysicalDirectory(path.dirname(location.filePath), location.directoryLabel);
  }
  await validateRuntimeIdentity(paths, spec.agentKey);
  return paths;
}

function runtimeConfig(config: InstallConfig, agentKey: string): string {
  const value = {
    org_id: config.orgId,
    agent_id: config.agentId,
    kid: config.kid,
    base_url: config.baseUrl,
    ...(config.token ? { token: config.token } : {}),
    agent_name: agentKey,
  };
  const encoded = `${JSON.stringify(value, null, 2)}\n`;
  if (Buffer.byteLength(encoded) > MAX_CONFIG_BYTES) {
    throw new Error(`Elydora runtime config exceeds ${MAX_CONFIG_BYTES} bytes`);
  }
  return encoded;
}

function hookDirectories(sources: readonly ManagedHookSource[]): ManagedDirectory[] {
  return sources.map((source) => ({
    path: path.dirname(source.filePath),
    label: source.directoryLabel,
  }));
}

export async function prepareManagedInstallation(
  spec: ManagedInstallationSpec,
  guardScript: string,
  auditScript: string,
): Promise<PreparedManagedInstallation> {
  if (spec.hookSources.length === 0) throw new Error('At least one hook source is required');
  const paths = await preflightManagedInstallation({
    agentKey: spec.agentKey,
    hookLocations: spec.hookSources,
    config: spec.config,
  }, guardScript, auditScript);
  const changes = await Promise.all([
    prepareManagedFileChange({
      filePath: paths.guardPath,
      label: 'Elydora guard runtime',
      next: generateGuardScript(spec.agentKey, spec.config.agentId, spec.guardOptions),
      mode: 0o700,
    }),
    prepareManagedFileChange({
      filePath: paths.configPath,
      label: 'Elydora runtime config',
      next: runtimeConfig(spec.config, spec.agentKey),
      mode: 0o600,
      maximumBytes: MAX_CONFIG_BYTES,
    }),
    prepareManagedFileChange({
      filePath: paths.keyPath,
      label: 'Elydora private key',
      next: spec.config.privateKey,
      mode: 0o600,
      maximumBytes: MAX_SECRET_BYTES,
    }),
    prepareManagedFileChange({
      filePath: paths.auditPath,
      label: 'Elydora audit runtime',
      next: generateHookScript(spec.agentKey, spec.config.agentId, spec.auditOptions),
      mode: 0o700,
    }),
    ...spec.hookSources.map((source) => prepareManagedFileChange({
      filePath: source.filePath,
      label: source.label,
      next: source.source,
      mode: 0o600,
      expectedSource: source.expectedSource,
      verifyExpectedSource: true,
    })),
  ]);
  await validateRuntimeIdentity(paths, spec.agentKey);
  return {
    paths,
    transaction: {
      displayName: spec.displayName,
      directories: hookDirectories(spec.hookSources),
      changes: changes.filter((change): change is ManagedFileChange => change !== undefined),
    },
  };
}

export async function commitManagedInstallation(
  prepared: PreparedManagedInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await ensurePrivateDirectory(prepared.paths.runtimeRoot);
  await ensurePrivateDirectory(prepared.paths.agentDirectory);
  await commitManagedTransaction(prepared.transaction, renameFile);
}
