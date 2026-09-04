import os from 'node:os';
import path from 'node:path';
import { MAX_SOURCE_BYTES } from './common.js';
import { AGENT_KEY, type RuntimeContract } from './droid-contract.js';
import {
  activeDocument,
  createLegacyHookDocument,
  createOwnedHookDocument,
  createSettingsDocument,
  hookBlock,
  parseDocument,
  type DroidDocument,
  type DroidDocumentKind,
  type DroidSources,
} from './droid-config.js';
import { readDroidPolicy } from './droid-policy.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';

interface FactoryPaths {
  readonly directory: string;
  readonly root: string;
  readonly legacyDirectory: string;
  readonly legacy: string;
  readonly settings: string;
  readonly localSettings: string;
}

function factoryPaths(): FactoryPaths {
  const directory = path.join(os.homedir(), '.factory');
  const legacyDirectory = path.join(directory, 'hooks');
  return {
    directory,
    root: path.join(directory, 'hooks.json'),
    legacyDirectory,
    legacy: path.join(legacyDirectory, 'hooks.json'),
    settings: path.join(directory, 'settings.json'),
    localSettings: path.join(directory, 'settings.local.json'),
  };
}

async function readDocument(
  filePath: string,
  kind: DroidDocumentKind,
  label: string,
): Promise<DroidDocument | undefined> {
  const snapshot = await readPhysicalFile(filePath, label, MAX_SOURCE_BYTES);
  return snapshot ? parseDocument({
    exists: true,
    filePath,
    kind,
    raw: snapshot.contents,
    snapshot,
  }) : undefined;
}

export async function readSources(): Promise<DroidSources> {
  const paths = factoryPaths();
  await inspectPhysicalDirectory(paths.directory, 'Factory Droid user configuration directory');
  await inspectPhysicalDirectory(paths.legacyDirectory, 'Factory Droid legacy hooks directory');
  const [root, legacy, settings, localSettings, policy] = await Promise.all([
    readDocument(paths.root, 'hooks', 'Factory Droid user hooks'),
    readDocument(paths.legacy, 'legacy', 'Factory Droid legacy hooks'),
    readDocument(paths.settings, 'settings', 'Factory Droid user settings'),
    readDocument(paths.localSettings, 'local-settings', 'Factory Droid local settings'),
    readDroidPolicy(),
  ]);
  return {
    root: root ?? createOwnedHookDocument(paths.root),
    legacy: legacy ?? createLegacyHookDocument(paths.legacy),
    settings: settings ?? createSettingsDocument(paths.settings),
    localSettings: localSettings
      ?? createSettingsDocument(paths.localSettings, 'local-settings'),
    policy,
  };
}

export function requireHooksEnabled(sources: DroidSources): void {
  const blocked = hookBlock(sources);
  if (blocked) {
    throw new Error(
      `Factory Droid user hooks are disabled by ${blocked.field} in ${blocked.label} at ${blocked.filePath}`,
    );
  }
}

export async function runtimeFilesExist(contracts: RuntimeContract[]): Promise<boolean> {
  for (const contract of contracts) {
    if (await managedRuntimeFilesExist(contract, AGENT_KEY)) return true;
  }
  return false;
}

export function displayConfigPath(sources: DroidSources): string {
  return activeDocument(sources).filePath;
}
