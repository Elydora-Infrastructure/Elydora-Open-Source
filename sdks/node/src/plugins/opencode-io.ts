import fsp from 'node:fs/promises';
import {
  AGENT_KEY,
  OPENCODE_AUDIT_OPTIONS,
  parseLegacyOpenCodePlugin,
  parseOpenCodePlugin,
  resolveOpenCodePaths,
  type OpenCodeLegacyContract,
  type OpenCodePaths,
  type OpenCodeRuntimeContract,
} from './opencode-contract.js';
import { errorMessage, hasCode, MAX_CONFIG_BYTES } from './common.js';
import { inspectPhysicalDirectory, readPhysicalFile, type FileSnapshot } from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';

export interface OpenCodePluginFile<TContract> {
  readonly exists: boolean;
  readonly filePath: string;
  readonly source?: string;
  readonly snapshot?: FileSnapshot;
  readonly contract?: TContract;
}

export interface OpenCodeSources {
  readonly paths: OpenCodePaths;
  readonly current: OpenCodePluginFile<OpenCodeRuntimeContract>;
  readonly legacy: OpenCodePluginFile<OpenCodeLegacyContract>;
}

async function inspectOpenCodeDirectories(paths: OpenCodePaths): Promise<void> {
  await inspectPhysicalDirectory(paths.homeDirectory, 'OpenCode home directory');
  await inspectPhysicalDirectory(paths.configRoot, 'OpenCode XDG config directory');
  await inspectPhysicalDirectory(paths.configDirectory, 'OpenCode config directory');
  await inspectPhysicalDirectory(paths.pluginsDirectory, 'OpenCode plugins directory');
}

async function readPluginFile<TContract>(
  filePath: string,
  label: string,
  parse: (filePath: string, source: string) => TContract | undefined,
): Promise<OpenCodePluginFile<TContract>> {
  const snapshot = await readPhysicalFile(filePath, label, MAX_CONFIG_BYTES);
  if (!snapshot) return { exists: false, filePath };
  return {
    exists: true,
    filePath,
    source: snapshot.contents,
    snapshot,
    contract: parse(filePath, snapshot.contents),
  };
}

export async function readOpenCodeSources(): Promise<OpenCodeSources> {
  const paths = resolveOpenCodePaths();
  await inspectOpenCodeDirectories(paths);
  const [current, legacy] = await Promise.all([
    readPluginFile(paths.pluginPath, 'OpenCode plugin', parseOpenCodePlugin),
    readPluginFile(
      paths.legacyPluginPath,
      'legacy OpenCode plugin',
      (_filePath, source) => parseLegacyOpenCodePlugin(source),
    ),
  ]);
  return { paths, current, legacy };
}

export function requireAvailableOpenCodePlugin(
  file: OpenCodePluginFile<OpenCodeRuntimeContract>,
): void {
  if (file.exists && !file.contract) {
    throw new Error(`OpenCode plugin path is owned by another integration: ${file.filePath}`);
  }
}

async function executableExists(filePath: string): Promise<boolean> {
  try {
    return (await fsp.stat(filePath)).isFile();
  } catch (error) {
    if (hasCode(error, 'ENOENT') || hasCode(error, 'ENOTDIR')) return false;
    throw new Error(`Inspect OpenCode Node.js runtime at ${filePath}: ${errorMessage(error)}`, {
      cause: error instanceof Error ? error : new Error(String(error)),
    });
  }
}

export async function openCodeRuntimeFilesExist(
  contract: OpenCodeRuntimeContract,
): Promise<boolean> {
  return await executableExists(contract.executablePath)
    && await managedRuntimeFilesExist(contract, AGENT_KEY, {
      auditOptions: OPENCODE_AUDIT_OPTIONS,
    });
}
