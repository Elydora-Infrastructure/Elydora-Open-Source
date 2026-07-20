import path from 'node:path';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type KimiConfigDocument,
  type RenderedKimiDocument,
} from './kimi-contract.js';
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

const DISPLAY_NAME = 'Kimi';

export type KimiRuntimePaths = ManagedRuntimePaths;
export type PreparedKimiInstallation = PreparedManagedInstallation;
export type { RenameFile };

function hookLocations(documents: readonly KimiConfigDocument[]) {
  return documents.map((document) => ({
    directoryLabel: document.contract.directoryLabel,
    filePath: document.contract.configPath,
  }));
}

export async function preflightKimiInstallation(
  config: InstallConfig,
  documents: readonly KimiConfigDocument[],
): Promise<KimiRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: hookLocations(documents),
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

function renderedSource(rendered: RenderedKimiDocument): string {
  const source = rendered.next ?? rendered.document.raw;
  if (source === undefined) {
    throw new Error(`${rendered.document.contract.label} installation source is missing`);
  }
  return source;
}

export async function prepareKimiInstallation(
  config: InstallConfig,
  rendered: readonly RenderedKimiDocument[],
): Promise<PreparedKimiInstallation> {
  return prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: rendered.map((item) => ({
      directoryLabel: item.document.contract.directoryLabel,
      label: item.document.contract.label,
      filePath: item.document.contract.configPath,
      expectedSource: item.document.raw,
      source: renderedSource(item),
    })),
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitKimiInstallation(
  prepared: PreparedKimiInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

export async function prepareKimiUninstall(
  rendered: readonly RenderedKimiDocument[],
): Promise<PreparedManagedTransaction> {
  const changed = rendered.filter((item) => item.changed);
  const changes = await Promise.all(changed.map((item) => prepareManagedFileChange({
    filePath: item.document.contract.configPath,
    label: item.document.contract.label,
    next: item.next,
    mode: 0o600,
    expectedSource: item.document.raw,
    verifyExpectedSource: true,
  })));
  return {
    displayName: DISPLAY_NAME,
    operation: 'uninstall',
    directories: changed.map((item) => ({
      path: path.dirname(item.document.contract.configPath),
      label: item.document.contract.directoryLabel,
    })),
    changes: changes.filter((change) => change !== undefined),
  };
}

export async function commitKimiUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
