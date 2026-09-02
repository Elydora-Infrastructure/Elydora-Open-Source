import path from 'node:path';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  LETTA_AUDIT_OPTIONS,
} from './letta-contract.js';
import { sameLettaPath } from './letta-command.js';
import {
  lettaDocumentLabel,
  type RenderedLettaDocument,
} from './letta-config.js';
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
import {
  requireLettaHooksEnabled,
  type LettaSources,
} from './letta-sources.js';

const DISPLAY_NAME = 'Letta Code';
const SETTINGS_DIRECTORY_LABEL = 'Letta Code global configuration directory';
const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

export type LettaRuntimePaths = ManagedRuntimePaths;
export type PreparedLettaInstallation = PreparedManagedInstallation;
export type { RenameFile };

export async function preflightLettaInstallation(
  config: InstallConfig,
  sources: LettaSources,
): Promise<LettaRuntimePaths> {
  requireLettaHooksEnabled(sources);
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: [{
      directoryLabel: SETTINGS_DIRECTORY_LABEL,
      filePath: sources.global.filePath,
    }],
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

function readOnlyPreconditions(sources: LettaSources, changedPath?: string) {
  return sources.preconditions.filter((condition) => (
    changedPath === undefined || !sameLettaPath(condition.filePath, changedPath)
  ));
}

export async function prepareLettaInstallation(
  config: InstallConfig,
  sources: LettaSources,
  rendered: RenderedLettaDocument,
): Promise<PreparedLettaInstallation> {
  await preflightLettaInstallation(config, sources);
  const settingsSource = rendered.next ?? rendered.document.raw;
  const prepared = await prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: [{
      directoryLabel: SETTINGS_DIRECTORY_LABEL,
      label: lettaDocumentLabel(rendered.document),
      filePath: rendered.document.filePath,
      expectedSource: rendered.document.exists ? rendered.document.raw : undefined,
      expectedSnapshot: rendered.document.snapshot,
      source: settingsSource,
    }],
    config,
    auditOptions: LETTA_AUDIT_OPTIONS,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
  return {
    ...prepared,
    transaction: {
      ...prepared.transaction,
      preconditions: readOnlyPreconditions(sources, rendered.document.filePath),
    },
  };
}

export async function commitLettaInstallation(
  prepared: PreparedLettaInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

export async function prepareLettaUninstall(
  sources: LettaSources,
  rendered: RenderedLettaDocument,
): Promise<PreparedManagedTransaction> {
  const change = rendered.changed
    ? await prepareManagedFileChange({
      filePath: rendered.document.filePath,
      label: lettaDocumentLabel(rendered.document),
      next: rendered.next,
      mode: 0o600,
      maximumBytes: MAX_SOURCE_BYTES,
      expectedSource: rendered.document.exists ? rendered.document.raw : undefined,
      expectedSnapshot: rendered.document.snapshot,
      verifyExpectedSource: true,
    })
    : undefined;
  return {
    displayName: DISPLAY_NAME,
    operation: 'uninstall',
    directories: [{
      path: path.dirname(rendered.document.filePath),
      label: SETTINGS_DIRECTORY_LABEL,
    }],
    changes: change ? [change] : [],
    preconditions: readOnlyPreconditions(
      sources,
      rendered.changed ? rendered.document.filePath : undefined,
    ),
  };
}

export async function commitLettaUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
