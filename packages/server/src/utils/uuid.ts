/** UUIDv7 generator conforming to RFC 9562. */

/** Generate a UUIDv7 string. */
export function generateUUIDv7(): string {
  const now = Date.now();
  const bytes = new Uint8Array(16);

  // Fill with random bytes first
  crypto.getRandomValues(bytes);

  // Encode 48-bit timestamp (big-endian) into bytes 0..5
  const high32 = Math.floor(now / 0x10000); // upper 32 bits of the 48-bit ts
  const low16 = now & 0xffff;               // lower 16 bits of the 48-bit ts

  bytes[0] = (high32 >>> 24) & 0xff;
  bytes[1] = (high32 >>> 16) & 0xff;
  bytes[2] = (high32 >>> 8) & 0xff;
  bytes[3] = high32 & 0xff;
  bytes[4] = (low16 >>> 8) & 0xff;
  bytes[5] = low16 & 0xff;

  // Set version 7 (bits 48-51)
  bytes[6] = (bytes[6]! & 0x0f) | 0x70;

  // Set variant 10xx (bits 64-65)
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;

  return formatUUID(bytes);
}

/** Format a 16-byte array as a standard UUID string (8-4-4-4-12). */
function formatUUID(bytes: Uint8Array): string {
  const hex = Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');

  return (
    hex.slice(0, 8) +
    '-' +
    hex.slice(8, 12) +
    '-' +
    hex.slice(12, 16) +
    '-' +
    hex.slice(16, 20) +
    '-' +
    hex.slice(20, 32)
  );
}
