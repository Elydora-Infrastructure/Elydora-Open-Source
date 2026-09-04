import type { JSONPath } from 'jsonc-parser';
import {
  MANAGED_EVENTS,
  managedGeminiRemovals,
  readGeminiHookControls,
  readGeminiHooks,
  type GeminiGroup,
  type GeminiHookControls,
  type GeminiHooks,
  type ManagedGeminiEvent,
} from './gemini-contract.js';
import { changeJsonc } from './jsonc-edit.js';
import { parseCommentedJsonObject, type JsonObject } from './strict-json.js';

export const GEMINI_OWNED_FILE_MARKER = '// Managed by Elydora';

export interface GeminiDocument {
  readonly filePath: string;
  readonly exists: boolean;
  readonly raw: string;
  readonly root: JsonObject;
  readonly hooks: GeminiHooks;
  readonly hookControls: GeminiHookControls;
  readonly hasHooksContainer: boolean;
  readonly ownedFile: boolean;
}

export interface RenderedGeminiDocument {
  readonly document: GeminiDocument;
  readonly changed: boolean;
  readonly next?: string;
}

interface DocumentOptions {
  readonly exists: boolean;
  readonly filePath: string;
  readonly raw: string;
}

export function parseGeminiDocument(options: DocumentOptions): GeminiDocument {
  const label = `Gemini CLI user settings at ${options.filePath}`;
  const root = parseCommentedJsonObject(options.raw, label);
  const hasHooksContainer = Object.hasOwn(root, 'hooks');
  return {
    ...options,
    root,
    hooks: readGeminiHooks(root.hooks),
    hookControls: readGeminiHookControls(root.hooksConfig),
    hasHooksContainer,
    ownedFile: options.raw.startsWith(GEMINI_OWNED_FILE_MARKER),
  };
}

export function createGeminiDocument(filePath: string): GeminiDocument {
  return parseGeminiDocument({
    exists: false,
    filePath,
    raw: `${GEMINI_OWNED_FILE_MARKER}\n{}\n`,
  });
}

function currentDocument(document: GeminiDocument, raw: string): GeminiDocument {
  return parseGeminiDocument({
    exists: document.exists,
    filePath: document.filePath,
    raw,
  });
}

function removeManagedEntries(
  document: GeminiDocument,
  raw: string,
  agentId?: string,
): string {
  const removals = managedGeminiRemovals(document.hooks, agentId);
  for (const event of MANAGED_EVENTS) {
    const eventRemovals = removals
      .filter((removal) => removal.event === event)
      .sort((left, right) => right.groupIndex - left.groupIndex);
    for (const removal of eventRemovals) {
      const groupPath: JSONPath = ['hooks', event, removal.groupIndex];
      if (removal.removeGroup) {
        raw = changeJsonc(raw, groupPath, undefined);
        continue;
      }
      for (const handlerIndex of [...removal.handlerIndexes].sort(
        (left, right) => right - left,
      )) {
        raw = changeJsonc(raw, [...groupPath, 'hooks', handlerIndex], undefined);
      }
    }
    if (eventRemovals.length > 0) {
      const current = currentDocument(document, raw);
      if ((current.hooks[event] ?? []).length === 0) {
        raw = changeJsonc(raw, ['hooks', event], undefined);
      }
    }
  }
  const current = currentDocument(document, raw);
  if (current.hasHooksContainer && Object.keys(current.hooks).length === 0) {
    raw = changeJsonc(raw, ['hooks'], undefined);
  }
  return raw;
}

function appendGroup(
  document: GeminiDocument,
  raw: string,
  event: ManagedGeminiEvent,
  group: GeminiGroup,
): string {
  const current = currentDocument(document, raw);
  if (current.hooks[event]) {
    return changeJsonc(raw, ['hooks', event, current.hooks[event].length], group, true);
  }
  return changeJsonc(raw, ['hooks', event], [group]);
}

export function renderGeminiDocument(
  document: GeminiDocument,
  agentId: string | undefined,
  additions: ReadonlyMap<ManagedGeminiEvent, GeminiGroup>,
): RenderedGeminiDocument {
  let raw = removeManagedEntries(document, document.raw, agentId);
  for (const event of MANAGED_EVENTS) {
    const group = additions.get(event);
    if (group) raw = appendGroup(document, raw, event, group);
  }
  const current = currentDocument(document, raw);
  if (additions.size === 0
    && document.ownedFile
    && Object.keys(current.root).length === 0) {
    return { document, changed: true, next: undefined };
  }
  return { document, changed: raw !== document.raw, next: raw };
}
