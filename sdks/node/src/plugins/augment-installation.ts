import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  AUDIT_WRAPPER,
  GUARD_SCRIPT,
  GUARD_WRAPPER,
  buildWrapper,
  type RenderedAugmentDocument,
} from './augment-contract.js';
import {
  commitManagedInstallation,
  managedRuntimePaths,
  preflightManagedInstallation,
  prepareManagedInstallation,
  type ManagedRuntimePaths,
  type PreparedManagedInstallation,
  type RenameFile,
} from './managed-installation.js';

const DISPLAY_NAME = 'Augment Code CLI';
const SETTINGS_DIRECTORY_LABEL = 'Auggie configuration directory';
const SETTINGS_LABEL = 'Auggie user settings';

export type AugmentRuntimePaths = ManagedRuntimePaths;
export type PreparedAugmentInstallation = PreparedManagedInstallation;
export type { RenameFile };

export function augmentRuntimePaths(config: InstallConfig): AugmentRuntimePaths {
  return managedRuntimePaths(config, AGENT_KEY, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function preflightAugmentInstallation(
  config: InstallConfig,
  settingsPath: string,
): Promise<AugmentRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: [{ directoryLabel: SETTINGS_DIRECTORY_LABEL, filePath: settingsPath }],
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareAugmentInstallation(
  config: InstallConfig,
  rendered: RenderedAugmentDocument,
): Promise<PreparedAugmentInstallation> {
  if (!rendered.changed && rendered.document.raw === undefined) {
    throw new Error('Auggie hook installation did not produce a settings document');
  }
  const settingsSource = rendered.next ?? rendered.document.raw;
  if (settingsSource === undefined) throw new Error('Auggie hook settings are missing');
  const paths = augmentRuntimePaths(config);
  return prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: [{
      directoryLabel: SETTINGS_DIRECTORY_LABEL,
      label: SETTINGS_LABEL,
      filePath: rendered.document.configPath,
      expectedSource: rendered.document.raw,
      source: settingsSource,
    }],
    runtimeFiles: [{
      fileName: GUARD_WRAPPER,
      label: 'Auggie guard wrapper',
      source: buildWrapper(paths.guardPath),
      mode: 0o700,
    }, {
      fileName: AUDIT_WRAPPER,
      label: 'Auggie audit wrapper',
      source: buildWrapper(paths.auditPath),
      mode: 0o700,
    }],
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitAugmentInstallation(
  prepared: PreparedAugmentInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}
