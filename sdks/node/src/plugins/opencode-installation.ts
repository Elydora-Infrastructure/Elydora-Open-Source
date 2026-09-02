import path from 'node:path';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  OPENCODE_AUDIT_OPTIONS,
  buildOpenCodeMetadata,
  buildOpenCodePlugin,
  sameOpenCodeAgentId,
} from './opencode-contract.js';
import type { OpenCodePluginFile, OpenCodeSources } from './opencode-io.js';
import {
  commitManagedInstallation,
  preflightManagedInstallation,
  prepareManagedInstallation,
  type ManagedHookSource,
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

const DISPLAY_NAME = 'OpenCode';
const DIRECTORY_LABEL = 'OpenCode plugins directory';

export type OpenCodeRuntimePaths = ManagedRuntimePaths;
export type PreparedOpenCodeInstallation = PreparedManagedInstallation;
export type { RenameFile };

export async function preflightOpenCodeInstallation(
  config: InstallConfig,
  sources: OpenCodeSources,
): Promise<OpenCodeRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: [{
      directoryLabel: DIRECTORY_LABEL,
      filePath: sources.paths.pluginPath,
    }],
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareOpenCodeInstallation(
  config: InstallConfig,
  sources: OpenCodeSources,
): Promise<PreparedOpenCodeInstallation> {
  const paths = await preflightOpenCodeInstallation(config, sources);
  const metadata = buildOpenCodeMetadata(
    config.agentId,
    process.execPath,
    paths.guardPath,
    paths.auditPath,
  );
  const hookSources: ManagedHookSource[] = [{
    directoryLabel: DIRECTORY_LABEL,
    label: 'OpenCode plugin',
    filePath: sources.current.filePath,
    expectedSource: sources.current.source,
    expectedSnapshot: sources.current.snapshot,
    source: buildOpenCodePlugin(metadata),
  }];
  if (sources.legacy.contract) {
    hookSources.push({
      directoryLabel: DIRECTORY_LABEL,
      label: 'legacy OpenCode plugin',
      filePath: sources.legacy.filePath,
      expectedSource: sources.legacy.source,
      expectedSnapshot: sources.legacy.snapshot,
    });
  }
  return prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources,
    config,
    auditOptions: OPENCODE_AUDIT_OPTIONS,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitOpenCodeInstallation(
  prepared: PreparedOpenCodeInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

function ownedByAgent<TContract extends { readonly agentId: string }>(
  file: OpenCodePluginFile<TContract>,
  agentId?: string,
): boolean {
  return file.contract !== undefined
    && (agentId === undefined || sameOpenCodeAgentId(file.contract.agentId, agentId));
}

export async function prepareOpenCodeUninstall(
  sources: OpenCodeSources,
  agentId?: string,
): Promise<PreparedManagedTransaction> {
  const owned = [sources.current, sources.legacy].filter((file) => ownedByAgent(file, agentId));
  const changes = await Promise.all(owned.map((file) => prepareManagedFileChange({
    filePath: file.filePath,
    label: file.filePath === sources.paths.pluginPath ? 'OpenCode plugin' : 'legacy OpenCode plugin',
    next: undefined,
    mode: 0o600,
    expectedSource: file.source,
    expectedSnapshot: file.snapshot,
    verifyExpectedSource: true,
  })));
  return {
    displayName: DISPLAY_NAME,
    operation: 'uninstall',
    directories: owned.map((file) => ({
      path: path.dirname(file.filePath),
      label: DIRECTORY_LABEL,
    })),
    changes: changes.filter((change): change is ManagedFileChange => change !== undefined),
  };
}

export async function commitOpenCodeUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
