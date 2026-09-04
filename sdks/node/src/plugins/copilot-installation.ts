import path from 'node:path';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type CopilotSources,
  type RenderedDocument,
} from './copilot-contract.js';
import { MAX_SOURCE_BYTES, samePath } from './common.js';
import { requireHooksEnabled } from './copilot-io.js';
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

const DISPLAY_NAME = 'GitHub Copilot CLI';
const DIRECTORY_LABEL = 'GitHub Copilot hooks directory';
const SOURCE_LABEL = 'GitHub Copilot hook source';

function hookLocations(sources: CopilotSources) {
  return [sources.user, ...(sources.legacy ? [sources.legacy] : [])].map((document) => ({
    directoryLabel: DIRECTORY_LABEL,
    filePath: document.filePath,
  }));
}

export async function preflightCopilotInstallation(
  config: InstallConfig,
  sources: CopilotSources,
): Promise<ManagedRuntimePaths> {
  requireHooksEnabled(sources);
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: hookLocations(sources),
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

function installationDocuments(
  sources: CopilotSources,
  rendered: readonly RenderedDocument[],
): readonly RenderedDocument[] {
  return rendered.filter((item) => item.changed
    || samePath(item.document.filePath, sources.user.filePath));
}

export async function prepareCopilotInstallation(
  config: InstallConfig,
  sources: CopilotSources,
  rendered: readonly RenderedDocument[],
): Promise<PreparedManagedInstallation> {
  await preflightCopilotInstallation(config, sources);
  const documents = installationDocuments(sources, rendered);
  const prepared = await prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: documents.map((item) => ({
      directoryLabel: DIRECTORY_LABEL,
      label: SOURCE_LABEL,
      filePath: item.document.filePath,
      expectedSource: item.document.raw,
      expectedSnapshot: item.document.snapshot,
      source: item.changed ? item.next : item.document.raw,
    })),
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
  const legacyPrecondition = sources.legacy
    && !documents.some((item) => samePath(item.document.filePath, sources.legacy!.filePath))
    ? [{
      filePath: sources.legacy.filePath,
      label: 'GitHub Copilot legacy project hooks',
      maximumBytes: MAX_SOURCE_BYTES,
      original: sources.legacy.snapshot,
    }]
    : [];
  return {
    ...prepared,
    transaction: {
      ...prepared.transaction,
      preconditions: [
        ...sources.settingsPreconditions.map((condition) => ({
          filePath: condition.filePath,
          label: condition.label,
          maximumBytes: MAX_SOURCE_BYTES,
          original: condition.snapshot,
        })),
        ...legacyPrecondition,
      ],
    },
  };
}

export async function commitCopilotInstallation(
  prepared: PreparedManagedInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

export async function prepareCopilotUninstall(
  rendered: readonly RenderedDocument[],
): Promise<PreparedManagedTransaction> {
  const changed = rendered.filter((item) => item.changed);
  const changes = await Promise.all(changed.map((item) => prepareManagedFileChange({
    filePath: item.document.filePath,
    label: SOURCE_LABEL,
    next: item.next,
    mode: 0o600,
    expectedSource: item.document.raw,
    expectedSnapshot: item.document.snapshot,
    verifyExpectedSource: true,
  })));
  return {
    displayName: DISPLAY_NAME,
    operation: 'uninstall',
    directories: changed.map((item) => ({
      path: path.dirname(item.document.filePath),
      label: DIRECTORY_LABEL,
    })),
    changes: changes.filter((change) => change !== undefined),
  };
}

export async function commitCopilotUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
