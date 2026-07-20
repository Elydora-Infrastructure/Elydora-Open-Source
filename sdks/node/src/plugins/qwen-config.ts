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
import type { FileSnapshot } from './managed-files.js';
import {
  MANAGED_EVENTS,
  managedQwenRemovals,
  readQwenHooks,
  type JsonObject,
  type ManagedQwenEvent,
  type QwenGroup,
  type QwenHooks,
  isObject,
} from './qwen-contract.js';

export const QWEN_OWNED_FILE_MARKER = '// Managed by Elydora';

export type QwenDocumentKind = 'system-defaults' | 'user' | 'workspace' | 'system';

export interface QwenDocument {
  readonly kind: QwenDocumentKind;
  readonly filePath: string;
  readonly exists: boolean;
  readonly raw: string;
  readonly snapshot?: FileSnapshot;
  readonly root: JsonObject;
  readonly hooks: QwenHooks;
  readonly hasHooksContainer: boolean;
  readonly disableAllHooks?: boolean;
  readonly folderTrustEnabled?: boolean;
  readonly ownedFile: boolean;
}

export interface RenderedQwenDocument {
  readonly document: QwenDocument;
  readonly changed: boolean;
  readonly next?: string;
}

interface DocumentOptions {
  readonly kind: QwenDocumentKind;
  readonly exists: boolean;
  readonly filePath: string;
  readonly raw: string;
  readonly snapshot?: FileSnapshot;
}

function rejectDuplicateKeys(
  node: Node | undefined,
  label: string,
  location: string[] = [],
): void {
  if (!node) return;
  if (node.type === 'object') {
    const keys = new Set<string>();
    for (const property of node.children ?? []) {
      const key = String(property.children?.[0]?.value);
      const childLocation = [...location, key];
      if (keys.has(key)) {
        throw new Error(`${label} contains duplicate field "${childLocation.join('.')}"`);
      }
      keys.add(key);
      rejectDuplicateKeys(property.children?.[1], label, childLocation);
    }
    return;
  }
  if (node.type === 'array') {
    for (const child of node.children ?? []) rejectDuplicateKeys(child, label, location);
  }
}

export function parseQwenJsoncObject(raw: string, label: string): JsonObject {
  const errors: ParseError[] = [];
  const value: unknown = parse(raw, errors, {
    allowTrailingComma: false,
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
    allowTrailingComma: false,
    disallowComments: false,
  }), label);
  return value;
}

export function qwenSourceLabel(kind: QwenDocumentKind): string {
  switch (kind) {
    case 'system-defaults': return 'Qwen Code system defaults';
    case 'workspace': return 'Qwen Code workspace settings';
    case 'system': return 'Qwen Code system override settings';
    default: return 'Qwen Code user settings';
  }
}

function readFolderTrustEnabled(root: JsonObject, label: string): boolean | undefined {
  if (root.security === undefined) return undefined;
  if (!isObject(root.security)) throw new Error(`${label} field "security" must be an object`);
  if (root.security.folderTrust === undefined) return undefined;
  if (!isObject(root.security.folderTrust)) {
    throw new Error(`${label} field "security.folderTrust" must be an object`);
  }
  const enabled = root.security.folderTrust.enabled;
  if (enabled !== undefined && typeof enabled !== 'boolean') {
    throw new Error(`${label} field "security.folderTrust.enabled" must be a boolean`);
  }
  return enabled as boolean | undefined;
}

export function parseQwenDocument(options: DocumentOptions): QwenDocument {
  const label = `${qwenSourceLabel(options.kind)} at ${options.filePath}`;
  const root = parseQwenJsoncObject(options.raw, label);
  if (root.disableAllHooks !== undefined && typeof root.disableAllHooks !== 'boolean') {
    throw new Error(`${label} field "disableAllHooks" must be a boolean`);
  }
  const hasHooksContainer = Object.hasOwn(root, 'hooks');
  return {
    ...options,
    root,
    hooks: readQwenHooks(root.hooks),
    hasHooksContainer,
    disableAllHooks: root.disableAllHooks as boolean | undefined,
    folderTrustEnabled: readFolderTrustEnabled(root, label),
    ownedFile: options.kind === 'user' && options.raw.startsWith(QWEN_OWNED_FILE_MARKER),
  };
}

export function createQwenDocument(
  kind: QwenDocumentKind,
  filePath: string,
): QwenDocument {
  return parseQwenDocument({
    kind,
    exists: false,
    filePath,
    raw: kind === 'user' ? `${QWEN_OWNED_FILE_MARKER}\n{}\n` : '{}\n',
  });
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
  jsonPath: JSONPath,
  value: unknown,
  isArrayInsertion = false,
): string {
  return applyEdits(raw, modify(raw, jsonPath, value, {
    formattingOptions: formatting(raw),
    isArrayInsertion,
  }));
}

function currentDocument(document: QwenDocument, raw: string): QwenDocument {
  return parseQwenDocument({
    kind: document.kind,
    exists: document.exists,
    filePath: document.filePath,
    raw,
    snapshot: document.snapshot,
  });
}

function removeManagedEntries(
  document: QwenDocument,
  raw: string,
  agentId?: string,
): string {
  const removals = managedQwenRemovals(document.hooks, agentId);
  for (const event of MANAGED_EVENTS) {
    const eventRemovals = removals
      .filter((removal) => removal.event === event)
      .sort((left, right) => right.groupIndex - left.groupIndex);
    for (const removal of eventRemovals) {
      const groupPath: JSONPath = ['hooks', event, removal.groupIndex];
      if (removal.removeGroup) {
        raw = change(raw, groupPath, undefined);
        continue;
      }
      for (const handlerIndex of [...removal.handlerIndexes].sort(
        (left, right) => right - left,
      )) {
        raw = change(raw, [...groupPath, 'hooks', handlerIndex], undefined);
      }
    }
    if (eventRemovals.length > 0) {
      const current = currentDocument(document, raw);
      if ((current.hooks[event] ?? []).length === 0) {
        raw = change(raw, ['hooks', event], undefined);
      }
    }
  }
  const current = currentDocument(document, raw);
  if (current.hasHooksContainer && Object.keys(current.hooks).length === 0) {
    raw = change(raw, ['hooks'], undefined);
  }
  return raw;
}

function appendGroup(
  document: QwenDocument,
  raw: string,
  event: ManagedQwenEvent,
  group: QwenGroup,
): string {
  const current = currentDocument(document, raw);
  if (current.hooks[event]) {
    return change(raw, ['hooks', event, current.hooks[event].length], group, true);
  }
  return change(raw, ['hooks', event], [group]);
}

export function renderQwenDocument(
  document: QwenDocument,
  agentId: string | undefined,
  additions: ReadonlyMap<ManagedQwenEvent, QwenGroup>,
): RenderedQwenDocument {
  let raw = removeManagedEntries(document, document.raw, agentId);
  for (const event of MANAGED_EVENTS) {
    const group = additions.get(event);
    if (group) raw = appendGroup(document, raw, event, group);
  }
  const current = currentDocument(document, raw);
  if (additions.size === 0 && document.ownedFile && Object.keys(current.root).length === 0) {
    return { document, changed: true, next: undefined };
  }
  return { document, changed: raw !== document.raw, next: raw };
}

export function qwenDocumentLabel(document: QwenDocument): string {
  return qwenSourceLabel(document.kind);
}
