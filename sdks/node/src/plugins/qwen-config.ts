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
} from "jsonc-parser";
import {
  TOOL_EVENTS,
  type JsonObject,
  type QwenGroup,
  type QwenHookSettings,
  type ToolEvent,
  isObject,
  managedRemovals,
  readHooks,
} from "./qwen-contract.js";

export const OWNED_FILE_MARKER = "// Managed by Elydora";

export interface QwenDocument {
  readonly filePath: string;
  readonly exists: boolean;
  readonly raw: string;
  readonly root: JsonObject;
  readonly hooks: QwenHookSettings;
  readonly hasHooksContainer: boolean;
  readonly hooksDisabled: boolean;
  readonly ownedFile: boolean;
}

export interface RenderedDocument {
  readonly document: QwenDocument;
  readonly changed: boolean;
  readonly next?: string;
}

interface DocumentOptions {
  readonly exists: boolean;
  readonly filePath: string;
  readonly raw: string;
}

function rejectDuplicateKeys(
  node: Node | undefined,
  label: string,
  path: string[] = [],
): void {
  if (!node) return;
  if (node.type === "object") {
    const keys = new Set<string>();
    for (const property of node.children ?? []) {
      const key = String(property.children?.[0]?.value);
      const location = [...path, key];
      if (keys.has(key))
        throw new Error(
          `${label} contains duplicate field "${location.join(".")}"`,
        );
      keys.add(key);
      rejectDuplicateKeys(property.children?.[1], label, location);
    }
    return;
  }
  if (node.type === "array") {
    for (const child of node.children ?? [])
      rejectDuplicateKeys(child, label, path);
  }
}

function parseJsonWithComments(raw: string, label: string): JsonObject {
  const errors: ParseError[] = [];
  const value: unknown = parse(raw, errors, {
    allowTrailingComma: false,
    disallowComments: false,
  });
  if (errors.length > 0) {
    const details = errors
      .map(
        (error) =>
          `${printParseErrorCode(error.error)} at offset ${error.offset}`,
      )
      .join(", ");
    throw new Error(`Failed to parse ${label}: ${details}`);
  }
  if (!isObject(value)) throw new Error(`${label} must contain a JSON object`);
  rejectDuplicateKeys(
    parseTree(raw, [], {
      allowTrailingComma: false,
      disallowComments: false,
    }),
    label,
  );
  return value;
}

export function parseDocument(options: DocumentOptions): QwenDocument {
  const label = `Qwen Code settings at ${options.filePath}`;
  const root = parseJsonWithComments(options.raw, label);
  if (
    root.disableAllHooks !== undefined &&
    typeof root.disableAllHooks !== "boolean"
  ) {
    throw new Error(`${label} field "disableAllHooks" must be a boolean`);
  }
  const hasHooksContainer = Object.hasOwn(root, "hooks");
  const hooks = hasHooksContainer
    ? readHooks(root.hooks, `${label} field "hooks"`)
    : {};
  return {
    ...options,
    root,
    hooks,
    hasHooksContainer,
    hooksDisabled: root.disableAllHooks === true,
    ownedFile: options.raw.startsWith(OWNED_FILE_MARKER),
  };
}

export function createOwnedDocument(filePath: string): QwenDocument {
  return parseDocument({
    exists: false,
    filePath,
    raw: `${OWNED_FILE_MARKER}\n{}\n`,
  });
}

function formatting(raw: string): FormattingOptions {
  const indentation = /\r?\n([ \t]+)\S/.exec(raw)?.[1];
  const insertSpaces = !indentation?.includes("\t");
  return {
    eol: raw.includes("\r\n") ? "\r\n" : "\n",
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
  return applyEdits(
    raw,
    modify(raw, jsonPath, value, {
      formattingOptions: formatting(raw),
      isArrayInsertion,
    }),
  );
}

function currentDocument(document: QwenDocument, raw: string): QwenDocument {
  return parseDocument({
    exists: document.exists,
    filePath: document.filePath,
    raw,
  });
}

function removeManagedEntries(
  document: QwenDocument,
  raw: string,
  agentId?: string,
): string {
  const removals = managedRemovals(document.hooks, agentId);
  for (const event of TOOL_EVENTS) {
    const eventRemovals = removals
      .filter((removal) => removal.event === event)
      .sort((left, right) => right.groupIndex - left.groupIndex);
    for (const removal of eventRemovals) {
      const groupPath: JSONPath = ["hooks", event, removal.groupIndex];
      if (removal.removeGroup) {
        raw = change(raw, groupPath, undefined);
        continue;
      }
      for (const handlerIndex of [...removal.handlerIndexes].sort(
        (left, right) => right - left,
      )) {
        raw = change(raw, [...groupPath, "hooks", handlerIndex], undefined);
      }
    }
    if (eventRemovals.length > 0) {
      const current = currentDocument(document, raw);
      if ((current.hooks[event] ?? []).length === 0) {
        raw = change(raw, ["hooks", event], undefined);
      }
    }
  }
  const current = currentDocument(document, raw);
  if (current.hasHooksContainer && Object.keys(current.hooks).length === 0) {
    raw = change(raw, ["hooks"], undefined);
  }
  return raw;
}

function appendGroup(
  document: QwenDocument,
  raw: string,
  event: ToolEvent,
  group: QwenGroup,
): string {
  const current = currentDocument(document, raw);
  if (current.hooks[event]) {
    return change(
      raw,
      ["hooks", event, current.hooks[event]!.length],
      group,
      true,
    );
  }
  return change(raw, ["hooks", event], [group]);
}

export function renderDocument(
  document: QwenDocument,
  agentId: string | undefined,
  additions: ReadonlyMap<ToolEvent, QwenGroup>,
): RenderedDocument {
  let raw = removeManagedEntries(document, document.raw, agentId);
  for (const event of TOOL_EVENTS) {
    const group = additions.get(event);
    if (group) raw = appendGroup(document, raw, event, group);
  }
  const current = currentDocument(document, raw);
  if (
    additions.size === 0 &&
    document.ownedFile &&
    Object.keys(current.root).length === 0
  ) {
    return { document, changed: true, next: undefined };
  }
  return { document, changed: raw !== document.raw, next: raw };
}
