import crypto from 'node:crypto';

/**
 * Generate a UUIDv7 (time-ordered UUID per RFC 9562).
 *
 * Layout (128 bits):
 *   48-bit Unix timestamp (ms) | 4-bit version (7) | 12-bit rand_a
 *   2-bit variant (10) | 62-bit rand_b
 */
export function uuidv7(): string {
  const now = Date.now();
  const random = crypto.randomBytes(10);

  // Bytes 0-5: 48-bit timestamp (ms)
  const bytes = Buffer.alloc(16);
  bytes[0] = (now / 2 ** 40) & 0xff;
  bytes[1] = (now / 2 ** 32) & 0xff;
  bytes[2] = (now / 2 ** 24) & 0xff;
  bytes[3] = (now / 2 ** 16) & 0xff;
  bytes[4] = (now / 2 ** 8) & 0xff;
  bytes[5] = now & 0xff;

  // Bytes 6-7: version (0111) + 12 bits random
  bytes[6] = 0x70 | (random[0]! & 0x0f);
  bytes[7] = random[1]!;

  // Bytes 8-15: variant (10) + 62 bits random
  bytes[8] = 0x80 | (random[2]! & 0x3f);
  bytes[9] = random[3]!;
  bytes[10] = random[4]!;
  bytes[11] = random[5]!;
  bytes[12] = random[6]!;
  bytes[13] = random[7]!;
  bytes[14] = random[8]!;
  bytes[15] = random[9]!;

  const hex = bytes.toString('hex');
  return (
    hex.slice(0, 8) + '-' +
    hex.slice(8, 12) + '-' +
    hex.slice(12, 16) + '-' +
    hex.slice(16, 20) + '-' +
    hex.slice(20, 32)
  );
}

/**
 * Generate a random nonce: 16 random bytes, base64url encoded.
 */
export function generateNonce(): string {
  const bytes = crypto.randomBytes(16);
  return base64urlEncode(bytes);
}

/**
 * Encode a Buffer or Uint8Array to base64url (no padding, RFC 4648 section 5).
 */
export function base64urlEncode(data: Buffer | Uint8Array): string {
  const buf = Buffer.isBuffer(data) ? data : Buffer.from(data);
  return buf.toString('base64url');
}

/**
 * Decode a base64url string (no padding) to a Buffer.
 */
export function base64urlDecode(input: string): Buffer {
  return Buffer.from(input, 'base64url');
}
