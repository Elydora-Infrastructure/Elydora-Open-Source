import {
  AGENT_KEY,
  createKiroCliV2Document,
  parseKiroCliV2Document,
  resolveKiroCliPaths,
  type KiroCliPaths,
  type KiroCliV2Document,
} from './kirocli-contract.js';
import {
  createKiroIdeDocument,
  parseKiroIdeDocument,
  type KiroIdeDocument,
  type KiroIdeRuntimeContract,
} from './kiroide-contract.js';
import { MAX_CONFIG_BYTES } from './common.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';

export interface KiroCliSources {
  readonly paths: KiroCliPaths;
  readonly v2: KiroCliV2Document;
  readonly v3: KiroIdeDocument;
}

async function inspectKiroCliDirectories(paths: KiroCliPaths): Promise<void> {
  await inspectPhysicalDirectory(paths.homeDirectory, 'Kiro CLI home directory');
  await inspectPhysicalDirectory(paths.kiroDirectory, 'Kiro CLI configuration directory');
  await Promise.all([
    inspectPhysicalDirectory(paths.agentsDirectory, 'Kiro CLI agents directory'),
    inspectPhysicalDirectory(paths.hooksDirectory, 'Kiro CLI hooks directory'),
  ]);
}

async function readV2Document(paths: KiroCliPaths): Promise<KiroCliV2Document> {
  const snapshot = await readPhysicalFile(
    paths.v2Path,
    'Kiro CLI v2 agent config',
    MAX_CONFIG_BYTES,
  );
  return snapshot
    ? parseKiroCliV2Document(paths.v2Path, snapshot.contents)
    : createKiroCliV2Document(paths.v2Path);
}

async function readV3Document(paths: KiroCliPaths): Promise<KiroIdeDocument> {
  const snapshot = await readPhysicalFile(
    paths.v3Path,
    'Kiro CLI v3 global hooks',
    MAX_CONFIG_BYTES,
  );
  return snapshot
    ? parseKiroIdeDocument(paths.v3Path, snapshot.contents, 'Kiro CLI v3 global hooks')
    : createKiroIdeDocument(paths.v3Path);
}

export async function readKiroCliSources(): Promise<KiroCliSources> {
  const paths = resolveKiroCliPaths();
  await inspectKiroCliDirectories(paths);
  const [v2, v3] = await Promise.all([readV2Document(paths), readV3Document(paths)]);
  return { paths, v2, v3 };
}

export async function kiroCliRuntimeFilesExist(
  contract: KiroIdeRuntimeContract,
): Promise<boolean> {
  return managedRuntimeFilesExist(contract, AGENT_KEY);
}
