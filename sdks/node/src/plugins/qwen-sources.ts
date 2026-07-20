import os from 'node:os';
import path from 'node:path';
import fsp from 'node:fs/promises';
import { parse as parseDotenv } from 'dotenv';
import {
  createQwenDocument,
  parseQwenDocument,
  parseQwenJsoncObject,
  qwenDocumentLabel,
  qwenSourceLabel,
  type QwenDocument,
  type QwenDocumentKind,
} from './qwen-config.js';
import { readPhysicalFile, type FileSnapshot } from './managed-files.js';

const MAX_SOURCE_BYTES = 2 * 1024 * 1024;
const HOME_ENV_KEYS = ['QWEN_HOME', 'QWEN_RUNTIME_DIR'] as const;

export interface QwenSourcePrecondition {
  readonly filePath: string;
  readonly label: string;
  readonly maximumBytes: number;
  readonly original?: FileSnapshot;
}

export interface QwenDisableControl {
  readonly disabled: boolean;
  readonly source?: QwenDocument;
}

export interface QwenSources {
  readonly qwenHome: string;
  readonly systemDefaults: QwenDocument;
  readonly user: QwenDocument;
  readonly workspace: QwenDocument;
  readonly system: QwenDocument;
  readonly workspaceActive: boolean;
  readonly workspaceTrusted: boolean;
  readonly disableControl: QwenDisableControl;
  readonly preconditions: readonly QwenSourcePrecondition[];
}

interface RoutingResult {
  readonly qwenHome: string;
  readonly preconditions: readonly QwenSourcePrecondition[];
}

function defaultQwenHome(): string {
  const home = os.homedir();
  return home ? path.join(home, '.qwen') : path.join(os.tmpdir(), '.qwen');
}

function resolveConfigPath(value: string): string {
  let resolved = value;
  if (resolved === '~' || resolved.startsWith('~/') || resolved.startsWith('~\\')) {
    const segments = resolved === '~'
      ? []
      : resolved.slice(2).split(/[/\\]+/).filter(Boolean);
    resolved = path.join(os.homedir(), ...segments);
  }
  return path.isAbsolute(resolved) ? resolved : path.resolve(resolved);
}

function environmentValue(key: string): { own: boolean; value?: string } {
  return {
    own: Object.hasOwn(process.env, key),
    value: process.env[key],
  };
}

async function resolveQwenRouting(): Promise<RoutingResult> {
  const values = new Map<string, string | undefined>();
  const owned = new Set<string>();
  for (const key of HOME_ENV_KEYS) {
    const current = environmentValue(key);
    values.set(key, current.value);
    if (current.own) owned.add(key);
  }
  if (HOME_ENV_KEYS.every((key) => values.get(key))) {
    return {
      qwenHome: resolveConfigPath(values.get('QWEN_HOME')!),
      preconditions: [],
    };
  }

  const initialQwenHome = values.get('QWEN_HOME');
  const initialDirectory = initialQwenHome
    ? resolveConfigPath(initialQwenHome)
    : defaultQwenHome();
  const candidates = [path.join(initialDirectory, '.env')];
  if (!initialQwenHome) candidates.push(path.join(path.dirname(initialDirectory), '.env'));
  const preconditions: QwenSourcePrecondition[] = [];
  const visited = new Set<string>();

  const readCandidate = async (filePath: string): Promise<void> => {
    const resolved = path.resolve(filePath);
    const key = process.platform === 'win32' ? resolved.toLowerCase() : resolved;
    if (visited.has(key)) return;
    visited.add(key);
    const snapshot = await readPhysicalFile(resolved, 'Qwen Code home environment');
    preconditions.push({
      filePath: resolved,
      label: 'Qwen Code home environment',
      maximumBytes: MAX_SOURCE_BYTES,
      original: snapshot,
    });
    if (!snapshot) return;
    const parsed = parseDotenv(snapshot.contents);
    for (const envKey of HOME_ENV_KEYS) {
      const value = parsed[envKey];
      if (value && !owned.has(envKey)) {
        values.set(envKey, value);
        owned.add(envKey);
      }
    }
  };

  for (const candidate of candidates) await readCandidate(candidate);
  const discoveredHome = values.get('QWEN_HOME');
  if (discoveredHome && discoveredHome !== initialQwenHome) {
    const discoveredDirectory = resolveConfigPath(discoveredHome);
    if (discoveredDirectory !== initialDirectory) {
      await readCandidate(path.join(discoveredDirectory, '.env'));
    }
  }
  return {
    qwenHome: values.get('QWEN_HOME')
      ? resolveConfigPath(values.get('QWEN_HOME')!)
      : defaultQwenHome(),
    preconditions,
  };
}

function systemSettingsPath(): string {
  const configured = process.env.QWEN_CODE_SYSTEM_SETTINGS_PATH;
  if (configured) return path.resolve(configured);
  if (process.platform === 'darwin') {
    return '/Library/Application Support/QwenCode/settings.json';
  }
  if (process.platform === 'win32') return 'C:\\ProgramData\\qwen-code\\settings.json';
  return '/etc/qwen-code/settings.json';
}

function systemDefaultsPath(systemPath: string): string {
  const configured = process.env.QWEN_CODE_SYSTEM_DEFAULTS_PATH;
  return configured
    ? path.resolve(configured)
    : path.join(path.dirname(systemPath), 'system-defaults.json');
}

async function readDocument(
  kind: QwenDocumentKind,
  filePath: string,
): Promise<QwenDocument> {
  const snapshot = await readPhysicalFile(filePath, qwenSourceLabel(kind));
  return snapshot
    ? parseQwenDocument({
      kind,
      exists: true,
      filePath,
      raw: snapshot.contents,
      snapshot,
    })
    : createQwenDocument(kind, filePath);
}

async function canonicalPath(filePath: string): Promise<string> {
  try {
    return await fsp.realpath(filePath);
  } catch {
    return path.resolve(filePath);
  }
}

function comparisonPath(filePath: string): string {
  const resolved = path.resolve(filePath);
  return process.platform === 'win32' ? resolved.toLowerCase() : resolved;
}

function isWithin(child: string, parent: string): boolean {
  const relative = path.relative(comparisonPath(parent), comparisonPath(child));
  return relative === '' || (!relative.startsWith('..') && !path.isAbsolute(relative));
}

async function workspaceTrust(
  system: QwenDocument,
  user: QwenDocument,
  qwenHome: string,
  workspacePath: string,
): Promise<{ trusted: boolean; precondition?: QwenSourcePrecondition }> {
  const enabled = user.folderTrustEnabled ?? system.folderTrustEnabled ?? false;
  if (!enabled) return { trusted: true };
  const configured = process.env.QWEN_CODE_TRUSTED_FOLDERS_PATH;
  const filePath = configured
    ? path.resolve(configured)
    : path.join(qwenHome, 'trustedFolders.json');
  const snapshot = await readPhysicalFile(filePath, 'Qwen Code trusted folders');
  const precondition = {
    filePath,
    label: 'Qwen Code trusted folders',
    maximumBytes: MAX_SOURCE_BYTES,
    original: snapshot,
  };
  if (!snapshot) return { trusted: true, precondition };
  const rules = parseQwenJsoncObject(snapshot.contents, `Qwen Code trusted folders at ${filePath}`);
  for (const [rulePath, level] of Object.entries(rules)) {
    if (!['TRUST_FOLDER', 'TRUST_PARENT', 'DO_NOT_TRUST'].includes(String(level))) {
      throw new Error(`Qwen Code trusted folders has invalid trust level for "${rulePath}"`);
    }
  }
  const workspace = await canonicalPath(workspacePath);
  for (const [rulePath, level] of Object.entries(rules)) {
    const canonicalRule = await canonicalPath(String(rulePath));
    const trustRoot = level === 'TRUST_PARENT' ? path.dirname(canonicalRule) : canonicalRule;
    if ((level === 'TRUST_FOLDER' || level === 'TRUST_PARENT')
      && isWithin(workspace, trustRoot)) return { trusted: true, precondition };
  }
  for (const [rulePath, level] of Object.entries(rules)) {
    if (level === 'DO_NOT_TRUST'
      && comparisonPath(workspace) === comparisonPath(await canonicalPath(rulePath))) {
      return { trusted: false, precondition };
    }
  }
  return { trusted: true, precondition };
}

function effectiveDisable(
  systemDefaults: QwenDocument,
  user: QwenDocument,
  workspace: QwenDocument,
  system: QwenDocument,
  useWorkspace: boolean,
): QwenDisableControl {
  let disabled = false;
  let source: QwenDocument | undefined;
  const documents = [systemDefaults, user, ...(useWorkspace ? [workspace] : []), system];
  for (const document of documents) {
    if (document.disableAllHooks === undefined) continue;
    disabled = document.disableAllHooks;
    source = document;
  }
  return { disabled, source };
}

function sourcePrecondition(document: QwenDocument): QwenSourcePrecondition {
  return {
    filePath: document.filePath,
    label: qwenDocumentLabel(document),
    maximumBytes: MAX_SOURCE_BYTES,
    original: document.snapshot,
  };
}

function deduplicatePreconditions(
  values: readonly QwenSourcePrecondition[],
): QwenSourcePrecondition[] {
  const result = new Map<string, QwenSourcePrecondition>();
  for (const value of values) {
    const key = comparisonPath(value.filePath);
    if (!result.has(key)) result.set(key, value);
  }
  return [...result.values()];
}

export async function readQwenSources(): Promise<QwenSources> {
  const routing = await resolveQwenRouting();
  const systemPath = systemSettingsPath();
  const workspacePath = path.join(process.cwd(), '.qwen', 'settings.json');
  const system = await readDocument('system', systemPath);
  const systemDefaults = await readDocument(
    'system-defaults',
    systemDefaultsPath(systemPath),
  );
  const user = await readDocument('user', path.join(routing.qwenHome, 'settings.json'));
  const [canonicalWorkspace, canonicalHome] = await Promise.all([
    canonicalPath(process.cwd()),
    canonicalPath(os.homedir()),
  ]);
  const workspaceActive = comparisonPath(canonicalWorkspace) !== comparisonPath(canonicalHome);
  const workspace = workspaceActive
    ? await readDocument('workspace', workspacePath)
    : createQwenDocument('workspace', workspacePath);
  const trust = workspaceActive
    ? await workspaceTrust(system, user, routing.qwenHome, canonicalWorkspace)
    : { trusted: false as const };
  const useWorkspace = workspaceActive && trust.trusted;
  const preconditions = deduplicatePreconditions([
    ...routing.preconditions,
    sourcePrecondition(systemDefaults),
    sourcePrecondition(user),
    ...(workspaceActive ? [sourcePrecondition(workspace)] : []),
    sourcePrecondition(system),
    ...(trust.precondition ? [trust.precondition] : []),
  ]);
  return {
    qwenHome: routing.qwenHome,
    systemDefaults,
    user,
    workspace,
    system,
    workspaceActive,
    workspaceTrusted: trust.trusted,
    disableControl: effectiveDisable(
      systemDefaults,
      user,
      workspace,
      system,
      useWorkspace,
    ),
    preconditions,
  };
}

export function requireQwenHooksEnabled(sources: QwenSources): void {
  if (!sources.disableControl.disabled) return;
  const source = sources.disableControl.source;
  const location = source ? `${qwenDocumentLabel(source)} at ${source.filePath}` : 'effective settings';
  throw new Error(`Qwen Code hooks are disabled by disableAllHooks in ${location}`);
}
