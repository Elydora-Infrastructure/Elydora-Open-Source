import type { InstallConfig } from './base.js';
import {
  commitManagedInstallation,
  managedRuntimePaths,
  preflightManagedInstallation,
  prepareManagedInstallation,
  type ManagedRuntimePaths,
  type PreparedManagedInstallation,
  type RenameFile,
} from './managed-installation.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type RenderedDocument,
} from './cursor-contract.js';
import { cursorConfigPath } from './cursor-io.js';

const DISPLAY_NAME = 'Cursor';
const HOOKS_DIRECTORY_LABEL = 'Cursor hooks directory';
const HOOKS_LABEL = 'Cursor user hooks';

export type CursorRuntimePaths = ManagedRuntimePaths;
export type PreparedCursorInstallation = PreparedManagedInstallation;
export type { RenameFile };

export function cursorRuntimePaths(config: InstallConfig): CursorRuntimePaths {
  return managedRuntimePaths(config, AGENT_KEY, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function preflightCursorInstallation(
  config: InstallConfig,
): Promise<CursorRuntimePaths> {
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
): Promise<PreparedCursorInstallation> {
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
    guardOptions: {
      failClosed: true,
      successOutput: '{"permission":"allow"}\n',
      denyProtocol: 'cursor',
    },
    auditOptions: {
      failClosed: true,
      nativePayload: true,
      successOutput: '{}\n',
    },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitCursorInstallation(
  prepared: PreparedCursorInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}
