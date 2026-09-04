import fsp from 'node:fs/promises';
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
import { asError, errorMessage } from './common.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { managedRuntimePresent } from './managed-runtime-status.js';
import {
  commitManagedTransaction,
  prepareManagedFileChange,
} from './managed-transaction.js';

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

export async function writeDocument(rendered: RenderedDocument): Promise<void> {
  if (!rendered.changed) return;
  const change = await prepareManagedFileChange({
    filePath: rendered.document.filePath,
    label: 'Codex user hooks',
    next: rendered.next,
    mode: 0o600,
    expectedSource: rendered.document.raw,
    verifyExpectedSource: true,
  });
  if (!change) return;
  await commitManagedTransaction({
    displayName: 'Codex',
    operation: 'uninstall',
    directories: [{
      path: path.dirname(rendered.document.filePath),
      label: 'Codex hooks directory',
    }],
    changes: [change],
  });
}

export async function runtimeFilesExist(contracts: RuntimeContract[]): Promise<boolean> {
  for (const contract of contracts) {
    if (await managedRuntimePresent(contract, AGENT_KEY)) return true;
  }
  return false;
}
