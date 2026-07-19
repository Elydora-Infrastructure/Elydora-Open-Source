import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  type JsonObject,
  type RuntimeContract,
  isObject,
} from './droid-contract.js';
import {
  type DroidSources,
  type RenderedDocument,
  createSettingsDocument,
  parseDocument,
} from './droid-config.js';

interface FileChange {
  readonly filePath: string;
  readonly label: string;
  readonly original?: string;
  readonly next?: string;
}

interface StagedChange {
  readonly change: FileChange;
  readonly tempPath?: string;
  readonly rollbackPath?: string;
  committed: boolean;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function hasErrorCode(error: unknown, code: string): boolean {
  return isObject(error) && error.code === code;
}

function factoryPaths(): {
  readonly root: string;
  readonly legacy: string;
  readonly settings: string;
} {
  const directory = path.join(os.homedir(), '.factory');
  return {
    root: path.join(directory, 'hooks.json'),
    legacy: path.join(directory, 'hooks', 'hooks.json'),
    settings: path.join(directory, 'settings.json'),
  };
}

async function readOptional(filePath: string, label: string): Promise<string | undefined> {
  try {
    return await fsp.readFile(filePath, 'utf-8');
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return undefined;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

export async function readSources(): Promise<DroidSources> {
  const paths = factoryPaths();
  const [rootRaw, settingsRaw] = await Promise.all([
    readOptional(paths.root, 'Factory Droid hooks'),
    readOptional(paths.settings, 'Factory Droid settings'),
  ]);
  let primary;
  if (rootRaw !== undefined) {
    primary = parseDocument({
      exists: true,
      filePath: paths.root,
      kind: 'hooks',
      raw: rootRaw,
    });
  } else {
    const legacyRaw = await readOptional(paths.legacy, 'Factory Droid legacy hooks');
    if (legacyRaw !== undefined) {
      primary = parseDocument({
        exists: true,
        filePath: paths.legacy,
        kind: 'legacy',
        raw: legacyRaw,
      });
    }
  }
  const settings = settingsRaw === undefined
    ? createSettingsDocument(paths.settings)
    : parseDocument({
      exists: true,
      filePath: paths.settings,
      kind: 'settings',
      raw: settingsRaw,
    });
  return { rootPath: paths.root, primary, settings };
}

function labelFor(rendered: RenderedDocument): string {
  if (rendered.document.kind === 'settings') return 'Factory Droid settings';
  if (rendered.document.kind === 'legacy') return 'Factory Droid legacy hooks';
  return 'Factory Droid hooks';
}

function fileChange(rendered: RenderedDocument): FileChange | undefined {
  if (!rendered.changed) return undefined;
  return {
    filePath: rendered.document.filePath,
    label: labelFor(rendered),
    original: rendered.document.exists ? rendered.document.raw : undefined,
    next: rendered.next,
  };
}

async function unlinkOptional(filePath: string): Promise<void> {
  try {
    await fsp.unlink(filePath);
  } catch (error) {
    if (!hasErrorCode(error, 'ENOENT')) throw error;
  }
}

async function writeExclusive(filePath: string, contents: string, label: string): Promise<void> {
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(filePath, 'wx', 0o600);
    await handle.writeFile(contents, 'utf-8');
    await handle.sync();
    await handle.close();
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
      throw new AggregateError(failures, `Stage ${label} at ${filePath}`);
    }
    throw new Error(`Stage ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: failures[0],
    });
  }
}

async function assertUnchanged(change: FileChange): Promise<void> {
  const current = await readOptional(change.filePath, change.label);
  if (current !== change.original) {
    throw new Error(`${change.label} changed during installation: ${change.filePath}`);
  }
}

async function stage(change: FileChange): Promise<StagedChange> {
  await assertUnchanged(change);
  const directory = path.dirname(change.filePath);
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  const token = randomUUID();
  const tempPath = change.next === undefined
    ? undefined
    : path.join(directory, `.${path.basename(change.filePath)}.${token}.tmp`);
  const rollbackPath = change.original === undefined
    ? undefined
    : path.join(directory, `.${path.basename(change.filePath)}.${token}.rollback`);
  try {
    if (tempPath) await writeExclusive(tempPath, change.next!, change.label);
    if (rollbackPath && change.next !== undefined) {
      await writeExclusive(rollbackPath, change.original!, `${change.label} rollback`);
    }
  } catch (error) {
    const failures = [asError(error)];
    for (const stagedPath of [tempPath, rollbackPath]) {
      if (!stagedPath) continue;
      try {
        await unlinkOptional(stagedPath);
      } catch (cleanupError) {
        failures.push(asError(cleanupError));
      }
    }
    if (failures.length > 1) throw new AggregateError(failures, `Stage ${change.label}`);
    throw error;
  }
  return { change, tempPath, rollbackPath, committed: false };
}

async function commit(staged: StagedChange): Promise<void> {
  await assertUnchanged(staged.change);
  if (staged.change.next === undefined) {
    if (!staged.rollbackPath) throw new Error(`Missing rollback path for ${staged.change.label}`);
    await fsp.rename(staged.change.filePath, staged.rollbackPath);
  } else {
    if (!staged.tempPath) throw new Error(`Missing staged file for ${staged.change.label}`);
    await fsp.rename(staged.tempPath, staged.change.filePath);
  }
  staged.committed = true;
}

async function rollback(staged: StagedChange): Promise<void> {
  if (!staged.committed) return;
  if (staged.change.original === undefined) {
    await unlinkOptional(staged.change.filePath);
    return;
  }
  if (!staged.rollbackPath) throw new Error(`Missing rollback data for ${staged.change.label}`);
  await fsp.rename(staged.rollbackPath, staged.change.filePath);
}

async function cleanup(staged: StagedChange): Promise<void> {
  const paths = [staged.tempPath, staged.rollbackPath].filter(
    (filePath): filePath is string => filePath !== undefined,
  );
  await Promise.all(paths.map(unlinkOptional));
}

export async function writeDocuments(rendered: RenderedDocument[]): Promise<void> {
  const changes = rendered.flatMap((item) => {
    const next = fileChange(item);
    return next ? [next] : [];
  });
  if (changes.length === 0) return;
  const staged: StagedChange[] = [];
  try {
    for (const change of changes) staged.push(await stage(change));
    for (const item of staged) await commit(item);
  } catch (error) {
    const failures = [asError(error)];
    for (const item of [...staged].reverse()) {
      try {
        await rollback(item);
      } catch (rollbackError) {
        failures.push(asError(rollbackError));
      }
    }
    for (const item of staged) {
      try {
        await cleanup(item);
      } catch (cleanupError) {
        failures.push(asError(cleanupError));
      }
    }
    if (failures.length > 1) throw new AggregateError(failures, 'Write Factory Droid hook sources');
    throw new Error(`Write Factory Droid hook sources: ${errorMessage(error)}`, {
      cause: failures[0],
    });
  }
  await Promise.all(staged.map(cleanup));
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
  const raw = await readOptional(filePath, 'Elydora runtime config');
  if (raw === undefined) return undefined;
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Failed to parse Elydora runtime config at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!isObject(value)) throw new Error(`Elydora runtime config at ${filePath} must contain a JSON object`);
  return value;
}

export async function runtimeFilesExist(contracts: RuntimeContract[]): Promise<boolean> {
  for (const contract of contracts) {
    const agentDirectory = path.dirname(contract.guardPath);
    const runtimeConfig = await readRuntimeConfig(path.join(agentDirectory, 'config.json'));
    if (!runtimeConfig || runtimeConfig.agent_name !== AGENT_KEY) continue;
    const files = await Promise.all([
      regularFileExists(contract.guardPath, 'Elydora guard runtime'),
      regularFileExists(contract.auditPath, 'Elydora audit runtime'),
    ]);
    if (files.every(Boolean)) return true;
  }
  return false;
}

export function displayConfigPath(sources: DroidSources): string {
  if (sources.primary) return sources.primary.filePath;
  if (sources.settings.exists && sources.settings.hasHooksContainer) return sources.settings.filePath;
  return sources.rootPath;
}
