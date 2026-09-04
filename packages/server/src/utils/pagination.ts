/** Cursor-based pagination helpers. */

import { base64urlDecode, base64urlEncode } from './crypto.js';

/** Shape of the decoded cursor payload. */
export interface CursorPayload {
  /** created_at of the last item on the previous page */
  readonly created_at: number;
  /** Primary key of the last item on the previous page */
  readonly id: string;
}

/** Encode a cursor payload into an opaque cursor string. */
export function encodeCursor(payload: CursorPayload): string {
  const json = JSON.stringify(payload);
  const bytes = new TextEncoder().encode(json);
  return base64urlEncode(bytes);
}

/** Decode an opaque cursor string back into its payload. */
export function decodeCursor(cursor: string): CursorPayload | null {
  try {
    const bytes = base64urlDecode(cursor);
    const json = new TextDecoder().decode(bytes);
    const parsed = JSON.parse(json);

    if (
      typeof parsed !== 'object' ||
      parsed === null ||
      typeof parsed.created_at !== 'number' ||
      typeof parsed.id !== 'string'
    ) {
      return null;
    }

    return { created_at: parsed.created_at, id: parsed.id };
  } catch {
    return null;
  }
}
