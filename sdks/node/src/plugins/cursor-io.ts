import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  CONFIG_FILE,
  type CursorDocument,
  type RenderedDocument,
  type RuntimeContract,
  createDocument,
  parseDocument,
} from './cursor-contract.js';
import { readPhysicalFile } from './managed-files.js';
import { managedRuntimePresent } from './managed-runtime-status.js';
import {
  commitManagedTransaction,
  prepareManagedFileChange,
} from './managed-transaction.js';

export function cursorConfigPath(): string {
  return path.join(os.homedir(), '.cursor', CONFIG_FILE);
}

export async function readDocument(): Promise<CursorDocument> {
  const filePath = cursorConfigPath();
  const snapshot = await readPhysicalFile(filePath, 'Cursor user hooks');
  return snapshot ? parseDocument(filePath, snapshot.contents) : createDocument(filePath);
}

export async function writeDocument(rendered: RenderedDocument): Promise<void> {
  if (!rendered.changed) return;
  const change = await prepareManagedFileChange({
    filePath: rendered.document.filePath,
    label: 'Cursor user hooks',
    next: rendered.next,
    mode: 0o600,
    expectedSource: rendered.document.raw,
    verifyExpectedSource: true,
  });
  if (!change) return;
  await commitManagedTransaction({
    displayName: 'Cursor',
    operation: 'uninstall',
    directories: [{
      path: path.dirname(rendered.document.filePath),
      label: 'Cursor hooks directory',
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
