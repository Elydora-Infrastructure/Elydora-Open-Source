import path from 'node:path';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  assertWrapperIntegrity,
  buildMetadata,
  buildWrapper,
  type ClineHookFile,
  runtimeContract,
  sameAgentId,
} from './cline-contract.js';
import {
  commitManagedInstallation,
  preflightManagedInstallation,
  prepareManagedInstallation,
  type ManagedRuntimePaths,
  type PreparedManagedInstallation,
  type RenameFile,
} from './managed-installation.js';
import {
  commitManagedTransaction,
  prepareManagedFileChange,
  type PreparedManagedTransaction,
} from './managed-transaction.js';

const DISPLAY_NAME = 'Cline';
const DIRECTORY_LABEL = 'Cline hooks directory';

export type ClineRuntimePaths = ManagedRuntimePaths;
export type PreparedClineInstallation = PreparedManagedInstallation;
export type { RenameFile };

function hookLocations(files: readonly ClineHookFile[]) {
  return files.map((file) => ({
    directoryLabel: DIRECTORY_LABEL,
    filePath: file.filePath,
  }));
}

export async function preflightClineInstallation(
  config: InstallConfig,
  files: readonly ClineHookFile[],
): Promise<ClineRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: hookLocations(files),
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareClineInstallation(
  config: InstallConfig,
  guardFile: ClineHookFile,
  auditFile: ClineHookFile,
): Promise<PreparedClineInstallation> {
  const paths = await preflightClineInstallation(config, [guardFile, auditFile]);
  const guardMetadata = buildMetadata('guard', config.agentId, paths.guardPath);
  const auditMetadata = buildMetadata('audit', config.agentId, paths.auditPath);
  const guardSource = buildWrapper(guardMetadata);
  const auditSource = buildWrapper(auditMetadata);
  runtimeContract(
    { exists: true, filePath: guardFile.filePath, source: guardSource, metadata: guardMetadata },
    { exists: true, filePath: auditFile.filePath, source: auditSource, metadata: auditMetadata },
  );
  return prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: [{
      directoryLabel: DIRECTORY_LABEL,
      label: 'Cline PreToolUse hook',
      filePath: guardFile.filePath,
      expectedSource: guardFile.source,
      source: guardSource,
    }, {
      directoryLabel: DIRECTORY_LABEL,
      label: 'Cline PostToolUse hook',
      filePath: auditFile.filePath,
      expectedSource: auditFile.source,
      source: auditSource,
    }],
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitClineInstallation(
  prepared: PreparedClineInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

export async function prepareClineUninstall(
  files: readonly ClineHookFile[],
  agentId?: string,
): Promise<PreparedManagedTransaction> {
  const owned = files.filter((file) => {
    const ownedAgentId = file.metadata?.agentId;
    return ownedAgentId !== undefined
      && (agentId === undefined || sameAgentId(ownedAgentId, agentId));
  });
  for (const file of owned) assertWrapperIntegrity(file);
  const changes = await Promise.all(owned.map((file) => prepareManagedFileChange({
    filePath: file.filePath,
    label: `Cline ${file.metadata!.kind} hook`,
    next: undefined,
    mode: 0o600,
    expectedSource: file.source,
    verifyExpectedSource: true,
  })));
  return {
    displayName: DISPLAY_NAME,
    operation: 'uninstall',
    directories: owned.map((file) => ({
      path: path.dirname(file.filePath),
      label: DIRECTORY_LABEL,
    })),
    changes: changes.filter((change) => change !== undefined),
  };
}

export async function commitClineUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
