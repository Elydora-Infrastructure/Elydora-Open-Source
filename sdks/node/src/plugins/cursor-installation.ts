import { randomUUID } from 'node:crypto';
import { constants } from 'node:fs';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { ensurePrivateDirectory, resolvePrivateChildDirectory } from '../runtime-paths.js';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type JsonObject,
  type RenderedDocument,
  parseStrictJsonObject,
  samePath,
} from './cursor-contract.js';
import { generateGuardScript } from './guard-template.js';
import { generateHookScript } from './hook-template.js';
import { cursorConfigPath } from './cursor-io.js';

const MAX_SECRET_BYTES = 64 * 1024;
const MAX_CONFIG_BYTES = 512 * 1024;
const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

interface FileSnapshot {
  readonly contents: string;
  readonly device: bigint | number;
  readonly inode: bigint | number;
  readonly mode: number;
}

interface FileChange {
  readonly filePath: string;
  readonly label: string;
  readonly next: string;
  readonly mode: number;
  readonly original?: FileSnapshot;
}

interface StagedChange {
  readonly change: FileChange;
  readonly temporaryPath: string;
  readonly rollbackPath?: string;
  committed: boolean;
}

export interface CursorRuntimePaths {
  readonly runtimeRoot: string;
  readonly agentDirectory: string;
  readonly configPath: string;
  readonly keyPath: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface PreparedCursorInstallation {
  readonly paths: CursorRuntimePaths;
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

async function readPhysicalFile(
  filePath: string,
  label: string,
  maximumBytes = MAX_SOURCE_BYTES,
): Promise<FileSnapshot | undefined> {
  let before;
  try {
    before = await fsp.lstat(filePath, { bigint: true });
  } catch (error) {
    if (hasCode(error, 'ENOENT')) return undefined;
    throw new Error(`Inspect ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!before.isFile() || before.isSymbolicLink()) {
    throw new Error(`${label} path is not a physical file: ${filePath}`);
  }
  if (before.size > BigInt(maximumBytes)) {
    throw new Error(`${label} exceeds ${maximumBytes} bytes: ${filePath}`);
  }
  let flags = constants.O_RDONLY;
  if (process.platform !== 'win32' && typeof constants.O_NOFOLLOW === 'number') {
    flags |= constants.O_NOFOLLOW;
  }
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(filePath, flags);
    const after = await handle.stat({ bigint: true });
    if (!after.isFile() || before.dev !== after.dev || before.ino !== after.ino) {
      throw new Error(`${label} changed while opening: ${filePath}`);
    }
    const contents = await handle.readFile('utf-8');
    if (Buffer.byteLength(contents) > maximumBytes) {
      throw new Error(`${label} exceeds ${maximumBytes} bytes: ${filePath}`);
    }
    return {
      contents,
      device: after.dev,
      inode: after.ino,
      mode: Number(after.mode & 0o777n),
    };
  } finally {
    await handle?.close();
  }
}

async function inspectDirectory(directory: string, label: string): Promise<boolean> {
  let metadata;
  try {
    metadata = await fsp.lstat(directory);
  } catch (error) {
    if (hasCode(error, 'ENOENT')) return false;
    throw new Error(`Inspect ${label} at ${directory}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!metadata.isDirectory() || metadata.isSymbolicLink()) {
    throw new Error(`${label} is not a physical directory: ${directory}`);
  }
  return true;
}

function validateInstallConfig(config: InstallConfig): void {
  for (const [field, value] of [
    ['orgId', config.orgId],
    ['agentId', config.agentId],
    ['kid', config.kid],
    ['privateKey', config.privateKey],
  ] as const) {
    if (!value) throw new Error(`${field} is required`);
  }
  if (config.agentName !== AGENT_KEY) {
    throw new Error(`Cursor installation requires agentName ${AGENT_KEY}`);
  }
  const seed = Buffer.from(config.privateKey, 'base64url');
  if (seed.length !== 32 || seed.toString('base64url') !== config.privateKey) {
    throw new Error('privateKey must be a canonical 32-byte base64url value');
  }
  const baseUrl = new URL(config.baseUrl);
  if (!['http:', 'https:'].includes(baseUrl.protocol)) {
    throw new Error('baseUrl must use HTTP or HTTPS');
  }
}

export function cursorRuntimePaths(config: InstallConfig): CursorRuntimePaths {
  validateInstallConfig(config);
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  const agentDirectory = resolvePrivateChildDirectory(runtimeRoot, config.agentId);
  const guardPath = path.join(agentDirectory, GUARD_SCRIPT);
  const auditPath = path.join(agentDirectory, AUDIT_SCRIPT);
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

async function validateRuntimeIdentity(paths: CursorRuntimePaths): Promise<void> {
  const runtimeRootExists = await inspectDirectory(paths.runtimeRoot, 'Elydora runtime directory');
  if (!runtimeRootExists) return;
  const agentDirectoryExists = await inspectDirectory(
    paths.agentDirectory,
    'Elydora agent runtime directory',
  );
  if (!agentDirectoryExists) return;
  const config = await readPhysicalFile(paths.configPath, 'Elydora runtime config', MAX_CONFIG_BYTES);
  const artifacts = await Promise.all([
    readPhysicalFile(paths.keyPath, 'Elydora private key', MAX_SECRET_BYTES),
    readPhysicalFile(paths.guardPath, 'Elydora guard runtime'),
    readPhysicalFile(paths.auditPath, 'Elydora audit runtime'),
    readPhysicalFile(path.join(paths.agentDirectory, 'chain-state.json'), 'Elydora chain state'),
    readPhysicalFile(path.join(paths.agentDirectory, 'status-cache.json'), 'Elydora status cache'),
  ]);
  if (!config) {
    if (artifacts.some(Boolean)) {
      throw new Error(
        `Elydora runtime identity cannot be verified without config.json: ${paths.agentDirectory}`,
      );
    }
    return;
  }
  const value = parseStrictJsonObject(config.contents, `Elydora runtime config at ${paths.configPath}`);
  if (value.agent_name !== AGENT_KEY || !sameAgentId(value.agent_id, path.basename(paths.agentDirectory))) {
    throw new Error(
      `Elydora runtime config identity does not match Cursor agent ${path.basename(paths.agentDirectory)}: ${paths.configPath}`,
    );
  }
}

export async function preflightCursorInstallation(config: InstallConfig): Promise<CursorRuntimePaths> {
  const paths = cursorRuntimePaths(config);
  await inspectDirectory(path.dirname(cursorConfigPath()), 'Cursor hooks directory');
  await validateRuntimeIdentity(paths);
  return paths;
}

function runtimeConfig(config: InstallConfig): string {
  const value: JsonObject = {
    org_id: config.orgId,
    agent_id: config.agentId,
    kid: config.kid,
    base_url: config.baseUrl,
    ...(config.token ? { token: config.token } : {}),
    agent_name: AGENT_KEY,
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
    original: await readPhysicalFile(filePath, label, maximumBytes),
  };
}

export async function prepareCursorInstallation(
  config: InstallConfig,
  rendered: RenderedDocument,
): Promise<PreparedCursorInstallation> {
  const paths = await preflightCursorInstallation(config);
  if (!rendered.changed && rendered.document.raw === undefined) {
    throw new Error('Cursor hook installation did not produce a configuration document');
  }
  const hooksConfig = rendered.next ?? rendered.document.raw;
  if (hooksConfig === undefined) throw new Error('Cursor hook configuration is missing');
  const changes = await Promise.all([
    prepareChange(
      paths.guardPath,
      'Elydora guard runtime',
      generateGuardScript(AGENT_KEY, config.agentId, {
        failClosed: true,
        successOutput: '{"permission":"allow"}\n',
      }),
      0o700,
    ),
    prepareChange(paths.configPath, 'Elydora runtime config', runtimeConfig(config), 0o600, MAX_CONFIG_BYTES),
    prepareChange(paths.keyPath, 'Elydora private key', config.privateKey, 0o600, MAX_SECRET_BYTES),
    prepareChange(
      paths.auditPath,
      'Elydora audit runtime',
      generateHookScript(AGENT_KEY, config.agentId, {
        failClosed: true,
        nativePayload: true,
        successOutput: '{}\n',
      }),
      0o700,
    ),
    prepareChange(
      rendered.document.filePath,
      'Cursor user hooks',
      hooksConfig,
      0o600,
      MAX_SOURCE_BYTES,
    ),
  ]);
  const hooksSource = changes.at(-1)!.original?.contents;
  if (hooksSource !== rendered.document.raw) {
    throw new Error(
      `Cursor user hooks changed before installation: ${rendered.document.filePath}`,
    );
  }
  await validateRuntimeIdentity(paths);
  return { paths, changes };
}

async function unlinkOptional(filePath: string): Promise<void> {
  try {
    await fsp.unlink(filePath);
  } catch (error) {
    if (!hasCode(error, 'ENOENT')) throw error;
  }
}

async function assertUnchanged(change: FileChange): Promise<void> {
  const current = await readPhysicalFile(change.filePath, change.label);
  if ((!current && change.original)
    || (current && !change.original)
    || (current && change.original && (
      current.contents !== change.original.contents
      || current.device !== change.original.device
      || current.inode !== change.original.inode
    ))) {
    throw new Error(`${change.label} changed during Cursor installation: ${change.filePath}`);
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

async function stage(change: FileChange): Promise<StagedChange> {
  await assertUnchanged(change);
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

async function commit(staged: StagedChange, renameFile: RenameFile): Promise<void> {
  await assertUnchanged(staged.change);
  await renameFile(staged.temporaryPath, staged.change.filePath);
  staged.committed = true;
}

async function rollback(staged: StagedChange, renameFile: RenameFile): Promise<void> {
  if (!staged.committed) return;
  if (!staged.change.original) {
    await unlinkOptional(staged.change.filePath);
    return;
  }
  if (!staged.rollbackPath) throw new Error(`Missing rollback data for ${staged.change.label}`);
  await renameFile(staged.rollbackPath, staged.change.filePath);
}

async function cleanup(staged: StagedChange): Promise<void> {
  await Promise.all(
    [staged.temporaryPath, staged.rollbackPath].filter(Boolean).map((item) => unlinkOptional(item!)),
  );
}

async function ensureCursorDirectory(directory: string): Promise<void> {
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  await inspectDirectory(directory, 'Cursor hooks directory');
}

export async function commitCursorInstallation(
  prepared: PreparedCursorInstallation,
  renameFile: RenameFile = fsp.rename,
): Promise<void> {
  await ensurePrivateDirectory(prepared.paths.runtimeRoot);
  await ensurePrivateDirectory(prepared.paths.agentDirectory);
  await ensureCursorDirectory(path.dirname(prepared.changes.at(-1)!.filePath));
  const staged: StagedChange[] = [];
  try {
    for (const change of prepared.changes) staged.push(await stage(change));
    for (const item of staged) await commit(item, renameFile);
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
    throw new AggregateError(failures, `Install Cursor hooks: ${errorMessage(error)}`);
  }
  const failures: Error[] = [];
  for (const item of staged) {
    try { await cleanup(item); } catch (error) { failures.push(asError(error)); }
  }
  if (failures.length > 0) {
    throw new AggregateError(failures, 'Clean Cursor installation transaction files');
  }
}
