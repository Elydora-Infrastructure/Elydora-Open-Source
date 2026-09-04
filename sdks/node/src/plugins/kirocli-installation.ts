import path from 'node:path';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type RenderedKiroCliV2Document,
} from './kirocli-contract.js';
import type { KiroCliSources } from './kirocli-io.js';
import type { RenderedKiroIdeDocument } from './kiroide-contract.js';
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

const DISPLAY_NAME = 'Kiro CLI';
const V2_DIRECTORY_LABEL = 'Kiro CLI agents directory';
const V3_DIRECTORY_LABEL = 'Kiro CLI global hooks directory';

function hookLocations(sources: KiroCliSources) {
  return [
    { directoryLabel: V2_DIRECTORY_LABEL, filePath: sources.paths.v2Path },
    { directoryLabel: V3_DIRECTORY_LABEL, filePath: sources.paths.v3Path },
  ];
}

export async function preflightKiroCliInstallation(
  config: InstallConfig,
  sources: KiroCliSources,
): Promise<ManagedRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: hookLocations(sources),
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareKiroCliInstallation(
  config: InstallConfig,
  sources: KiroCliSources,
  v2: RenderedKiroCliV2Document,
  v3: RenderedKiroIdeDocument,
): Promise<PreparedManagedInstallation> {
  if (v2.next === undefined || v3.next === undefined) {
    throw new Error('Kiro CLI installation requires both v2 and v3 hook documents');
  }
  return prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: [
      {
        directoryLabel: V2_DIRECTORY_LABEL,
        label: 'Kiro CLI v2 agent config',
        filePath: sources.paths.v2Path,
        expectedSource: sources.v2.raw,
        source: v2.next,
      },
      {
        directoryLabel: V3_DIRECTORY_LABEL,
        label: 'Kiro CLI v3 global hooks',
        filePath: sources.paths.v3Path,
        expectedSource: sources.v3.raw,
        source: v3.next,
      },
    ],
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitKiroCliInstallation(
  prepared: PreparedManagedInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

export async function prepareKiroCliUninstall(
  sources: KiroCliSources,
  v2: RenderedKiroCliV2Document,
  v3: RenderedKiroIdeDocument,
): Promise<PreparedManagedTransaction> {
  const changes: ManagedFileChange[] = [];
  const directories = [];
  for (const item of [
    { rendered: v2, label: 'Kiro CLI v2 agent config', directoryLabel: V2_DIRECTORY_LABEL },
    { rendered: v3, label: 'Kiro CLI v3 global hooks', directoryLabel: V3_DIRECTORY_LABEL },
  ]) {
    if (!item.rendered.changed) continue;
    const change = await prepareManagedFileChange({
      filePath: item.rendered.document.filePath,
      label: item.label,
      next: item.rendered.next,
      mode: 0o600,
      expectedSource: item.rendered.document.raw,
      verifyExpectedSource: true,
    });
    if (!change) continue;
    changes.push(change);
    directories.push({
      path: path.dirname(item.rendered.document.filePath),
      label: item.directoryLabel,
    });
  }
  return { displayName: DISPLAY_NAME, operation: 'uninstall', directories, changes };
}

export async function commitKiroCliUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
