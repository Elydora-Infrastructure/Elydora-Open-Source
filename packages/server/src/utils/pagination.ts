/**
 * Cursor-based pagination helpers.
 *
 * Cursors are opaque base64url-encoded JSON objects containing the
 * created_at timestamp and primary key of the last item in the current
 * page. This design avoids OFFSET-based pagination which degrades at
 * scale and is susceptible to drift when rows are inserted concurrently.
 */

import { base64urlDecode, base64urlEncode } from './crypto.js';

/** Shape of the decoded cursor payload. */
export interface CursorPayload {
  /** created_at of the last item on the previous page */
  readonly created_at: number;
  /** Primary key of the last item on the previous page */
  readonly id: string;
}

/**
 * Encode a cursor payload into an opaque cursor string.
 *
 * @param payload - The cursor data to encode
 * @returns An opaque base64url-encoded cursor string
 */
export function encodeCursor(payload: CursorPayload): string {
  const json = JSON.stringify(payload);
  const bytes = new TextEncoder().encode(json);
  return base64urlEncode(bytes);
}

/**
 * Decode an opaque cursor string back into its payload.
 *
 * @param cursor - The opaque cursor string
 * @returns The decoded cursor payload, or null if invalid
 */
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
