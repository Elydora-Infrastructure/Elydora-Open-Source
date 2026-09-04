import os from 'node:os';
import path from 'node:path';
import {
  AGENT_KEY,
  CONFIG_FILE,
  type CopilotDocument,
  type CopilotSources,
  type RuntimeContract,
  createDocument,
  parseDocument,
} from './copilot-contract.js';
import { MAX_SOURCE_BYTES } from './common.js';
import {
  inspectPhysicalDirectory,
  readPhysicalFile,
  type FileSnapshot,
} from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';
import {
  parseStrictJsonObject,
  parseStrictJsoncObject,
  type JsonObject,
} from './strict-json.js';

export interface CopilotPaths {
  readonly copilotHome: string;
  readonly userHooksDirectory: string;
  readonly userHookPath: string;
  readonly legacyHookPath: string;
  readonly settingsLayers: readonly SettingsLayer[];
  readonly inspectedDirectories: readonly DirectoryLocation[];
}

interface DirectoryLocation {
  readonly path: string;
  readonly label: string;
}

interface SettingsLayer {
  readonly filePath: string;
  readonly label: string;
  readonly jsonc: boolean;
}

interface ParsedSettingsLayer extends SettingsLayer {
  readonly disableAllHooks?: boolean;
  readonly snapshot?: FileSnapshot;
}

export function resolveCopilotPaths(): CopilotPaths {
  const override = process.env.COPILOT_HOME;
  const copilotHome = override ? override : path.join(os.homedir(), '.copilot');
  const project = process.cwd();
  const github = path.join(project, '.github');
  const githubCopilot = path.join(github, 'copilot');
  const githubHooks = path.join(github, 'hooks');
  const claude = path.join(project, '.claude');
  return {
    copilotHome,
    userHooksDirectory: path.join(copilotHome, 'hooks'),
    userHookPath: path.join(copilotHome, 'hooks', CONFIG_FILE),
    legacyHookPath: path.join(githubHooks, 'hooks.json'),
    settingsLayers: [
      { filePath: path.join(copilotHome, 'config.json'), label: 'legacy Copilot user config', jsonc: false },
      { filePath: path.join(copilotHome, 'settings.json'), label: 'Copilot user settings', jsonc: true },
      { filePath: path.join(claude, 'settings.json'), label: 'Claude repository settings', jsonc: true },
      { filePath: path.join(claude, 'settings.local.json'), label: 'Claude local settings', jsonc: true },
      { filePath: path.join(githubCopilot, 'settings.json'), label: 'Copilot repository settings', jsonc: true },
      { filePath: path.join(githubCopilot, 'settings.local.json'), label: 'Copilot local settings', jsonc: true },
    ],
    inspectedDirectories: [
      { path: project, label: 'Copilot working directory' },
      { path: copilotHome, label: 'COPILOT_HOME' },
      { path: path.join(copilotHome, 'hooks'), label: 'Copilot user hooks directory' },
      { path: github, label: 'GitHub configuration directory' },
      { path: githubHooks, label: 'GitHub repository hooks directory' },
      { path: githubCopilot, label: 'Copilot repository settings directory' },
      { path: claude, label: 'Claude repository settings directory' },
    ],
  };
}

async function inspectDirectories(locations: readonly DirectoryLocation[]): Promise<void> {
  for (const location of locations) {
    await inspectPhysicalDirectory(location.path, location.label);
  }
}

async function readHookDocument(
  filePath: string,
  label: string,
): Promise<CopilotDocument | undefined> {
  const snapshot = await readPhysicalFile(filePath, label, MAX_SOURCE_BYTES);
  return snapshot ? parseDocument(filePath, snapshot, label) : undefined;
}

function parseSettings(raw: string, layer: SettingsLayer): JsonObject {
  if (raw.trim().length === 0) return {};
  const label = `${layer.label} at ${layer.filePath}`;
  return layer.jsonc
    ? parseStrictJsoncObject(raw, label)
    : parseStrictJsonObject(raw, label);
}

async function readSettingsLayer(layer: SettingsLayer): Promise<ParsedSettingsLayer> {
  const snapshot = await readPhysicalFile(layer.filePath, layer.label, MAX_SOURCE_BYTES);
  if (!snapshot) return layer;
  const root = parseSettings(snapshot.contents, layer);
  if (root.disableAllHooks !== undefined && typeof root.disableAllHooks !== 'boolean') {
    throw new Error(
      `${layer.label} at ${layer.filePath} field "disableAllHooks" must be a boolean`,
    );
  }
  return {
    ...layer,
    disableAllHooks: root.disableAllHooks as boolean | undefined,
    snapshot,
  };
}

function effectiveDisabledSource(layers: readonly ParsedSettingsLayer[]): string | undefined {
  let disabledBy: string | undefined;
  for (const layer of layers) {
    if (layer.disableAllHooks === true) disabledBy = `${layer.label} at ${layer.filePath}`;
    else if (layer.disableAllHooks === false) disabledBy = undefined;
  }
  return disabledBy;
}

export async function readSources(): Promise<CopilotSources> {
  const paths = resolveCopilotPaths();
  await inspectDirectories(paths.inspectedDirectories);
  const [user, legacy] = await Promise.all([
    readHookDocument(paths.userHookPath, 'GitHub Copilot user hooks'),
    readHookDocument(paths.legacyHookPath, 'GitHub Copilot legacy project hooks'),
  ]);
  const layers = await Promise.all(paths.settingsLayers.map(readSettingsLayer));
  const userDocument = user ?? createDocument(paths.userHookPath);
  const disabledBy = userDocument.hooksDisabled
    ? `GitHub Copilot user hooks at ${paths.userHookPath}`
    : effectiveDisabledSource(layers);
  return {
    user: userDocument,
    legacy,
    disabledBy,
    settingsPreconditions: layers.map((layer) => ({
      filePath: layer.filePath,
      label: layer.label,
      snapshot: layer.snapshot,
    })),
  };
}

export function requireHooksEnabled(sources: CopilotSources): void {
  if (sources.disabledBy) {
    throw new Error(
      `GitHub Copilot hooks are disabled by ${sources.disabledBy}; set disableAllHooks to false before installation`,
    );
  }
}

export async function runtimeFilesExist(contracts: RuntimeContract[]): Promise<boolean> {
  for (const contract of contracts) {
    if (await managedRuntimeFilesExist(contract, AGENT_KEY)) return true;
  }
  return false;
}
