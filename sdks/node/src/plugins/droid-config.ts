import {
  applyEdits,
  modify,
  parse,
  parseTree,
  printParseErrorCode,
  type FormattingOptions,
  type JSONPath,
  type Node,
  type ParseError,
} from 'jsonc-parser';
import {
  TOOL_EVENTS,
  type DroidGroup,
  type DroidHookSettings,
  type JsonObject,
  type ToolEvent,
  hasOwn,
  isObject,
  managedRemovals,
  readHookSettings,
} from './droid-contract.js';

export const OWNED_FILE_MARKER = '// Managed by Elydora';

export type DroidDocumentKind = 'hooks' | 'legacy' | 'settings';

export interface DroidDocument {
  readonly kind: DroidDocumentKind;
  readonly filePath: string;
  readonly exists: boolean;
  readonly raw: string;
  readonly root: JsonObject;
  readonly hooks: DroidHookSettings;
  readonly basePath: JSONPath;
  readonly hasHooksContainer: boolean;
  readonly ownedFile: boolean;
}

export interface DroidSources {
  readonly rootPath: string;
  readonly primary?: DroidDocument;
  readonly settings: DroidDocument;
}

export interface InstallationTargets {
  readonly targets: ReadonlyMap<ToolEvent, DroidDocument>;
  readonly createdRoot?: DroidDocument;
}

export interface RenderedDocument {
  readonly document: DroidDocument;
  readonly changed: boolean;
  readonly next?: string;
}

interface DocumentOptions {
  readonly exists: boolean;
  readonly filePath: string;
  readonly kind: DroidDocumentKind;
  readonly raw: string;
}

function rejectDuplicateKeys(node: Node | undefined, label: string, path: string[] = []): void {
  if (!node) return;
  if (node.type === 'object') {
    const keys = new Set<string>();
    for (const property of node.children ?? []) {
      const key = String(property.children?.[0]?.value);
      const location = [...path, key];
      if (keys.has(key)) throw new Error(`${label} contains duplicate field "${location.join('.')}"`);
      keys.add(key);
      rejectDuplicateKeys(property.children?.[1], label, location);
    }
    return;
  }
  if (node.type === 'array') {
    for (const child of node.children ?? []) rejectDuplicateKeys(child, label, path);
  }
}

function parseJsoncObject(raw: string, label: string): JsonObject {
  const errors: ParseError[] = [];
  const value: unknown = parse(raw, errors, {
    allowTrailingComma: true,
    disallowComments: false,
  });
  if (errors.length > 0) {
    const details = errors
      .map((error) => `${printParseErrorCode(error.error)} at offset ${error.offset}`)
      .join(', ');
    throw new Error(`Failed to parse ${label}: ${details}`);
  }
  if (!isObject(value)) throw new Error(`${label} must contain a JSON object`);
  rejectDuplicateKeys(parseTree(raw, [], {
    allowTrailingComma: true,
    disallowComments: false,
  }), label);
  return value;
}

function labelFor(kind: DroidDocumentKind, filePath: string): string {
  if (kind === 'settings') return `Factory Droid settings at ${filePath}`;
  if (kind === 'legacy') return `Factory Droid legacy hooks at ${filePath}`;
  return `Factory Droid hooks at ${filePath}`;
}

export function parseDocument(options: DocumentOptions): DroidDocument {
  const label = labelFor(options.kind, options.filePath);
  const root = parseJsoncObject(options.raw, label);
  if (options.kind === 'settings') {
    const hasHooksContainer = hasOwn(root, 'hooks');
    const hooks = hasHooksContainer
      ? readHookSettings(root.hooks, `${label} field "hooks"`)
      : {};
    return {
      ...options,
      root,
      hooks,
      basePath: ['hooks'],
      hasHooksContainer,
      ownedFile: false,
    };
  }
  return {
    ...options,
    root,
    hooks: readHookSettings(root, label),
    basePath: [],
    hasHooksContainer: true,
    ownedFile: options.raw.startsWith(OWNED_FILE_MARKER),
  };
}

export function createSettingsDocument(filePath: string): DroidDocument {
  return parseDocument({ exists: false, filePath, kind: 'settings', raw: '{}\n' });
}

export function createOwnedHookDocument(filePath: string): DroidDocument {
  return parseDocument({
    exists: false,
    filePath,
    kind: 'hooks',
    raw: `${OWNED_FILE_MARKER}\n{}\n`,
  });
}

function eventPath(document: DroidDocument, event: string): JSONPath {
  return [...document.basePath, event];
}

function formatting(raw: string): FormattingOptions {
  const indentation = /\r?\n([ \t]+)\S/.exec(raw)?.[1];
  const insertSpaces = !indentation?.includes('\t');
  return {
    eol: raw.includes('\r\n') ? '\r\n' : '\n',
    insertSpaces,
    tabSize: insertSpaces ? Math.max(1, indentation?.length ?? 2) : 1,
  };
}

function change(
  raw: string,
  path: JSONPath,
  value: unknown,
  isArrayInsertion = false,
): string {
  return applyEdits(raw, modify(raw, path, value, {
    formattingOptions: formatting(raw),
    isArrayInsertion,
  }));
}

function currentDocument(document: DroidDocument, raw: string): DroidDocument {
  return parseDocument({
    exists: document.exists,
    filePath: document.filePath,
    kind: document.kind,
    raw,
  });
}

function removeManagedEntries(
  document: DroidDocument,
  raw: string,
  agentId?: string,
): string {
  const removals = managedRemovals(document.hooks, agentId);
  for (const event of TOOL_EVENTS) {
    const eventRemovals = removals
      .filter((removal) => removal.event === event)
      .sort((left, right) => right.groupIndex - left.groupIndex);
    if (eventRemovals.length === 0) continue;
    for (const removal of eventRemovals) {
      const groupPath = [...eventPath(document, event), removal.groupIndex];
      if (removal.removeGroup) {
        raw = change(raw, groupPath, undefined);
        continue;
      }
      for (const handlerIndex of [...removal.handlerIndexes].sort((left, right) => right - left)) {
        raw = change(raw, [...groupPath, 'hooks', handlerIndex], undefined);
      }
    }
    const current = currentDocument(document, raw);
    if ((current.hooks[event] ?? []).length === 0) {
      raw = change(raw, eventPath(document, event), undefined);
    }
  }
  return raw;
}

function appendGroup(
  document: DroidDocument,
  raw: string,
  event: ToolEvent,
  group: DroidGroup,
): string {
  const current = currentDocument(document, raw);
  if (hasOwn(current.hooks, event)) {
    const groups = current.hooks[event] ?? [];
    return change(raw, [...eventPath(document, event), groups.length], group, true);
  }
  return change(raw, eventPath(document, event), [group]);
}

function hookFileIsEmpty(document: DroidDocument, raw: string): boolean {
  if (document.kind === 'settings') return false;
  return Object.keys(currentDocument(document, raw).hooks).length === 0;
}

export function renderDocument(
  document: DroidDocument,
  agentId: string | undefined,
  additions: ReadonlyMap<ToolEvent, DroidGroup>,
): RenderedDocument {
  let raw = removeManagedEntries(document, document.raw, agentId);
  for (const event of TOOL_EVENTS) {
    const group = additions.get(event);
    if (group) raw = appendGroup(document, raw, event, group);
  }
  currentDocument(document, raw);
  if (additions.size === 0 && document.ownedFile && hookFileIsEmpty(document, raw)) {
    return { document, changed: true, next: undefined };
  }
  return { document, changed: raw !== document.raw, next: raw };
}

function eventTarget(
  event: ToolEvent,
  sources: DroidSources,
  createdRoot: () => DroidDocument,
): DroidDocument {
  if (sources.primary && hasOwn(sources.primary.hooks, event)) return sources.primary;
  if (sources.settings.hasHooksContainer && hasOwn(sources.settings.hooks, event)) {
    return sources.settings;
  }
  if (sources.primary) return sources.primary;
  if (sources.settings.hasHooksContainer) return sources.settings;
  return createdRoot();
}

export function installationTargets(sources: DroidSources): InstallationTargets {
  let root: DroidDocument | undefined;
  const createdRoot = (): DroidDocument => {
    root ??= createOwnedHookDocument(sources.rootPath);
    return root;
  };
  const targets = new Map<ToolEvent, DroidDocument>();
  for (const event of TOOL_EVENTS) targets.set(event, eventTarget(event, sources, createdRoot));
  return { targets, createdRoot: root };
}

export function additionsFor(
  document: DroidDocument,
  targets: ReadonlyMap<ToolEvent, DroidDocument>,
  groups: ReadonlyMap<ToolEvent, DroidGroup>,
): ReadonlyMap<ToolEvent, DroidGroup> {
  const additions = new Map<ToolEvent, DroidGroup>();
  for (const event of TOOL_EVENTS) {
    if (targets.get(event) === document) additions.set(event, groups.get(event)!);
  }
  return additions;
}
