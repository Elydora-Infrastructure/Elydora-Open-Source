import path from 'node:path';
import type { InstallConfig } from './base.js';
import { sameAgentId } from './common.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type KiroIdePaths,
  type RenderedKiroIdeDocument,
} from './kiroide-contract.js';
import {
  type KiroIdeSources,
  type LegacyKiroIdeDocument,
  requirePhysicalLegacyDirectory,
} from './kiroide-io.js';
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
  type ManagedFileChange,
  type PreparedManagedTransaction,
} from './managed-transaction.js';

const DISPLAY_NAME = 'Kiro IDE';
const DIRECTORY_LABEL = 'Kiro IDE hooks directory';

function hookLocation(paths: KiroIdePaths) {
  return [{ directoryLabel: DIRECTORY_LABEL, filePath: paths.configPath }];
}

function removesLegacy(legacy: LegacyKiroIdeDocument, agentId?: string): boolean {
  return legacy.contract !== undefined
    && (agentId === undefined || sameAgentId(legacy.contract.agentId, agentId));
}

async function prepareLegacyRemoval(
  legacy: LegacyKiroIdeDocument,
  agentId?: string,
): Promise<ManagedFileChange | undefined> {
  if (!removesLegacy(legacy, agentId)) return undefined;
  if (legacy.raw === undefined) throw new Error(`Legacy Kiro IDE hook source is missing: ${legacy.filePath}`);
  await requirePhysicalLegacyDirectory(legacy);
  return prepareManagedFileChange({
    filePath: legacy.filePath,
    label: 'legacy Kiro IDE hook',
    next: undefined,
    mode: 0o600,
    expectedSource: legacy.raw,
    verifyExpectedSource: true,
  });
}

export async function preflightKiroIdeInstallation(
  config: InstallConfig,
  paths: KiroIdePaths,
): Promise<ManagedRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: hookLocation(paths),
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareKiroIdeInstallation(
  config: InstallConfig,
  sources: KiroIdeSources,
  rendered: RenderedKiroIdeDocument,
): Promise<PreparedManagedInstallation> {
  if (rendered.next === undefined) throw new Error('Kiro IDE installation requires a hook document');
  const prepared = await prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: [{
      directoryLabel: DIRECTORY_LABEL,
      label: 'Kiro IDE hooks',
      filePath: sources.paths.configPath,
      expectedSource: sources.document.raw,
      source: rendered.next,
    }],
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
  const legacyChange = await prepareLegacyRemoval(sources.legacy, config.agentId);
  if (!legacyChange) return prepared;
  return {
    ...prepared,
    transaction: {
      ...prepared.transaction,
      directories: [
        ...prepared.transaction.directories,
        { path: path.dirname(sources.legacy.filePath), label: 'legacy Kiro IDE hooks directory' },
      ],
      changes: [...prepared.transaction.changes, legacyChange],
    },
  };
}

export async function commitKiroIdeInstallation(
  prepared: PreparedManagedInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

export async function prepareKiroIdeUninstall(
  sources: KiroIdeSources,
  rendered: RenderedKiroIdeDocument,
  agentId?: string,
): Promise<PreparedManagedTransaction> {
  const changes: ManagedFileChange[] = [];
  const directories = [];
  if (rendered.changed) {
    const configChange = await prepareManagedFileChange({
      filePath: sources.document.filePath,
      label: 'Kiro IDE hooks',
      next: rendered.next,
      mode: 0o600,
      expectedSource: sources.document.raw,
      verifyExpectedSource: true,
    });
    if (configChange) {
      changes.push(configChange);
      directories.push({ path: sources.paths.hooksDirectory, label: DIRECTORY_LABEL });
    }
  }
  const legacyChange = await prepareLegacyRemoval(sources.legacy, agentId);
  if (legacyChange) {
    changes.push(legacyChange);
    directories.push({
      path: path.dirname(sources.legacy.filePath),
      label: 'legacy Kiro IDE hooks directory',
    });
  }
  return {
    displayName: DISPLAY_NAME,
    operation: 'uninstall',
    directories,
    changes,
  };
}

export async function commitKiroIdeUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
