import path from 'node:path';
import type { InstallConfig } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  type RenderedGrokDocument,
} from './grok-contract.js';
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

const DISPLAY_NAME = 'Grok Build';
const HOOKS_DIRECTORY_LABEL = 'Grok hooks directory';
const HOOKS_LABEL = 'Grok user hooks';

export type GrokRuntimePaths = ManagedRuntimePaths;
export type PreparedGrokInstallation = PreparedManagedInstallation;
export type { RenameFile };

export async function preflightGrokInstallation(
  config: InstallConfig,
  hooksPath: string,
): Promise<GrokRuntimePaths> {
  return preflightManagedInstallation({
    agentKey: AGENT_KEY,
    hookLocations: [{ directoryLabel: HOOKS_DIRECTORY_LABEL, filePath: hooksPath }],
    config,
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
}

export async function prepareGrokInstallation(
  config: InstallConfig,
  rendered: RenderedGrokDocument,
): Promise<PreparedGrokInstallation> {
  const hooksSource = rendered.next ?? rendered.document.raw;
  if (hooksSource === undefined) throw new Error('Grok hook configuration is missing');
  const prepared = await prepareManagedInstallation({
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
    guardOptions: { denyProtocol: 'grok' },
    auditOptions: { nativePayload: true },
  }, GUARD_SCRIPT, AUDIT_SCRIPT);
  const hooksDirectory = path.dirname(rendered.document.filePath);
  return {
    ...prepared,
    transaction: {
      ...prepared.transaction,
      directories: [
        { path: path.dirname(hooksDirectory), label: 'Grok home directory' },
        ...prepared.transaction.directories,
      ],
    },
  };
}

export async function commitGrokInstallation(
  prepared: PreparedGrokInstallation,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedInstallation(prepared, renameFile);
}

export async function prepareGrokUninstall(
  rendered: RenderedGrokDocument,
): Promise<PreparedManagedTransaction> {
  const change = rendered.changed
    ? await prepareManagedFileChange({
      filePath: rendered.document.filePath,
      label: HOOKS_LABEL,
      next: rendered.next,
      mode: 0o600,
      expectedSource: rendered.document.raw,
      verifyExpectedSource: true,
    })
    : undefined;
  return {
    displayName: DISPLAY_NAME,
    operation: 'uninstall',
    directories: rendered.changed
      ? [
        {
          path: path.dirname(path.dirname(rendered.document.filePath)),
          label: 'Grok home directory',
        },
        { path: path.dirname(rendered.document.filePath), label: HOOKS_DIRECTORY_LABEL },
      ]
      : [],
    changes: change ? [change] : [],
  };
}

export async function commitGrokUninstall(
  prepared: PreparedManagedTransaction,
  renameFile?: RenameFile,
): Promise<void> {
  await commitManagedTransaction(prepared, renameFile);
}
