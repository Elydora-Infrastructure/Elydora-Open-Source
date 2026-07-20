import path from 'node:path';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
} from './qwen-contract.js';
import { sameQwenPath } from './qwen-command.js';
import {
  qwenDocumentLabel,
  type RenderedQwenDocument,
} from './qwen-config.js';
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
  requireQwenHooksEnabled,
  type QwenSources,
} from './qwen-sources.js';

const DISPLAY_NAME = 'Qwen Code';
const SETTINGS_DIRECTORY_LABEL = 'Qwen Code user configuration directory';
const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

export type QwenRuntimePaths = ManagedRuntimePaths;
export type PreparedQwenInstallation = PreparedManagedInstallation;
export type { RenameFile };

export async function preflightQwenInstallation(
  config: InstallConfig,
  sources: QwenSources,
): Promise<QwenRuntimePaths> {
  requireQwenHooksEnabled(sources);
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: [{
      directoryLabel: SETTINGS_DIRECTORY_LABEL,
      filePath: sources.user.filePath,
    }],
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

function readOnlyPreconditions(sources: QwenSources, changedPath?: string) {
  return sources.preconditions.filter((condition) => (
    changedPath === undefined || !sameQwenPath(condition.filePath, changedPath)
  ));
}

export async function prepareQwenInstallation(
  config: InstallConfig,
  sources: QwenSources,
  rendered: RenderedQwenDocument,
): Promise<PreparedQwenInstallation> {
  await preflightQwenInstallation(config, sources);
  const settingsSource = rendered.next ?? rendered.document.raw;
  const prepared = await prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: [{
      directoryLabel: SETTINGS_DIRECTORY_LABEL,
      label: qwenDocumentLabel(rendered.document),
      filePath: rendered.document.filePath,
      expectedSource: rendered.document.exists ? rendered.document.raw : undefined,
      expectedSnapshot: rendered.document.snapshot,
      source: settingsSource,
    }],
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
  return {
    ...prepared,
    transaction: {
      ...prepared.transaction,
      preconditions: readOnlyPreconditions(sources, rendered.document.filePath),
    },
  };
}

export async function commitQwenInstallation(
  prepared: PreparedQwenInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

export async function prepareQwenUninstall(
  sources: QwenSources,
  rendered: RenderedQwenDocument,
): Promise<PreparedManagedTransaction> {
  const change = rendered.changed
    ? await prepareManagedFileChange({
      filePath: rendered.document.filePath,
      label: qwenDocumentLabel(rendered.document),
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

export async function commitQwenUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
