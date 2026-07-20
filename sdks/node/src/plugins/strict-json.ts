import {
  parse,
  parseTree,
  printParseErrorCode,
  type Node,
  type ParseError,
} from 'jsonc-parser';

export type JsonObject = Record<string, unknown>;

interface JsonObjectParseOptions {
  readonly allowTrailingComma: boolean;
  readonly disallowComments: boolean;
}

export function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
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

function parseJsonObject(
  raw: string,
  label: string,
  options: JsonObjectParseOptions,
): JsonObject {
  const errors: ParseError[] = [];
  const value: unknown = parse(raw, errors, options);
  if (errors.length > 0) {
    const details = errors
      .map((error) => `${printParseErrorCode(error.error)} at offset ${error.offset}`)
      .join(', ');
    throw new Error(`Failed to parse ${label}: ${details}`);
  }
  if (!isObject(value)) throw new Error(`${label} must contain a JSON object`);
  rejectDuplicateKeys(parseTree(raw, [], options), label);
  return value;
}

export function parseStrictJsonObject(raw: string, label: string): JsonObject {
  return parseJsonObject(raw, label, {
    allowTrailingComma: false,
    disallowComments: true,
  });
}

export function parseStrictJsoncObject(raw: string, label: string): JsonObject {
  return parseJsonObject(raw, label, {
    allowTrailingComma: true,
    disallowComments: false,
  });
}
