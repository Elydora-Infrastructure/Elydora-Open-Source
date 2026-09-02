import {
  applyEdits,
  modify,
  type FormattingOptions,
  type JSONPath,
} from 'jsonc-parser';
import type { FileSnapshot } from './managed-files.js';
import {
  MANAGED_EVENTS,
  managedLettaRemovals,
  readLettaHooks,
  type LettaGroup,
  type LettaHooks,
  type ManagedLettaEvent,
} from './letta-contract.js';
import { parseStrictJsonObject, type JsonObject } from './strict-json.js';

export type LettaDocumentKind = 'global' | 'project' | 'project-local';

export interface LettaDocument {
  readonly kind: LettaDocumentKind;
  readonly filePath: string;
  readonly exists: boolean;
  readonly raw: string;
  readonly snapshot?: FileSnapshot;
  readonly root: JsonObject;
  readonly hooks: LettaHooks;
  readonly hasHooksContainer: boolean;
}

export interface RenderedLettaDocument {
  readonly document: LettaDocument;
  readonly changed: boolean;
  readonly next?: string;
}

interface DocumentOptions {
  readonly kind: LettaDocumentKind;
  readonly exists: boolean;
  readonly filePath: string;
  readonly raw: string;
  readonly snapshot?: FileSnapshot;
}

export function lettaSourceLabel(kind: LettaDocumentKind): string {
  switch (kind) {
    case 'project': return 'Letta Code project settings';
    case 'project-local': return 'Letta Code project-local settings';
    default: return 'Letta Code global settings';
  }
}

export function parseLettaDocument(options: DocumentOptions): LettaDocument {
  const label = `${lettaSourceLabel(options.kind)} at ${options.filePath}`;
  const root = parseStrictJsonObject(options.raw, label);
  return {
    ...options,
    root,
    hooks: readLettaHooks(root.hooks),
    hasHooksContainer: Object.hasOwn(root, 'hooks'),
  };
}

export function createLettaDocument(
  kind: LettaDocumentKind,
  filePath: string,
): LettaDocument {
  return parseLettaDocument({
    kind,
    exists: false,
    filePath,
    raw: '{}\n',
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

function currentDocument(document: LettaDocument, raw: string): LettaDocument {
  return parseLettaDocument({
    kind: document.kind,
    exists: document.exists,
    filePath: document.filePath,
    raw,
    snapshot: document.snapshot,
  });
}

function groupsForEvent(hooks: LettaHooks, event: ManagedLettaEvent): LettaGroup[] {
  const groups = hooks[event];
  return Array.isArray(groups) ? groups : [];
}

function removeManagedEntries(
  document: LettaDocument,
  initial: string,
  agentId?: string,
): string {
  let raw = initial;
  const removals = managedLettaRemovals(document.hooks, agentId);
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
      if (groupsForEvent(current.hooks, event).length === 0) {
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
  document: LettaDocument,
  raw: string,
  event: ManagedLettaEvent,
  group: LettaGroup,
): string {
  const current = currentDocument(document, raw);
  const groups = groupsForEvent(current.hooks, event);
  if (groups.length > 0) {
    return change(raw, ['hooks', event, groups.length], group, true);
  }
  return change(raw, ['hooks', event], [group]);
}

export function renderLettaDocument(
  document: LettaDocument,
  agentId: string | undefined,
  additions: ReadonlyMap<ManagedLettaEvent, LettaGroup>,
): RenderedLettaDocument {
  let raw = removeManagedEntries(document, document.raw, agentId);
  for (const event of MANAGED_EVENTS) {
    const group = additions.get(event);
    if (group) raw = appendGroup(document, raw, event, group);
  }
  const current = currentDocument(document, raw);
  if (additions.size === 0 && Object.keys(current.root).length === 0) {
    return { document, changed: true, next: undefined };
  }
  return { document, changed: raw !== document.raw, next: raw };
}

export function lettaDocumentLabel(document: LettaDocument): string {
  return lettaSourceLabel(document.kind);
}
