import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type RenderedDocument,
} from './codex-contract.js';
import {
  commitManagedInstallation,
  managedRuntimePaths,
  preflightManagedInstallation,
  prepareManagedInstallation,
  type ManagedRuntimePaths,
  type PreparedManagedInstallation,
  type RenameFile,
} from './managed-installation.js';

const DISPLAY_NAME = 'Codex';
const HOOKS_DIRECTORY_LABEL = 'Codex hooks directory';
const HOOKS_LABEL = 'Codex user hooks';

export type CodexRuntimePaths = ManagedRuntimePaths;
export type PreparedCodexInstallation = PreparedManagedInstallation;
export type { RenameFile };

export function codexRuntimePaths(config: InstallConfig): CodexRuntimePaths {
  return managedRuntimePaths(config, AGENT_KEY, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function preflightCodexInstallation(
  config: InstallConfig,
  hooksPath: string,
): Promise<CodexRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: [{ directoryLabel: HOOKS_DIRECTORY_LABEL, filePath: hooksPath }],
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareCodexInstallation(
  config: InstallConfig,
  rendered: RenderedDocument,
): Promise<PreparedCodexInstallation> {
  if (!rendered.changed && rendered.document.raw === undefined) {
    throw new Error('Codex hook installation did not produce a configuration document');
  }
  const hooksSource = rendered.next ?? rendered.document.raw;
  if (hooksSource === undefined) throw new Error('Codex hook configuration is missing');
  return prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: [{
      directoryLabel: HOOKS_DIRECTORY_LABEL,
      label: HOOKS_LABEL,
      filePath: rendered.document.filePath,
      expectedSource: rendered.document.raw,
      source: hooksSource,
    }],
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitCodexInstallation(
  prepared: PreparedCodexInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}
