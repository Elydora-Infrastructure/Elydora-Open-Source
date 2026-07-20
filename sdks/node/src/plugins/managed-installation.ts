import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { ensurePrivateDirectory, resolvePrivateChildDirectory } from '../runtime-paths.js';
import type { InstallConfig } from './base.js';
import { generateGuardScript, type GuardScriptOptions } from './guard-template.js';
import { generateHookScript, type HookScriptOptions } from './hook-template.js';
import {
  inspectPhysicalDirectory,
  readPhysicalFile,
  type FileSnapshot,
} from './managed-files.js';
import { parseStrictJsonObject } from './strict-json.js';

const MAX_SECRET_BYTES = 64 * 1024;
const MAX_CONFIG_BYTES = 512 * 1024;
const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

interface FileChange {
  readonly filePath: string;
  readonly label: string;
  readonly next: string;
  readonly mode: number;
  readonly maximumBytes: number;
  readonly original?: FileSnapshot;
}
interface StagedChange {
  readonly change: FileChange;
  readonly temporaryPath: string;
  rollbackPath?: string;
  committedSnapshot?: FileSnapshot;
  committed: boolean;
}
export interface ManagedRuntimePaths {
  readonly runtimeRoot: string;
  readonly agentDirectory: string;
  readonly configPath: string;
  readonly keyPath: string;
  readonly guardPath: string;
  readonly auditPath: string;
}
export interface ManagedInstallationSpec {
  readonly agentKey: string;
  readonly displayName: string;
  readonly hooksDirectoryLabel: string;
  readonly hooksLabel: string;
  readonly hooksPath: string;
  readonly expectedHooksSource?: string;
  readonly hooksSource: string;
  readonly config: InstallConfig;
  readonly guardOptions?: GuardScriptOptions;
  readonly auditOptions?: HookScriptOptions;
}
export interface ManagedPreflightSpec {
  readonly agentKey: string;
  readonly hooksDirectoryLabel: string;
  readonly hooksPath: string;
  readonly config: InstallConfig;
}
export interface PreparedManagedInstallation {
  readonly displayName: string;
  readonly hooksDirectoryLabel: string;
  readonly paths: ManagedRuntimePaths;
  readonly changes: readonly FileChange[];
}
export type RenameFile = (source: string, destination: string) => Promise<void>;
function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
function hasCode(error: unknown, code: string): boolean {
  return typeof error === 'object' && error !== null && 'code' in error && error.code === code;
}

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
  if (!['http:', 'https:'].includes(baseUrl.protocol)) {
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
  await inspectPhysicalDirectory(path.dirname(spec.hooksPath), spec.hooksDirectoryLabel);
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

async function prepareChange(
  filePath: string,
  label: string,
  next: string,
  mode: number,
  maximumBytes = MAX_SOURCE_BYTES,
): Promise<FileChange> {
  return {
    filePath,
    label,
    next,
    mode,
    maximumBytes,
    original: await readPhysicalFile(filePath, label, maximumBytes),
  };
}

export async function prepareManagedInstallation(
  spec: ManagedInstallationSpec,
  guardScript: string,
  auditScript: string,
): Promise<PreparedManagedInstallation> {
  const paths = await preflightManagedInstallation(spec, guardScript, auditScript);
  const changes = await Promise.all([
    prepareChange(
      paths.guardPath,
      'Elydora guard runtime',
      generateGuardScript(spec.agentKey, spec.config.agentId, spec.guardOptions),
      0o700,
    ),
    prepareChange(
      paths.configPath,
      'Elydora runtime config',
      runtimeConfig(spec.config, spec.agentKey),
      0o600,
      MAX_CONFIG_BYTES,
    ),
    prepareChange(
      paths.keyPath,
      'Elydora private key',
      spec.config.privateKey,
      0o600,
      MAX_SECRET_BYTES,
    ),
    prepareChange(
      paths.auditPath,
      'Elydora audit runtime',
      generateHookScript(spec.agentKey, spec.config.agentId, spec.auditOptions),
      0o700,
    ),
    prepareChange(spec.hooksPath, spec.hooksLabel, spec.hooksSource, 0o600),
  ]);
  const hooksSource = changes.at(-1)!.original?.contents;
  if (hooksSource !== spec.expectedHooksSource) {
    throw new Error(`${spec.hooksLabel} changed before installation: ${spec.hooksPath}`);
  }
  await validateRuntimeIdentity(paths, spec.agentKey);
  return {
    displayName: spec.displayName,
    hooksDirectoryLabel: spec.hooksDirectoryLabel,
    paths,
    changes,
  };
}

async function unlinkOptional(filePath: string): Promise<void> {
  try {
    await fsp.unlink(filePath);
  } catch (error) {
    if (!hasCode(error, 'ENOENT')) throw error;
  }
}

async function assertUnchanged(change: FileChange, displayName: string): Promise<void> {
  const current = await readPhysicalFile(change.filePath, change.label, change.maximumBytes);
  if ((!current && change.original)
    || (current && !change.original)
    || (current && change.original && (
      current.contents !== change.original.contents
      || current.device !== change.original.device
      || current.inode !== change.original.inode
    ))) {
    throw new Error(`${change.label} changed during ${displayName} installation: ${change.filePath}`);
  }
}

async function writeExclusive(
  filePath: string,
  contents: string,
  mode: number,
  label: string,
): Promise<void> {
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(filePath, 'wx', mode);
    await handle.writeFile(contents, 'utf-8');
    await handle.sync();
    await handle.close();
    handle = undefined;
    if (process.platform !== 'win32') await fsp.chmod(filePath, mode);
  } catch (error) {
    const failures = [asError(error)];
    if (handle) {
      try { await handle.close(); } catch (closeError) { failures.push(asError(closeError)); }
    }
    try { await unlinkOptional(filePath); } catch (cleanupError) {
      failures.push(asError(cleanupError));
    }
    if (failures.length > 1) throw new AggregateError(failures, `Stage ${label}`);
    throw new Error(`Stage ${label}: ${errorMessage(error)}`, { cause: failures[0] });
  }
}

async function stage(change: FileChange, displayName: string): Promise<StagedChange> {
  await assertUnchanged(change, displayName);
  const token = randomUUID();
  const directory = path.dirname(change.filePath);
  const temporaryPath = path.join(directory, `.${path.basename(change.filePath)}.${token}.tmp`);
  const rollbackPath = change.original
    ? path.join(directory, `.${path.basename(change.filePath)}.${token}.rollback`)
    : undefined;
  try {
    await writeExclusive(temporaryPath, change.next, change.mode, change.label);
    if (rollbackPath && change.original) {
      await writeExclusive(
        rollbackPath,
        change.original.contents,
        change.original.mode,
        `${change.label} rollback`,
      );
    }
    return { change, temporaryPath, rollbackPath, committed: false };
  } catch (error) {
    const failures = [asError(error)];
    for (const item of [temporaryPath, rollbackPath]) {
      if (!item) continue;
      try { await unlinkOptional(item); } catch (cleanupError) {
        failures.push(asError(cleanupError));
      }
    }
    if (failures.length > 1) throw new AggregateError(failures, `Stage ${change.label}`);
    throw error;
  }
}

async function commit(
  staged: StagedChange,
  displayName: string,
  renameFile: RenameFile,
): Promise<void> {
  await assertUnchanged(staged.change, displayName);
  await renameFile(staged.temporaryPath, staged.change.filePath);
  staged.committed = true;
  const current = await readPhysicalFile(
    staged.change.filePath,
    staged.change.label,
    staged.change.maximumBytes,
  );
  if (!current || current.contents !== staged.change.next) {
    throw new Error(
      `${staged.change.label} changed immediately after commit: ${staged.change.filePath}`,
    );
  }
  staged.committedSnapshot = current;
}

async function assertCommittedUnchanged(staged: StagedChange): Promise<void> {
  const current = await readPhysicalFile(
    staged.change.filePath,
    staged.change.label,
    staged.change.maximumBytes,
  );
  const committed = staged.committedSnapshot;
  if (!current || !committed
    || current.contents !== committed.contents
    || current.device !== committed.device
    || current.inode !== committed.inode) {
    throw new Error(
      `${staged.change.label} changed during transaction recovery: ${staged.change.filePath}`,
    );
  }
}

function preserveRollback(staged: StagedChange, cause: unknown): Error {
  if (!staged.rollbackPath) return asError(cause);
  const rollbackPath = staged.rollbackPath;
  staged.rollbackPath = undefined;
  return new Error(
    `${errorMessage(cause)}; original content preserved at ${rollbackPath}`,
    { cause: asError(cause) },
  );
}

async function rollback(staged: StagedChange, renameFile: RenameFile): Promise<void> {
  if (!staged.committed) return;
  try {
    await assertCommittedUnchanged(staged);
  } catch (error) {
    throw preserveRollback(staged, error);
  }
  if (!staged.change.original) {
    await unlinkOptional(staged.change.filePath);
    return;
  }
  if (!staged.rollbackPath) throw new Error(`Missing rollback data for ${staged.change.label}`);
  try {
    await renameFile(staged.rollbackPath, staged.change.filePath);
  } catch (error) {
    throw preserveRollback(staged, error);
  }
}

async function cleanup(staged: StagedChange): Promise<void> {
  await Promise.all(
    [staged.temporaryPath, staged.rollbackPath].filter(Boolean).map((item) => unlinkOptional(item!)),
  );
}

async function ensureManagedDirectory(directory: string, label: string): Promise<void> {
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  if (!await inspectPhysicalDirectory(directory, label)) {
    throw new Error(`${label} is missing: ${directory}`);
  }
}

export async function commitManagedInstallation(
  prepared: PreparedManagedInstallation,
  renameFile: RenameFile = fsp.rename,
): Promise<void> {
  await ensurePrivateDirectory(prepared.paths.runtimeRoot);
  await ensurePrivateDirectory(prepared.paths.agentDirectory);
  await ensureManagedDirectory(
    path.dirname(prepared.changes.at(-1)!.filePath),
    prepared.hooksDirectoryLabel,
  );
  const staged: StagedChange[] = [];
  try {
    for (const change of prepared.changes) {
      staged.push(await stage(change, prepared.displayName));
    }
    for (const item of staged) await commit(item, prepared.displayName, renameFile);
  } catch (error) {
    const failures = [asError(error)];
    for (const item of [...staged].reverse()) {
      try { await rollback(item, renameFile); } catch (rollbackError) {
        failures.push(asError(rollbackError));
      }
    }
    for (const item of staged) {
      try { await cleanup(item); } catch (cleanupError) {
        failures.push(asError(cleanupError));
      }
    }
    throw new AggregateError(
      failures,
      `Install ${prepared.displayName} hooks: ${errorMessage(error)}`,
    );
  }
  const failures: Error[] = [];
  for (const item of staged) {
    try { await cleanup(item); } catch (error) { failures.push(asError(error)); }
  }
  if (failures.length > 0) {
    throw new AggregateError(
      failures,
      `Clean ${prepared.displayName} installation transaction files`,
    );
  }
}
