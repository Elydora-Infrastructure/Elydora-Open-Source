import fsp from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {
  createLettaDocument,
  lettaDocumentLabel,
  lettaSourceLabel,
  parseLettaDocument,
  type LettaDocument,
  type LettaDocumentKind,
} from './letta-config.js';
import {
  inspectPhysicalDirectory,
  readPhysicalFile,
  type FileSnapshot,
} from './managed-files.js';

const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

export interface LettaSourcePrecondition {
  readonly filePath: string;
  readonly label: string;
  readonly maximumBytes: number;
  readonly original?: FileSnapshot;
}

export interface LettaDisableControl {
  readonly disabled: boolean;
  readonly source?: LettaDocument;
}

export interface LettaSources {
  readonly homeDirectory: string;
  readonly global: LettaDocument;
  readonly project: LettaDocument;
  readonly projectLocal: LettaDocument;
  readonly projectActive: boolean;
  readonly disableControl: LettaDisableControl;
  readonly preconditions: readonly LettaSourcePrecondition[];
}

function comparisonPath(filePath: string): string {
  const resolved = path.resolve(filePath);
  return process.platform === 'win32' ? resolved.toLowerCase() : resolved;
}

function hasCode(error: unknown, code: string): boolean {
  return typeof error === 'object' && error !== null && 'code' in error && error.code === code;
}

async function canonicalPath(filePath: string): Promise<string> {
  try {
    return await fsp.realpath(filePath);
  } catch (error) {
    if (hasCode(error, 'ENOENT') || hasCode(error, 'ENOTDIR')) return path.resolve(filePath);
    throw new Error(`Resolve Letta Code path at ${filePath}: ${
      error instanceof Error ? error.message : String(error)
    }`, { cause: error instanceof Error ? error : new Error(String(error)) });
  }
}

function resolveHomeDirectory(): string {
  return path.resolve(process.env.HOME || os.homedir());
}

async function readDocument(
  kind: LettaDocumentKind,
  filePath: string,
): Promise<LettaDocument> {
  const snapshot = await readPhysicalFile(filePath, lettaSourceLabel(kind), MAX_SOURCE_BYTES);
  return snapshot
    ? parseLettaDocument({
      kind,
      exists: true,
      filePath,
      raw: snapshot.contents,
      snapshot,
    })
    : createLettaDocument(kind, filePath);
}

function sourcePrecondition(document: LettaDocument): LettaSourcePrecondition {
  return {
    filePath: document.filePath,
    label: lettaDocumentLabel(document),
    maximumBytes: MAX_SOURCE_BYTES,
    original: document.snapshot,
  };
}

function deduplicatePreconditions(
  values: readonly LettaSourcePrecondition[],
): LettaSourcePrecondition[] {
  const result = new Map<string, LettaSourcePrecondition>();
  for (const value of values) {
    const key = comparisonPath(value.filePath);
    if (!result.has(key)) result.set(key, value);
  }
  return [...result.values()];
}

function effectiveDisable(
  global: LettaDocument,
  project: LettaDocument,
  projectLocal: LettaDocument,
  projectActive: boolean,
): LettaDisableControl {
  if (global.hooks.disabled === false) return { disabled: false, source: global };
  if (global.hooks.disabled === true) return { disabled: true, source: global };
  if (projectActive && project.hooks.disabled === true) {
    return { disabled: true, source: project };
  }
  if (projectLocal.hooks.disabled === true) {
    return { disabled: true, source: projectLocal };
  }
  return { disabled: false };
}

export async function readLettaSources(): Promise<LettaSources> {
  const homeDirectory = resolveHomeDirectory();
  const workspace = process.cwd();
  const globalDirectory = path.join(homeDirectory, '.letta');
  const projectDirectory = path.join(workspace, '.letta');
  await inspectPhysicalDirectory(homeDirectory, 'Letta Code home directory');
  await inspectPhysicalDirectory(globalDirectory, 'Letta Code global configuration directory');
  if (comparisonPath(projectDirectory) !== comparisonPath(globalDirectory)) {
    await inspectPhysicalDirectory(projectDirectory, 'Letta Code project configuration directory');
  }
  const globalPath = path.join(globalDirectory, 'settings.json');
  const projectPath = path.join(projectDirectory, 'settings.json');
  const projectLocalPath = path.join(projectDirectory, 'settings.local.json');
  const [canonicalWorkspace, canonicalHome] = await Promise.all([
    canonicalPath(workspace),
    canonicalPath(homeDirectory),
  ]);
  const projectActive = comparisonPath(canonicalWorkspace) !== comparisonPath(canonicalHome);
  const global = await readDocument('global', globalPath);
  const project = projectActive
    ? await readDocument('project', projectPath)
    : createLettaDocument('project', projectPath);
  const projectLocal = await readDocument('project-local', projectLocalPath);
  const preconditions = deduplicatePreconditions([
    sourcePrecondition(global),
    ...(projectActive ? [sourcePrecondition(project)] : []),
    sourcePrecondition(projectLocal),
  ]);
  return {
    homeDirectory,
    global,
    project,
    projectLocal,
    projectActive,
    disableControl: effectiveDisable(global, project, projectLocal, projectActive),
    preconditions,
  };
}

export function requireLettaHooksEnabled(sources: LettaSources): void {
  if (!sources.disableControl.disabled) return;
  const source = sources.disableControl.source;
  const location = source
    ? `${lettaDocumentLabel(source)} at ${source.filePath}`
    : 'effective settings';
  throw new Error(`Letta Code hooks are disabled by hooks.disabled in ${location}`);
}
