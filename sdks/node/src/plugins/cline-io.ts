import path from 'node:path';
import {
  AGENT_KEY,
  type ClineHookFile,
  type ClineRuntimeContract,
  parseMetadata,
} from './cline-contract.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';

export async function readHookFile(filePath: string): Promise<ClineHookFile> {
  const directory = path.dirname(filePath);
  if (!await inspectPhysicalDirectory(directory, 'Cline hooks directory')) {
    return { exists: false, filePath };
  }
  const snapshot = await readPhysicalFile(filePath, 'Cline hook');
  if (!snapshot) return { exists: false, filePath };
  return {
    exists: true,
    filePath,
    source: snapshot.contents,
    metadata: parseMetadata(filePath, snapshot.contents),
  };
}

export function requireAvailableHookFile(file: ClineHookFile): void {
  if (file.exists && !file.metadata) {
    throw new Error(`Cline hook at ${file.filePath} already exists and is owned by another integration`);
  }
}

export async function runtimeFilesExist(contract: ClineRuntimeContract): Promise<boolean> {
  return managedRuntimeFilesExist(contract, AGENT_KEY);
}
