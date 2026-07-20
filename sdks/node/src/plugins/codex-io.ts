import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  CONFIG_FILE,
  type CodexDocument,
  type RenderedDocument,
  type RuntimeContract,
  createDocument,
  parseDocument,
} from './codex-contract.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { parseStrictJsonObject } from './strict-json.js';

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function hasCode(error: unknown, code: string): boolean {
  return typeof error === 'object' && error !== null && 'code' in error && error.code === code;
}

async function codexHomePath(): Promise<string> {
  const configured = process.env.CODEX_HOME;
  if (configured === undefined || configured === '') return path.join(os.homedir(), '.codex');
  let metadata;
  try {
    metadata = await fsp.stat(configured);
  } catch (error) {
    throw new Error(`Resolve CODEX_HOME at ${configured}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!metadata.isDirectory()) throw new Error(`CODEX_HOME is not a directory: ${configured}`);
  let canonical;
  try {
    canonical = await fsp.realpath(configured);
  } catch (error) {
    throw new Error(`Canonicalize CODEX_HOME at ${configured}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!await inspectPhysicalDirectory(canonical, 'CODEX_HOME')) {
    throw new Error(`CODEX_HOME is missing: ${canonical}`);
  }
  return canonical;
}

export async function codexConfigPath(): Promise<string> {
  return path.join(await codexHomePath(), CONFIG_FILE);
}

export async function readDocument(): Promise<CodexDocument> {
  const filePath = await codexConfigPath();
  const snapshot = await readPhysicalFile(filePath, 'Codex user hooks');
  return snapshot ? parseDocument(filePath, snapshot.contents) : createDocument(filePath);
}

async function unlinkOptional(filePath: string): Promise<void> {
  try {
    await fsp.unlink(filePath);
  } catch (error) {
    if (!hasCode(error, 'ENOENT')) throw error;
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
    if (process.platform !== 'win32') await fsp.chmod(filePath, 0o600);
  } catch (error) {
    const failures = [asError(error)];
    if (handle) {
      try { await handle.close(); } catch (closeError) { failures.push(asError(closeError)); }
    }
    try { await unlinkOptional(filePath); } catch (cleanupError) {
      failures.push(asError(cleanupError));
    }
    if (failures.length > 1) throw new AggregateError(failures, 'Stage Codex user hooks');
    throw new Error(`Stage Codex user hooks: ${errorMessage(error)}`, { cause: failures[0] });
  }
}

async function assertUnchanged(document: CodexDocument): Promise<void> {
  const current = await readPhysicalFile(document.filePath, 'Codex user hooks');
  if (current?.contents !== document.raw) {
    throw new Error(`Codex user hooks changed during update: ${document.filePath}`);
  }
}

async function ensureHooksDirectory(directory: string): Promise<void> {
  try {
    await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  } catch (error) {
    throw new Error(`Create Codex hooks directory at ${directory}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!await inspectPhysicalDirectory(directory, 'Codex hooks directory')) {
    throw new Error(`Codex hooks directory is missing: ${directory}`);
  }
}

export async function writeDocument(rendered: RenderedDocument): Promise<void> {
  if (!rendered.changed) return;
  await assertUnchanged(rendered.document);
  const directory = path.dirname(rendered.document.filePath);
  await ensureHooksDirectory(directory);
  if (rendered.next === undefined) {
    try {
      await fsp.unlink(rendered.document.filePath);
    } catch (error) {
      if (!hasCode(error, 'ENOENT')) {
        throw new Error(
          `Remove Codex user hooks at ${rendered.document.filePath}: ${errorMessage(error)}`,
          { cause: asError(error) },
        );
      }
    }
    return;
  }

  const temporaryPath = path.join(
    directory,
    `.${path.basename(rendered.document.filePath)}.${randomUUID()}.tmp`,
  );
  let failure: Error | undefined;
  try {
    await writeExclusive(temporaryPath, rendered.next);
    await assertUnchanged(rendered.document);
    await fsp.rename(temporaryPath, rendered.document.filePath);
  } catch (error) {
    failure = new Error(
      `Write Codex user hooks at ${rendered.document.filePath}: ${errorMessage(error)}`,
      { cause: asError(error) },
    );
  }
  let cleanupFailure: Error | undefined;
  try {
    await unlinkOptional(temporaryPath);
  } catch (error) {
    cleanupFailure = new Error(
      `Clean Codex hook staging file at ${temporaryPath}: ${errorMessage(error)}`,
      { cause: asError(error) },
    );
  }
  if (failure && cleanupFailure) {
    throw new AggregateError([failure, cleanupFailure], 'Write Codex user hooks');
  }
  if (failure) throw failure;
  if (cleanupFailure) throw cleanupFailure;
}

async function physicalFileExists(filePath: string, label: string): Promise<boolean> {
  return Boolean(await readPhysicalFile(filePath, label));
}

function sameAgentId(left: unknown, right: string): boolean {
  return typeof left === 'string' && (process.platform === 'win32'
    ? left.toLowerCase() === right.toLowerCase()
    : left === right);
}

export async function runtimeFilesExist(contracts: RuntimeContract[]): Promise<boolean> {
  for (const contract of contracts) {
    const directory = path.dirname(contract.guardPath);
    const configPath = path.join(directory, 'config.json');
    const snapshot = await readPhysicalFile(configPath, 'Elydora runtime config');
    if (!snapshot) continue;
    const config = parseStrictJsonObject(
      snapshot.contents,
      `Elydora runtime config at ${configPath}`,
    );
    if (config.agent_name !== AGENT_KEY || !sameAgentId(config.agent_id, contract.agentId)) {
      continue;
    }
    const files = await Promise.all([
      physicalFileExists(contract.guardPath, 'Elydora guard runtime'),
      physicalFileExists(contract.auditPath, 'Elydora audit runtime'),
      physicalFileExists(path.join(directory, 'private.key'), 'Elydora private key'),
    ]);
    if (files.every(Boolean)) return true;
  }
  return false;
}
