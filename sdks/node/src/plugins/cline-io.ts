import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import path from 'node:path';
import {
  AGENT_KEY,
  type ClineHookFile,
  type ClineRuntimeContract,
  parseMetadata,
  sameAgentId,
} from './cline-contract.js';

type JsonObject = Record<string, unknown>;

interface StagedFile {
  readonly state: ClineHookFile;
  readonly tempPath: string;
}

function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function hasErrorCode(error: unknown, code: string): boolean {
  return isObject(error) && error.code === code;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

export async function readHookFile(filePath: string): Promise<ClineHookFile> {
  let source: string;
  try {
    source = await fsp.readFile(filePath, 'utf-8');
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return { exists: false, filePath };
    throw new Error(`Read Cline hook at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  return {
    exists: true,
    filePath,
    source,
    metadata: parseMetadata(filePath, source),
  };
}

export function requireAvailableHookFile(file: ClineHookFile): void {
  if (file.exists && !file.metadata) {
    throw new Error(`Cline hook at ${file.filePath} already exists and is owned by another integration`);
  }
}

async function removeTemporary(tempPath: string): Promise<void> {
  try {
    await fsp.unlink(tempPath);
  } catch (error) {
    if (!hasErrorCode(error, 'ENOENT')) throw error;
  }
}

async function failStage(
  handle: FileHandle | undefined,
  tempPath: string,
  cause: unknown,
): Promise<never> {
  const errors = [asError(cause)];
  if (handle) {
    try {
      await handle.close();
    } catch (error) {
      errors.push(asError(error));
    }
  }
  try {
    await removeTemporary(tempPath);
  } catch (error) {
    errors.push(asError(error));
  }
  const message = `Stage Cline hook at ${tempPath}: ${errorMessage(cause)}`;
  if (errors.length > 1) throw new AggregateError(errors, message);
  throw new Error(message, { cause: errors[0] });
}

async function stageFile(state: ClineHookFile, source: string): Promise<StagedFile> {
  const directory = path.dirname(state.filePath);
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  const tempPath = path.join(directory, `.${path.basename(state.filePath)}.${randomUUID()}.tmp`);
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(tempPath, 'wx', 0o700);
    await handle.writeFile(source, 'utf-8');
    await handle.sync();
    await handle.close();
    handle = undefined;
    return { state, tempPath };
  } catch (error) {
    return failStage(handle, tempPath, error);
  }
}

async function writeAtomic(filePath: string, source: string): Promise<void> {
  const staged = await stageFile({ exists: false, filePath }, source);
  try {
    await fsp.rename(staged.tempPath, filePath);
  } catch (error) {
    const errors = [asError(error)];
    try {
      await removeTemporary(staged.tempPath);
    } catch (cleanupError) {
      errors.push(asError(cleanupError));
    }
    throw new AggregateError(errors, `Restore Cline hook at ${filePath}: ${errorMessage(error)}`);
  }
}

async function rollbackFile(state: ClineHookFile): Promise<void> {
  if (state.exists) {
    await writeAtomic(state.filePath, state.source!);
    return;
  }
  try {
    await fsp.unlink(state.filePath);
  } catch (error) {
    if (!hasErrorCode(error, 'ENOENT')) throw error;
  }
}

export async function writeHookPair(
  guard: { readonly state: ClineHookFile; readonly source: string },
  audit: { readonly state: ClineHookFile; readonly source: string },
): Promise<void> {
  const staged: StagedFile[] = [];
  try {
    staged.push(await stageFile(guard.state, guard.source));
    staged.push(await stageFile(audit.state, audit.source));
  } catch (error) {
    const errors = [asError(error)];
    for (const item of staged) {
      try {
        await removeTemporary(item.tempPath);
      } catch (cleanupError) {
        errors.push(asError(cleanupError));
      }
    }
    throw new AggregateError(errors, `Stage Cline hook pair: ${errorMessage(error)}`);
  }

  const committed: StagedFile[] = [];
  try {
    for (const item of staged) {
      await fsp.rename(item.tempPath, item.state.filePath);
      committed.push(item);
    }
  } catch (error) {
    const errors = [asError(error)];
    for (const item of [...committed].reverse()) {
      try {
        await rollbackFile(item.state);
      } catch (rollbackError) {
        errors.push(asError(rollbackError));
      }
    }
    for (const item of staged.slice(committed.length)) {
      try {
        await removeTemporary(item.tempPath);
      } catch (cleanupError) {
        errors.push(asError(cleanupError));
      }
    }
    throw new AggregateError(errors, `Write Cline hook pair: ${errorMessage(error)}`);
  }
}

export async function removeOwnedHooks(
  files: readonly ClineHookFile[],
  agentId?: string,
): Promise<void> {
  for (const file of files) {
    const ownedAgentId = file.metadata?.agentId;
    if (!ownedAgentId || (agentId && !sameAgentId(ownedAgentId, agentId))) continue;
    try {
      await fsp.unlink(file.filePath);
    } catch (error) {
      if (hasErrorCode(error, 'ENOENT')) continue;
      throw new Error(`Remove Cline hook at ${file.filePath}: ${errorMessage(error)}`, {
        cause: asError(error),
      });
    }
  }
}

export async function regularFileExists(filePath: string, label: string): Promise<boolean> {
  try {
    return (await fsp.stat(filePath)).isFile();
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT') || hasErrorCode(error, 'ENOTDIR')) return false;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

export async function requireRuntime(filePath: string, label: string): Promise<void> {
  if (!filePath) throw new Error(`${label} path is required`);
  if (!await regularFileExists(filePath, label)) throw new Error(`${label} is missing: ${filePath}`);
}

async function readRuntimeConfig(filePath: string): Promise<JsonObject | undefined> {
  let raw: string;
  try {
    raw = await fsp.readFile(filePath, 'utf-8');
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return undefined;
    throw new Error(`Read Elydora runtime config at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Failed to parse Elydora runtime config at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!isObject(value)) throw new Error(`Elydora runtime config at ${filePath} must be a JSON object`);
  return value;
}

export async function runtimeFilesExist(contract: ClineRuntimeContract): Promise<boolean> {
  const configPath = path.join(contract.agentDirectory, 'config.json');
  const config = await readRuntimeConfig(configPath);
  if (!config
    || config.agent_name !== AGENT_KEY
    || typeof config.agent_id !== 'string'
    || !sameAgentId(config.agent_id, contract.agentId)) return false;
  const existence = await Promise.all([
    regularFileExists(contract.guardPath, 'Elydora guard runtime'),
    regularFileExists(contract.auditPath, 'Elydora audit runtime'),
  ]);
  return existence.every(Boolean);
}
