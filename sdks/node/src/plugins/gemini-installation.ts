import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
} from './gemini-contract.js';
import type { RenderedGeminiDocument } from './gemini-config.js';
import {
  commitManagedInstallation,
  preflightManagedInstallation,
  prepareManagedInstallation,
  type ManagedRuntimePaths,
  type PreparedManagedInstallation,
  type RenameFile,
} from './managed-installation.js';

const DISPLAY_NAME = 'Gemini CLI';
const SETTINGS_DIRECTORY_LABEL = 'Gemini CLI configuration directory';
const SETTINGS_LABEL = 'Gemini CLI user settings';

export type GeminiRuntimePaths = ManagedRuntimePaths;
export type PreparedGeminiInstallation = PreparedManagedInstallation;
export type { RenameFile };

export async function preflightGeminiInstallation(
  config: InstallConfig,
  settingsPath: string,
): Promise<GeminiRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: [{ directoryLabel: SETTINGS_DIRECTORY_LABEL, filePath: settingsPath }],
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareGeminiInstallation(
  config: InstallConfig,
  rendered: RenderedGeminiDocument,
): Promise<PreparedGeminiInstallation> {
  const settingsSource = rendered.next ?? rendered.document.raw;
  if (settingsSource === undefined) throw new Error('Gemini CLI hook settings are missing');
  return prepareManagedInstallation({
    agentKey: AGENT_KEY,
    displayName: DISPLAY_NAME,
    hookSources: [{
      directoryLabel: SETTINGS_DIRECTORY_LABEL,
      label: SETTINGS_LABEL,
      filePath: rendered.document.filePath,
      expectedSource: rendered.document.exists ? rendered.document.raw : undefined,
      source: settingsSource,
    }],
    config,
    guardOptions: { successOutput: '{}\n' },
    auditOptions: { nativePayload: true, successOutput: '{}\n' },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function commitGeminiInstallation(
  prepared: PreparedGeminiInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}
