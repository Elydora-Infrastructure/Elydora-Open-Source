import {
  applyEdits,
  modify,
  type FormattingOptions,
  type JSONPath,
} from 'jsonc-parser';
import type { FileSnapshot } from './managed-files.js';
import type { DroidPolicyState } from './droid-policy.js';
import {
  TOOL_EVENTS,
  type DroidGroup,
  type DroidHookMap,
  type ToolEvent,
  managedRemovals,
  readHookMap,
} from './droid-contract.js';
import {
  isObject,
  parseStrictJsoncObject,
  type JsonObject,
} from './strict-json.js';

export const OWNED_FILE_MARKER = '// Managed by Elydora';

export type DroidDocumentKind = 'hooks' | 'legacy' | 'settings' | 'local-settings';

export interface DroidDocument {
  readonly kind: DroidDocumentKind;
  readonly filePath: string;
  readonly exists: boolean;
  readonly raw: string;
  readonly snapshot?: FileSnapshot;
  readonly root: JsonObject;
  readonly hooks: DroidHookMap;
  readonly basePath: JSONPath;
  readonly hasHooksContainer: boolean;
  readonly hooksDisabled?: boolean;
  readonly showHookOutput?: boolean;
  readonly ownedFile: boolean;
}

export interface DroidSources {
  readonly root: DroidDocument;
  readonly legacy: DroidDocument;
  readonly settings: DroidDocument;
  readonly localSettings: DroidDocument;
  readonly policy: DroidPolicyState;
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
  readonly snapshot?: FileSnapshot;
}

function hasOwn(value: JsonObject, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function labelFor(kind: DroidDocumentKind, filePath: string): string {
  if (kind === 'settings') return `Factory Droid settings at ${filePath}`;
  if (kind === 'local-settings') return `Factory Droid local settings at ${filePath}`;
  if (kind === 'legacy') return `Factory Droid legacy hooks at ${filePath}`;
  return `Factory Droid hooks at ${filePath}`;
}

function optionalBoolean(root: JsonObject, field: string, label: string): boolean | undefined {
  const value = root[field];
  if (value !== undefined && typeof value !== 'boolean') {
    throw new Error(`${label} field "${field}" must be a boolean`);
  }
  return value as boolean | undefined;
}

function legacyDirectHooks(root: JsonObject, label: string): {
  readonly hooks: DroidHookMap;
  readonly hooksDisabled?: boolean;
  readonly showHookOutput?: boolean;
} {
  const hooksDisabled = optionalBoolean(root, 'hooksDisabled', label);
  const showHookOutput = optionalBoolean(root, 'showHookOutput', label);
  const hookEntries = Object.fromEntries(
    Object.entries(root).filter(([key]) => key !== 'hooksDisabled' && key !== 'showHookOutput'),
  );
  return { hooks: readHookMap(hookEntries, label), hooksDisabled, showHookOutput };
}

export function parseDocument(options: DocumentOptions): DroidDocument {
  const label = labelFor(options.kind, options.filePath);
  const root = parseStrictJsoncObject(options.raw, label);
  if (options.kind === 'settings' || options.kind === 'local-settings') {
    const hasHooksContainer = hasOwn(root, 'hooks');
    const hooks = hasHooksContainer
      ? readHookMap(root.hooks, `${label} field "hooks"`)
      : {};
    return {
      ...options,
      root,
      hooks,
      basePath: ['hooks'],
      hasHooksContainer,
      hooksDisabled: optionalBoolean(root, 'hooksDisabled', label),
      showHookOutput: optionalBoolean(root, 'showHookOutput', label),
      ownedFile: false,
    };
  }
  if (hasOwn(root, 'hooks')) {
    if (!isObject(root.hooks)) throw new Error(`${label} field "hooks" must be an object`);
    return {
      ...options,
      root,
      hooks: readHookMap(root.hooks, `${label} field "hooks"`),
      basePath: ['hooks'],
      hasHooksContainer: true,
      ownedFile: options.raw.startsWith(OWNED_FILE_MARKER),
    };
  }
  const legacy = legacyDirectHooks(root, label);
  return {
    ...options,
    root,
    ...legacy,
    basePath: [],
    hasHooksContainer: false,
    ownedFile: options.raw.startsWith(OWNED_FILE_MARKER),
  };
}

export function createSettingsDocument(
  filePath: string,
  kind: 'settings' | 'local-settings' = 'settings',
): DroidDocument {
  return parseDocument({ exists: false, filePath, kind, raw: '{}\n' });
}

export function createLegacyHookDocument(filePath: string): DroidDocument {
  return parseDocument({ exists: false, filePath, kind: 'legacy', raw: '{}\n' });
}

export function createOwnedHookDocument(filePath: string): DroidDocument {
  return parseDocument({
    exists: false,
    filePath,
    kind: 'hooks',
    raw: `${OWNED_FILE_MARKER}\n{\n  "hooks": {}\n}\n`,
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

function change(raw: string, path: JSONPath, value: unknown, isArrayInsertion = false): string {
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
    snapshot: document.snapshot,
  });
}

function removeManagedEntries(document: DroidDocument, raw: string, agentId?: string): string {
  const removals = managedRemovals(document.hooks, agentId);
  for (const event of TOOL_EVENTS) {
    const eventRemovals = removals
      .filter((removal) => removal.event === event)
      .sort((left, right) => right.groupIndex - left.groupIndex);
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
    if (eventRemovals.length > 0) {
      const current = currentDocument(document, raw);
      if ((current.hooks[event] ?? []).length === 0) {
        raw = change(raw, eventPath(document, event), undefined);
      }
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
  if (Object.prototype.hasOwnProperty.call(current.hooks, event)) {
    const groups = current.hooks[event] ?? [];
    return change(raw, [...eventPath(document, event), groups.length], group, true);
  }
  return change(raw, eventPath(document, event), [group]);
}

function hookFileIsEmpty(document: DroidDocument, raw: string): boolean {
  if (document.kind === 'settings' || document.kind === 'local-settings') return false;
  const current = currentDocument(document, raw);
  const remainingRootFields = Object.keys(current.root).filter((key) => key !== 'hooks');
  return Object.keys(current.hooks).length === 0 && remainingRootFields.length === 0;
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
  if (additions.size === 0
    && document.exists
    && document.ownedFile
    && hookFileIsEmpty(document, raw)) {
    return { document, changed: true, next: undefined };
  }
  return { document, changed: raw !== document.raw, next: raw };
}

export function activeDocument(sources: DroidSources): DroidDocument {
  if (sources.root.exists) return sources.root;
  if (sources.legacy.exists) return sources.legacy;
  if (sources.localSettings.hasHooksContainer) return sources.localSettings;
  if (sources.settings.hasHooksContainer) return sources.settings;
  return sources.root;
}

export function effectiveHooks(sources: DroidSources): DroidHookMap {
  return activeDocument(sources).hooks;
}

export interface DroidHookBlock {
  readonly field: 'allowManagedHooksOnly' | 'hooksDisabled';
  readonly filePath: string;
  readonly label: string;
}

export function hookBlock(sources: DroidSources): DroidHookBlock | undefined {
  if (sources.policy.allowManagedHooksOnlyBy) {
    return {
      field: 'allowManagedHooksOnly',
      ...sources.policy.allowManagedHooksOnlyBy,
    };
  }
  if (sources.policy.hooksDisabled !== undefined) {
    return sources.policy.hooksDisabled ? {
      field: 'hooksDisabled',
      ...sources.policy.hooksDisabledBy!,
    } : undefined;
  }
  const selected = sources.localSettings.hooksDisabled !== undefined
    ? sources.localSettings
    : sources.settings;
  if (selected.hooksDisabled === true) {
    return {
      field: 'hooksDisabled',
      filePath: selected.filePath,
      label: labelFor(selected.kind, selected.filePath),
    };
  }
  const active = activeDocument(sources);
  return active.hooksDisabled === true
    ? {
      field: 'hooksDisabled',
      filePath: active.filePath,
      label: labelFor(active.kind, active.filePath),
    }
    : undefined;
}

export function sourceDocuments(sources: DroidSources): DroidDocument[] {
  return [sources.root, sources.legacy, sources.settings, sources.localSettings];
}

export function installationDocuments(sources: DroidSources): DroidDocument[] {
  const target = activeDocument(sources);
  const documents = [
    sources.root.exists || target === sources.root ? sources.root : undefined,
    sources.legacy.exists ? sources.legacy : undefined,
    sources.settings.hasHooksContainer ? sources.settings : undefined,
    sources.localSettings.hasHooksContainer ? sources.localSettings : undefined,
  ];
  const unique = new Map<string, DroidDocument>();
  for (const document of documents) {
    if (document) unique.set(document.filePath, document);
  }
  return [...unique.values()];
}

export function additionsForTarget(
  document: DroidDocument,
  target: DroidDocument,
  groups: ReadonlyMap<ToolEvent, DroidGroup>,
): ReadonlyMap<ToolEvent, DroidGroup> {
  return document === target ? groups : new Map();
}
