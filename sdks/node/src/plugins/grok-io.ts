import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  AUDIT_OPTIONS,
  GUARD_OPTIONS,
  createGrokDocument,
  parseGrokDocument,
  type GrokDocument,
  type GrokRuntimeContract,
} from './grok-contract.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';

const CONFIG_FILE = 'elydora-audit.json';

export function grokConfigPath(): string {
  const configured = process.env.GROK_HOME;
  if (configured === '') {
    throw new Error(
      'GROK_HOME is empty; unset GROK_HOME or set it to an absolute home directory',
    );
  }
  const grokHome = configured === undefined
    ? path.join(os.homedir(), '.grok')
    : path.resolve(configured);
  return path.join(grokHome, 'hooks', CONFIG_FILE);
}

export async function readGrokDocument(): Promise<GrokDocument> {
  const filePath = grokConfigPath();
  await inspectPhysicalDirectory(path.dirname(path.dirname(filePath)), 'Grok home directory');
  await inspectPhysicalDirectory(path.dirname(filePath), 'Grok hooks directory');
  const snapshot = await readPhysicalFile(filePath, 'Grok user hooks');
  return snapshot
    ? parseGrokDocument(filePath, snapshot.contents)
    : createGrokDocument(filePath);
}

export async function grokRuntimeFilesExist(
  contracts: readonly GrokRuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    const exists = await managedRuntimeFilesExist(contract, AGENT_KEY, {
      guardOptions: GUARD_OPTIONS,
      auditOptions: AUDIT_OPTIONS,
    });
    if (exists) return true;
  }
  return false;
}
