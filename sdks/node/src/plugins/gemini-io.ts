import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  AUDIT_OPTIONS,
  CONFIG_FILE,
  GUARD_OPTIONS,
  type GeminiRuntimeContract,
} from './gemini-contract.js';
import {
  createGeminiDocument,
  parseGeminiDocument,
  type GeminiDocument,
  type RenderedGeminiDocument,
} from './gemini-config.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';
import {
  commitManagedTransaction,
  prepareManagedFileChange,
} from './managed-transaction.js';

export function geminiConfigurationDirectory(): string {
  const configuredHome = process.env.GEMINI_CLI_HOME;
  const home = configuredHome ? configuredHome : os.homedir();
  return path.join(home, '.gemini');
}

export function geminiSettingsPath(): string {
  return path.join(geminiConfigurationDirectory(), CONFIG_FILE);
}

export async function readGeminiDocument(): Promise<GeminiDocument> {
  const filePath = geminiSettingsPath();
  await inspectPhysicalDirectory(path.dirname(filePath), 'Gemini CLI configuration directory');
  const snapshot = await readPhysicalFile(filePath, 'Gemini CLI user settings');
  return snapshot
    ? parseGeminiDocument({
      exists: true,
      filePath,
      raw: snapshot.contents,
    })
    : createGeminiDocument(filePath);
}

export async function writeGeminiDocument(rendered: RenderedGeminiDocument): Promise<void> {
  if (!rendered.changed) return;
  const change = await prepareManagedFileChange({
    filePath: rendered.document.filePath,
    label: 'Gemini CLI user settings',
    next: rendered.next,
    mode: 0o600,
    expectedSource: rendered.document.exists ? rendered.document.raw : undefined,
    verifyExpectedSource: true,
  });
  if (!change) return;
  await commitManagedTransaction({
    displayName: 'Gemini CLI',
    operation: 'uninstall',
    directories: [{
      path: path.dirname(rendered.document.filePath),
      label: 'Gemini CLI configuration directory',
    }],
    changes: [change],
  });
}

export async function geminiRuntimeFilesExist(
  contracts: readonly GeminiRuntimeContract[],
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
