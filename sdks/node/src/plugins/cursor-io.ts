import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  CONFIG_FILE,
  type CursorDocument,
  type JsonObject,
  type RenderedDocument,
  type RuntimeContract,
  createDocument,
  isObject,
  parseDocument,
  parseStrictJsonObject,
} from './cursor-contract.js';

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function hasErrorCode(error: unknown, code: string): boolean {
  return isObject(error) && error.code === code;
}

export function cursorConfigPath(): string {
  return path.join(os.homedir(), '.cursor', CONFIG_FILE);
}

async function readOptionalPhysical(filePath: string, label: string): Promise<string | undefined> {
  let metadata;
  try {
    metadata = await fsp.lstat(filePath);
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT') || hasErrorCode(error, 'ENOTDIR')) return undefined;
    throw new Error(`Inspect ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!metadata.isFile() || metadata.isSymbolicLink()) {
    throw new Error(`${label} path is not a physical file: ${filePath}`);
  }
  try {
    return await fsp.readFile(filePath, 'utf-8');
  } catch (error) {
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

export async function readDocument(): Promise<CursorDocument> {
  const filePath = cursorConfigPath();
  const raw = await readOptionalPhysical(filePath, 'Cursor user hooks');
  return raw === undefined ? createDocument(filePath) : parseDocument(filePath, raw);
}

async function ensurePhysicalDirectory(directory: string): Promise<void> {
  try {
    await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  } catch (error) {
    throw new Error(`Create Cursor hooks directory at ${directory}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  let metadata;
  try {
    metadata = await fsp.lstat(directory);
  } catch (error) {
    throw new Error(`Inspect Cursor hooks directory at ${directory}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!metadata.isDirectory() || metadata.isSymbolicLink()) {
    throw new Error(`Cursor hooks directory is not a physical directory: ${directory}`);
  }
}

async function unlinkOptional(filePath: string): Promise<void> {
  try {
    await fsp.unlink(filePath);
  } catch (error) {
    if (!hasErrorCode(error, 'ENOENT')) throw error;
  }
}

async function writeExclusive(filePath: string, contents: string): Promise<void> {
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(filePath, 'wx', 0o600);
    await handle.writeFile(contents, 'utf-8');
    await handle.sync();
    await handle.close();
    handle = undefined;
  } catch (error) {
    const failures = [asError(error)];
    if (handle) {
      try {
        await handle.close();
      } catch (closeError) {
        failures.push(asError(closeError));
      }
    }
    try {
      await unlinkOptional(filePath);
    } catch (cleanupError) {
      failures.push(asError(cleanupError));
    }
    if (failures.length > 1) {
      throw new AggregateError(failures, 'Stage Cursor user hooks');
    }
    throw new Error(`Stage Cursor user hooks: ${errorMessage(error)}`, { cause: failures[0] });
  }
}

async function assertUnchanged(document: CursorDocument): Promise<void> {
  const current = await readOptionalPhysical(document.filePath, 'Cursor user hooks');
  if (current !== document.raw) {
    throw new Error(`Cursor user hooks changed during update: ${document.filePath}`);
  }
}

export async function writeDocument(rendered: RenderedDocument): Promise<void> {
  if (!rendered.changed) return;
  await assertUnchanged(rendered.document);
  if (rendered.next === undefined) {
    try {
      await fsp.unlink(rendered.document.filePath);
    } catch (error) {
      if (!hasErrorCode(error, 'ENOENT')) {
        throw new Error(
          `Remove Cursor user hooks at ${rendered.document.filePath}: ${errorMessage(error)}`,
          { cause: asError(error) },
        );
      }
    }
    return;
  }

  const directory = path.dirname(rendered.document.filePath);
  await ensurePhysicalDirectory(directory);
  const temporaryPath = path.join(
    directory,
    `.${path.basename(rendered.document.filePath)}.${randomUUID()}.tmp`,
  );
  let committed = false;
  let failure: Error | undefined;
  try {
    await writeExclusive(temporaryPath, rendered.next);
    await assertUnchanged(rendered.document);
    await fsp.rename(temporaryPath, rendered.document.filePath);
    committed = true;
  } catch (error) {
    failure = new Error(
      `Write Cursor user hooks at ${rendered.document.filePath}: ${errorMessage(error)}`,
      { cause: asError(error) },
    );
  }
  let cleanupFailure: Error | undefined;
  if (!committed) {
    try {
      await unlinkOptional(temporaryPath);
    } catch (error) {
      cleanupFailure = new Error(
        `Clean Cursor hook staging file at ${temporaryPath}: ${errorMessage(error)}`,
        { cause: asError(error) },
      );
    }
  }
  if (failure && cleanupFailure) {
    throw new AggregateError([failure, cleanupFailure], 'Write Cursor user hooks');
  }
  if (failure) throw failure;
  if (cleanupFailure) throw cleanupFailure;
}

export async function physicalFileExists(filePath: string, label: string): Promise<boolean> {
  let metadata;
  try {
    metadata = await fsp.lstat(filePath);
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT') || hasErrorCode(error, 'ENOTDIR')) return false;
    throw new Error(`Inspect ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!metadata.isFile() || metadata.isSymbolicLink()) {
    throw new Error(`${label} path is not a physical file: ${filePath}`);
  }
  return true;
}

export async function requireRuntime(filePath: string, label: string): Promise<void> {
  if (!filePath) throw new Error(`${label} path is required`);
  if (!await physicalFileExists(filePath, label)) {
    throw new Error(`${label} is missing: ${filePath}`);
  }
}

async function readRuntimeConfig(filePath: string): Promise<JsonObject | undefined> {
  const raw = await readOptionalPhysical(filePath, 'Elydora runtime config');
  if (raw === undefined) return undefined;
  return parseStrictJsonObject(raw, `Elydora runtime config at ${filePath}`);
}

function sameAgentId(left: unknown, right: string): boolean {
  if (typeof left !== 'string') return false;
  return process.platform === 'win32'
    ? left.toLowerCase() === right.toLowerCase()
    : left === right;
}

export async function runtimeFilesExist(contracts: RuntimeContract[]): Promise<boolean> {
  for (const contract of contracts) {
    const runtimeConfig = await readRuntimeConfig(
      path.join(path.dirname(contract.guardPath), 'config.json'),
    );
    if (!runtimeConfig
      || runtimeConfig.agent_name !== AGENT_KEY
      || !sameAgentId(runtimeConfig.agent_id, contract.agentId)) continue;
    const files = await Promise.all([
      physicalFileExists(contract.guardPath, 'Elydora guard runtime'),
      physicalFileExists(contract.auditPath, 'Elydora audit runtime'),
      physicalFileExists(
        path.join(path.dirname(contract.guardPath), 'private.key'),
        'Elydora private key',
      ),
    ]);
    if (files.every(Boolean)) return true;
  }
  return false;
}
