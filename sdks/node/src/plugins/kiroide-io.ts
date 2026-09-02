import path from 'node:path';
import {
  createKiroIdeDocument,
  legacyKiroIdeRuntimeContract,
  parseKiroIdeDocument,
  resolveKiroIdePaths,
  type KiroIdeDocument,
  type KiroIdePaths,
  type KiroIdeRuntimeContract,
} from './kiroide-contract.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';

export interface LegacyKiroIdeDocument {
  readonly exists: boolean;
  readonly filePath: string;
  readonly raw?: string;
  readonly contract?: KiroIdeRuntimeContract;
}

export interface KiroIdeSources {
  readonly paths: KiroIdePaths;
  readonly document: KiroIdeDocument;
  readonly legacy: LegacyKiroIdeDocument;
}

async function inspectWorkspace(paths: KiroIdePaths): Promise<void> {
  if (!await inspectPhysicalDirectory(paths.workspaceRoot, 'Kiro IDE workspace')) {
    throw new Error(`Kiro IDE workspace is missing: ${paths.workspaceRoot}`);
  }
  await inspectPhysicalDirectory(paths.kiroDirectory, 'Kiro IDE configuration directory');
  await inspectPhysicalDirectory(paths.hooksDirectory, 'Kiro IDE hooks directory');
}

async function readDocument(paths: KiroIdePaths): Promise<KiroIdeDocument> {
  await inspectWorkspace(paths);
  const snapshot = await readPhysicalFile(paths.configPath, 'Kiro IDE hooks');
  return snapshot
    ? parseKiroIdeDocument(paths.configPath, snapshot.contents)
    : createKiroIdeDocument(paths.configPath);
}

async function readLegacyDocument(paths: KiroIdePaths): Promise<LegacyKiroIdeDocument> {
  const snapshot = await readPhysicalFile(paths.legacyConfigPath, 'legacy Kiro IDE hook');
  if (!snapshot) return { exists: false, filePath: paths.legacyConfigPath };
  return {
    exists: true,
    filePath: paths.legacyConfigPath,
    raw: snapshot.contents,
    contract: legacyKiroIdeRuntimeContract(snapshot.contents, paths.legacyConfigPath),
  };
}

export async function readKiroIdeSources(): Promise<KiroIdeSources> {
  const paths = resolveKiroIdePaths();
  const [document, legacy] = await Promise.all([
    readDocument(paths),
    readLegacyDocument(paths),
  ]);
  return { paths, document, legacy };
}

export async function requirePhysicalLegacyDirectory(
  legacy: LegacyKiroIdeDocument,
): Promise<void> {
  if (!legacy.exists) return;
  const hooksDirectory = path.dirname(legacy.filePath);
  const kiroDirectory = path.dirname(hooksDirectory);
  if (!await inspectPhysicalDirectory(kiroDirectory, 'legacy Kiro IDE configuration directory')) {
    throw new Error(`Legacy Kiro IDE configuration directory is missing: ${kiroDirectory}`);
  }
  if (!await inspectPhysicalDirectory(hooksDirectory, 'legacy Kiro IDE hooks directory')) {
    throw new Error(`Legacy Kiro IDE hooks directory is missing: ${hooksDirectory}`);
  }
}
