import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import path from 'node:path';
import {
  inspectPhysicalDirectory,
  readPhysicalFile,
  type FileSnapshot,
} from './managed-files.js';

const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

export interface ManagedChangeSpec {
  readonly filePath: string;
  readonly label: string;
  readonly next?: string;
  readonly mode: number;
  readonly maximumBytes?: number;
  readonly expectedSource?: string;
  readonly verifyExpectedSource?: boolean;
}

export interface ManagedFileChange {
  readonly filePath: string;
  readonly label: string;
  readonly next?: string;
  readonly mode: number;
  readonly maximumBytes: number;
  readonly original?: FileSnapshot;
}

export interface ManagedDirectory {
  readonly path: string;
  readonly label: string;
}

export interface PreparedManagedTransaction {
  readonly displayName: string;
  readonly directories: readonly ManagedDirectory[];
  readonly changes: readonly ManagedFileChange[];
}

interface StagedChange {
  readonly change: ManagedFileChange;
  temporaryPath?: string;
  rollbackPath?: string;
  committedSnapshot?: FileSnapshot;
  committed: boolean;
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

function targetKey(filePath: string): string {
  const resolved = path.resolve(filePath);
  return process.platform === 'win32' ? resolved.toLowerCase() : resolved;
}

async function unlinkOptional(filePath?: string): Promise<void> {
  if (!filePath) return;
  try {
    await fsp.unlink(filePath);
  } catch (error) {
    if (!hasCode(error, 'ENOENT')) throw error;
  }
}

export async function prepareManagedFileChange(
  spec: ManagedChangeSpec,
): Promise<ManagedFileChange | undefined> {
  const maximumBytes = spec.maximumBytes ?? MAX_SOURCE_BYTES;
  if (spec.next !== undefined && Buffer.byteLength(spec.next) > maximumBytes) {
    throw new Error(`${spec.label} exceeds ${maximumBytes} bytes: ${spec.filePath}`);
  }
  const original = await readPhysicalFile(spec.filePath, spec.label, maximumBytes);
  if (spec.verifyExpectedSource && original?.contents !== spec.expectedSource) {
    throw new Error(`${spec.label} changed before installation: ${spec.filePath}`);
  }
  if (!original && spec.next === undefined) return undefined;
  return {
    filePath: spec.filePath,
    label: spec.label,
    next: spec.next,
    mode: spec.mode,
    maximumBytes,
    original,
  };
}

async function assertUnchanged(change: ManagedFileChange, displayName: string): Promise<void> {
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

async function reservePath(filePath: string, label: string): Promise<void> {
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(filePath, 'wx', 0o600);
    await handle.close();
    handle = undefined;
    await fsp.unlink(filePath);
  } catch (error) {
    const failures = [asError(error)];
    if (handle) {
      try { await handle.close(); } catch (closeError) { failures.push(asError(closeError)); }
    }
    try { await unlinkOptional(filePath); } catch (cleanupError) {
      failures.push(asError(cleanupError));
    }
    if (failures.length > 1) throw new AggregateError(failures, `Reserve ${label}`);
    throw new Error(`Reserve ${label}: ${errorMessage(error)}`, { cause: failures[0] });
  }
}

async function stage(change: ManagedFileChange, displayName: string): Promise<StagedChange> {
  await assertUnchanged(change, displayName);
  const token = randomUUID();
  const directory = path.dirname(change.filePath);
  const temporaryPath = change.next === undefined
    ? undefined
    : path.join(directory, `.${path.basename(change.filePath)}.${token}.tmp`);
  const rollbackPath = change.original
    ? path.join(directory, `.${path.basename(change.filePath)}.${token}.rollback`)
    : undefined;
  const staged: StagedChange = {
    change,
    temporaryPath,
    rollbackPath,
    committed: false,
  };
  try {
    if (temporaryPath && change.next !== undefined) {
      await writeExclusive(temporaryPath, change.next, change.mode, change.label);
    }
    if (rollbackPath && change.original) {
      if (change.next === undefined) {
        await reservePath(rollbackPath, `${change.label} rollback`);
      } else {
        await writeExclusive(
          rollbackPath,
          change.original.contents,
          change.original.mode,
          `${change.label} rollback`,
        );
      }
    }
    return staged;
  } catch (error) {
    const failures = [asError(error)];
    for (const item of [temporaryPath, rollbackPath]) {
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
  if (staged.change.next === undefined) {
    if (!staged.rollbackPath) throw new Error(`Missing rollback data for ${staged.change.label}`);
    await renameFile(staged.change.filePath, staged.rollbackPath);
  } else {
    if (!staged.temporaryPath) throw new Error(`Missing staged file for ${staged.change.label}`);
    await renameFile(staged.temporaryPath, staged.change.filePath);
  }
  staged.committed = true;
  const current = await readPhysicalFile(
    staged.change.filePath,
    staged.change.label,
    staged.change.maximumBytes,
  );
  if (staged.change.next === undefined) {
    if (current) throw new Error(`${staged.change.label} remained after removal`);
    return;
  }
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
  if (staged.change.next === undefined) {
    if (current) {
      throw new Error(
        `${staged.change.label} changed during transaction recovery: ${staged.change.filePath}`,
      );
    }
    return;
  }
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
  try {
    if (staged.change.next === undefined || staged.change.original) {
      if (!staged.rollbackPath) throw new Error(`Missing rollback data for ${staged.change.label}`);
      await renameFile(staged.rollbackPath, staged.change.filePath);
    } else {
      await unlinkOptional(staged.change.filePath);
    }
  } catch (error) {
    throw preserveRollback(staged, error);
  }
}

async function cleanup(staged: StagedChange): Promise<void> {
  await Promise.all([
    unlinkOptional(staged.temporaryPath),
    unlinkOptional(staged.rollbackPath),
  ]);
}

async function ensureManagedDirectory(directory: string, label: string): Promise<void> {
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  if (!await inspectPhysicalDirectory(directory, label)) {
    throw new Error(`${label} is missing: ${directory}`);
  }
}

function transactionDirectories(transaction: PreparedManagedTransaction): ManagedDirectory[] {
  const directories = new Map<string, ManagedDirectory>();
  for (const item of transaction.directories) directories.set(targetKey(item.path), item);
  for (const change of transaction.changes) {
    const directory = path.dirname(change.filePath);
    const key = targetKey(directory);
    if (!directories.has(key)) {
      directories.set(key, { path: directory, label: `Directory for ${change.label}` });
    }
  }
  return [...directories.values()];
}

export async function commitManagedTransaction(
  transaction: PreparedManagedTransaction,
  renameFile: RenameFile = fsp.rename,
): Promise<void> {
  const targets = new Set<string>();
  for (const change of transaction.changes) {
    const key = targetKey(change.filePath);
    if (targets.has(key)) {
      throw new Error(
        `${transaction.displayName} installation contains duplicate file target ${change.filePath}`,
      );
    }
    targets.add(key);
  }
  for (const directory of transactionDirectories(transaction)) {
    await ensureManagedDirectory(directory.path, directory.label);
  }
  const staged: StagedChange[] = [];
  try {
    for (const change of transaction.changes) staged.push(await stage(change, transaction.displayName));
    for (const item of staged) await commit(item, transaction.displayName, renameFile);
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
      `Install ${transaction.displayName} hooks: ${errorMessage(error)}`,
    );
  }
  const failures: Error[] = [];
  for (const item of staged) {
    try { await cleanup(item); } catch (error) { failures.push(asError(error)); }
  }
  if (failures.length > 0) {
    throw new AggregateError(
      failures,
      `Clean ${transaction.displayName} installation transaction files`,
    );
  }
}
