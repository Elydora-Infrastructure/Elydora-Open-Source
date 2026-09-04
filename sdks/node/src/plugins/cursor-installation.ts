import type { InstallConfig } from './base.js';
import {
  commitManagedInstallation,
  preflightManagedInstallation,
  prepareManagedInstallation,
  type ManagedRuntimePaths,
  type PreparedManagedInstallation,
  type RenameFile,
} from './managed-installation.js';
import {
  AGENT_KEY,
  AUDIT_OPTIONS,
  AUDIT_SCRIPT,
  GUARD_OPTIONS,
  GUARD_SCRIPT,
  type RenderedDocument,
} from './cursor-contract.js';
import { cursorConfigPath } from './cursor-io.js';

const DISPLAY_NAME = 'Cursor';
const HOOKS_DIRECTORY_LABEL = 'Cursor hooks directory';
const HOOKS_LABEL = 'Cursor user hooks';

export async function preflightCursorInstallation(
  config: InstallConfig,
): Promise<ManagedRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: [{
      directoryLabel: HOOKS_DIRECTORY_LABEL,
      filePath: cursorConfigPath(),
    }],
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareCursorInstallation(
  config: InstallConfig,
  rendered: RenderedDocument,
): Promise<PreparedManagedInstallation> {
  if (!rendered.changed && rendered.document.raw === undefined) {
    throw new Error('Cursor hook installation did not produce a configuration document');
  }
  const hooksSource = rendered.next ?? rendered.document.raw;
  if (hooksSource === undefined) throw new Error('Cursor hook configuration is missing');
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
    guardOptions: GUARD_OPTIONS,
    auditOptions: AUDIT_OPTIONS,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitCursorInstallation(
  prepared: PreparedManagedInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}
