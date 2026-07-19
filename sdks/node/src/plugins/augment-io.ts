import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  type AugmentDocument,
  type JsonObject,
  type RuntimeContract,
  isObject,
  readHooks,
} from './augment-contract.js';

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function hasErrorCode(error: unknown, code: string): boolean {
  return isObject(error) && error.code === code;
}

export function resolveConfigPath(): string {
  return path.join(os.homedir(), '.augment', 'settings.json');
}

async function readJsonObject(filePath: string, label: string): Promise<JsonObject | undefined> {
  let raw: string;
  try {
    raw = await fsp.readFile(filePath, 'utf-8');
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return undefined;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Failed to parse ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!isObject(value)) throw new Error(`${label} at ${filePath} must contain a JSON object`);
  return value;
}

export async function readConfig(): Promise<AugmentDocument> {
  const configPath = resolveConfigPath();
  const root = await readJsonObject(configPath, 'Auggie settings');
  if (!root) return { exists: false, configPath, root: {}, hooks: {} };
  return { exists: true, configPath, root, hooks: readHooks(root) };
}

async function failWrite(
  handle: FileHandle | undefined,
  tempPath: string,
  label: string,
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
    await fsp.unlink(tempPath);
  } catch (error) {
    if (!hasErrorCode(error, 'ENOENT')) errors.push(asError(error));
  }
  const message = `Write ${label}: ${errorMessage(cause)}`;
  if (errors.length > 1) throw new AggregateError(errors, message);
  throw new Error(message, { cause: errors[0] });
}

async function writeAtomic(
  filePath: string,
  contents: string,
  mode: number,
  label: string,
): Promise<void> {
  const directory = path.dirname(filePath);
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  const tempPath = path.join(directory, `.${path.basename(filePath)}.${randomUUID()}.tmp`);
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(tempPath, 'wx', mode);
    await handle.writeFile(contents, 'utf-8');
    await handle.sync();
    await handle.close();
    handle = undefined;
    await fsp.rename(tempPath, filePath);
  } catch (error) {
    await failWrite(handle, tempPath, `${label} at ${filePath}`, error);
  }
}

export async function writeConfig(configPath: string, root: JsonObject): Promise<void> {
  await writeAtomic(
    configPath,
    JSON.stringify(root, null, 2) + '\n',
    0o600,
    'Auggie settings',
  );
}

export async function writeWrapper(wrapperPath: string, contents: string): Promise<void> {
  await writeAtomic(wrapperPath, contents, 0o700, 'Auggie hook wrapper');
}

export async function removeConfig(configPath: string): Promise<void> {
  try {
    await fsp.unlink(configPath);
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return;
    throw new Error(`Remove Auggie settings at ${configPath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
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

function sameAgentId(left: string, right: string): boolean {
  return process.platform === 'win32' ? left.toLowerCase() === right.toLowerCase() : left === right;
}

export async function runtimeFilesExist(contracts: RuntimeContract[]): Promise<boolean> {
  const root = path.join(os.homedir(), '.elydora');
  let entries: Array<{ isDirectory(): boolean; name: string }>;
  try {
    entries = await fsp.readdir(root, { withFileTypes: true });
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return false;
    throw new Error(`Read Elydora runtime directory at ${root}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  for (const contract of contracts) {
    const directory = entries.find(
      (item) => item.isDirectory() && sameAgentId(item.name, contract.agentId),
    );
    if (!directory) continue;
    const agentDirectory = path.join(root, directory.name);
    const runtimeConfigPath = path.join(agentDirectory, 'config.json');
    const runtimeConfig = await readJsonObject(runtimeConfigPath, 'Elydora runtime config');
    if (!runtimeConfig || runtimeConfig.agent_name !== AGENT_KEY) continue;
    const files = [
      [contract.guardPath, 'Elydora guard runtime'],
      [contract.auditPath, 'Elydora audit runtime'],
      [contract.guardWrapperPath, 'Auggie guard wrapper'],
      [contract.auditWrapperPath, 'Auggie audit wrapper'],
    ] as const;
    const existence = await Promise.all(
      files.map(([filePath, label]) => regularFileExists(filePath, label)),
    );
    if (existence.every(Boolean)) return true;
  }
  return false;
}
