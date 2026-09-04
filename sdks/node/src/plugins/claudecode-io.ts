import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  CONFIG_FILE,
  createClaudeDocument,
  parseClaudeDocument,
  type ClaudeDocument,
  type ClaudeRuntimeContract,
  type RenderedClaudeDocument,
} from './claudecode-contract.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';
import {
  commitManagedTransaction,
  prepareManagedFileChange,
} from './managed-transaction.js';

export function claudeConfigDirectory(): string {
  const configured = process.env.CLAUDE_CONFIG_DIR;
  return configured === undefined
    ? path.join(os.homedir(), '.claude')
    : path.resolve(configured);
}

export function claudeSettingsPath(): string {
  return path.join(claudeConfigDirectory(), CONFIG_FILE);
}

export async function readClaudeDocument(): Promise<ClaudeDocument> {
  const filePath = claudeSettingsPath();
  await inspectPhysicalDirectory(path.dirname(filePath), 'Claude Code configuration directory');
  const snapshot = await readPhysicalFile(filePath, 'Claude Code user settings');
  return snapshot
    ? parseClaudeDocument(filePath, snapshot.contents)
    : createClaudeDocument(filePath);
}

export async function writeClaudeDocument(rendered: RenderedClaudeDocument): Promise<void> {
  if (!rendered.changed) return;
  const change = await prepareManagedFileChange({
    filePath: rendered.document.filePath,
    label: 'Claude Code user settings',
    next: rendered.next,
    mode: 0o600,
    expectedSource: rendered.document.raw,
    verifyExpectedSource: true,
  });
  if (!change) return;
  await commitManagedTransaction({
    displayName: 'Claude Code',
    operation: 'uninstall',
    directories: [{
      path: path.dirname(rendered.document.filePath),
      label: 'Claude Code configuration directory',
    }],
    changes: [change],
  });
}

export async function claudeRuntimeFilesExist(
  contracts: readonly ClaudeRuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    if (await managedRuntimeFilesExist(contract, AGENT_KEY)) return true;
  }
  return false;
}
