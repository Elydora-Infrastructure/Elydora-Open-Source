import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type RenderedClaudeDocument,
} from './claudecode-contract.js';
import {
  commitManagedInstallation,
  preflightManagedInstallation,
  prepareManagedInstallation,
  type ManagedRuntimePaths,
  type PreparedManagedInstallation,
  type RenameFile,
} from './managed-installation.js';

const DISPLAY_NAME = 'Claude Code';
const SETTINGS_DIRECTORY_LABEL = 'Claude Code configuration directory';
const SETTINGS_LABEL = 'Claude Code user settings';

export async function preflightClaudeInstallation(
  config: InstallConfig,
  settingsPath: string,
): Promise<ManagedRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: [{ directoryLabel: SETTINGS_DIRECTORY_LABEL, filePath: settingsPath }],
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareClaudeInstallation(
  config: InstallConfig,
  rendered: RenderedClaudeDocument,
): Promise<PreparedManagedInstallation> {
  if (!rendered.changed && rendered.document.raw === undefined) {
    throw new Error('Claude Code hook installation did not produce a settings document');
  }
  const settingsSource = rendered.next ?? rendered.document.raw;
  if (settingsSource === undefined) throw new Error('Claude Code hook settings are missing');
  return prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: [{
      directoryLabel: SETTINGS_DIRECTORY_LABEL,
      label: SETTINGS_LABEL,
      filePath: rendered.document.filePath,
      expectedSource: rendered.document.raw,
      source: settingsSource,
    }],
    config,
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitClaudeInstallation(
  prepared: PreparedManagedInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}
