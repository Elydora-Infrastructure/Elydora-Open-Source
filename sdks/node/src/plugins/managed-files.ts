import { constants } from 'node:fs';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';

const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

export interface FileSnapshot {
  readonly contents: string;
  readonly device: bigint | number;
  readonly inode: bigint | number;
  readonly mode: number;
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function hasCode(error: unknown, code: string): boolean {
  return typeof error === 'object' && error !== null && 'code' in error && error.code === code;
}

export async function readPhysicalFile(
  filePath: string,
  label: string,
  maximumBytes = MAX_SOURCE_BYTES,
): Promise<FileSnapshot | undefined> {
  let before;
  try {
    before = await fsp.lstat(filePath, { bigint: true });
  } catch (error) {
    if (hasCode(error, 'ENOENT') || hasCode(error, 'ENOTDIR')) return undefined;
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

export async function inspectPhysicalDirectory(directory: string, label: string): Promise<boolean> {
  let metadata;
  try {
    metadata = await fsp.lstat(directory);
  } catch (error) {
    if (hasCode(error, 'ENOENT') || hasCode(error, 'ENOTDIR')) return false;
    throw new Error(`Inspect ${label} at ${directory}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!metadata.isDirectory() || metadata.isSymbolicLink()) {
    throw new Error(`${label} is not a physical directory: ${directory}`);
  }
  return true;
}
