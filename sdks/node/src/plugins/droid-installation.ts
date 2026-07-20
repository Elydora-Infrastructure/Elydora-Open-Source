import path from 'node:path';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  samePath,
} from './droid-contract.js';
import {
  activeDocument,
  installationDocuments,
  sourceDocuments,
  type DroidDocument,
  type DroidSources,
  type RenderedDocument,
} from './droid-config.js';
import { requireHooksEnabled } from './droid-io.js';
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

const DISPLAY_NAME = 'Factory Droid';
const DIRECTORY_LABEL = 'Factory Droid user configuration directory';
const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

export type DroidRuntimePaths = ManagedRuntimePaths;
export type PreparedDroidInstallation = PreparedManagedInstallation;
export type { RenameFile };

function sourceLabel(document: DroidDocument): string {
  if (document.kind === 'settings') return 'Factory Droid user settings';
  if (document.kind === 'local-settings') return 'Factory Droid local settings';
  if (document.kind === 'legacy') return 'Factory Droid legacy hooks';
  return 'Factory Droid user hooks';
}

function hookLocations(sources: DroidSources) {
  return installationDocuments(sources).map((document) => ({
    directoryLabel: document.kind === 'legacy'
      ? 'Factory Droid legacy hooks directory'
      : DIRECTORY_LABEL,
    filePath: document.filePath,
  }));
}

export async function preflightDroidInstallation(
  config: InstallConfig,
  sources: DroidSources,
): Promise<DroidRuntimePaths> {
  requireHooksEnabled(sources);
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: hookLocations(sources),
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

function installationChanges(
  sources: DroidSources,
  rendered: readonly RenderedDocument[],
): readonly RenderedDocument[] {
  const target = activeDocument(sources);
  return rendered.filter((item) => item.changed || item.document === target);
}

function sourcePreconditions(
  sources: DroidSources,
  changed: readonly RenderedDocument[],
) {
  const changedPaths = changed.map((item) => item.document.filePath);
  return sourceDocuments(sources)
    .filter((document) => !changedPaths.some((filePath) => samePath(filePath, document.filePath)))
    .map((document) => ({
      filePath: document.filePath,
      label: sourceLabel(document),
      maximumBytes: MAX_SOURCE_BYTES,
      original: document.snapshot,
    }));
}

export async function prepareDroidInstallation(
  config: InstallConfig,
  sources: DroidSources,
  rendered: readonly RenderedDocument[],
): Promise<PreparedDroidInstallation> {
  await preflightDroidInstallation(config, sources);
  const changed = installationChanges(sources, rendered);
  const prepared = await prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: changed.map((item) => ({
      directoryLabel: item.document.kind === 'legacy'
        ? 'Factory Droid legacy hooks directory'
        : DIRECTORY_LABEL,
      label: sourceLabel(item.document),
      filePath: item.document.filePath,
      expectedSource: item.document.exists ? item.document.raw : undefined,
      expectedSnapshot: item.document.snapshot,
      source: item.changed ? item.next : item.document.raw,
    })),
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
  return {
    ...prepared,
    transaction: {
      ...prepared.transaction,
      preconditions: [
        ...sourcePreconditions(sources, changed),
        ...sources.policy.preconditions,
      ],
    },
  };
}

export async function commitDroidInstallation(
  prepared: PreparedDroidInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

export async function prepareDroidUninstall(
  rendered: readonly RenderedDocument[],
): Promise<PreparedManagedTransaction> {
  const changed = rendered.filter((item) => item.changed);
  const changes = await Promise.all(changed.map((item) => prepareManagedFileChange({
    filePath: item.document.filePath,
    label: sourceLabel(item.document),
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
      label: item.document.kind === 'legacy'
        ? 'Factory Droid legacy hooks directory'
        : DIRECTORY_LABEL,
    })),
    changes: changes.filter((change) => change !== undefined),
    preconditions: rendered
      .filter((item) => !item.changed)
      .map((item) => ({
        filePath: item.document.filePath,
        label: sourceLabel(item.document),
        maximumBytes: MAX_SOURCE_BYTES,
        original: item.document.snapshot,
      })),
  };
}

export async function commitDroidUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
