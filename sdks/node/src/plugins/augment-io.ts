import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  AUDIT_WRAPPER,
  GUARD_WRAPPER,
  buildWrapper,
  createAugmentDocument,
  parseAugmentDocument,
  type AugmentDocument,
  type RenderedAugmentDocument,
  type RuntimeContract,
} from './augment-contract.js';
import { MAX_CONFIG_BYTES, samePath } from './common.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';
import {
  commitManagedTransaction,
  prepareManagedFileChange,
} from './managed-transaction.js';

export function resolveConfigPath(): string {
  return path.join(os.homedir(), '.augment', 'settings.json');
}

export async function readConfig(): Promise<AugmentDocument> {
  const configPath = resolveConfigPath();
  await inspectPhysicalDirectory(path.dirname(configPath), 'Auggie configuration directory');
  const snapshot = await readPhysicalFile(configPath, 'Auggie user settings', MAX_CONFIG_BYTES);
  return snapshot
    ? parseAugmentDocument(configPath, snapshot.contents)
    : createAugmentDocument(configPath);
}

export async function writeAugmentDocument(rendered: RenderedAugmentDocument): Promise<void> {
  if (!rendered.changed) return;
  const change = await prepareManagedFileChange({
    filePath: rendered.document.configPath,
    label: 'Auggie user settings',
    next: rendered.next,
    mode: 0o600,
    maximumBytes: MAX_CONFIG_BYTES,
    expectedSource: rendered.document.raw,
    verifyExpectedSource: true,
  });
  if (!change) return;
  await commitManagedTransaction({
    displayName: 'Augment Code CLI',
    operation: 'uninstall',
    directories: [{
      path: path.dirname(rendered.document.configPath),
      label: 'Auggie configuration directory',
    }],
    changes: [change],
  });
}

function wrappersValid(contract: RuntimeContract): boolean {
  const agentDirectory = path.dirname(contract.guardPath);
  return samePath(contract.guardWrapperPath, path.join(agentDirectory, GUARD_WRAPPER))
    && samePath(contract.auditWrapperPath, path.join(agentDirectory, AUDIT_WRAPPER));
}

async function runtimeContractExists(contract: RuntimeContract): Promise<boolean> {
  if (!wrappersValid(contract)) return false;
  if (!await managedRuntimeFilesExist(contract, AGENT_KEY)) return false;
  const [guardWrapper, auditWrapper] = await Promise.all([
    readPhysicalFile(contract.guardWrapperPath, 'Auggie guard wrapper'),
    readPhysicalFile(contract.auditWrapperPath, 'Auggie audit wrapper'),
  ]);
  return guardWrapper?.contents === buildWrapper(contract.guardPath)
    && auditWrapper?.contents === buildWrapper(contract.auditPath);
}

export async function augmentRuntimeFilesExist(
  contracts: readonly RuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    if (await runtimeContractExists(contract)) return true;
  }
  return false;
}
