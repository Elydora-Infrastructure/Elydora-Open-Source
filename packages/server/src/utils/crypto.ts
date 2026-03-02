/**
 * Cryptographic utilities for the Elydora API.
 *
 * All operations use the Web Crypto API (SubtleCrypto) which is available
 * natively in Node.js 20+ with full Ed25519 support.
 */

// ---------------------------------------------------------------------------
// Base64url helpers (RFC 4648 section 5)
// ---------------------------------------------------------------------------

/** Decode a base64url string to a Uint8Array. */
export function base64urlDecode(input: string): Uint8Array {
  // Convert base64url to standard base64
  let base64 = input.replace(/-/g, '+').replace(/_/g, '/');
  // Pad to a multiple of 4
  while (base64.length % 4 !== 0) {
    base64 += '=';
  }
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

/** Encode a Uint8Array to a base64url string (no padding). */
export function base64urlEncode(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]!);
  }
  return btoa(binary)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
}

// ---------------------------------------------------------------------------
// SHA-256
// ---------------------------------------------------------------------------

/** Compute SHA-256 of arbitrary data and return base64url. */
export async function sha256Base64url(data: BufferSource | string): Promise<string> {
  const input = typeof data === 'string' ? new TextEncoder().encode(data) : data;
  const hash = await crypto.subtle.digest('SHA-256', input);
  return base64urlEncode(new Uint8Array(hash));
}

// ---------------------------------------------------------------------------
// Ed25519 signature verification
// ---------------------------------------------------------------------------

/**
 * Import a raw Ed25519 public key (32 bytes) as a CryptoKey.
 *
 * @param publicKeyBytes - 32-byte raw Ed25519 public key
 */
export async function importEd25519PublicKey(publicKeyBytes: Uint8Array): Promise<CryptoKey> {
  return crypto.subtle.importKey(
    'raw',
    publicKeyBytes as unknown as Uint8Array<ArrayBuffer>,
    { name: 'Ed25519' },
    true, // extractable
    ['verify'],
  );
}

/**
 * Verify an Ed25519 signature over data.
 *
 * @param publicKeyBytes - 32-byte raw public key
 * @param signatureBytes - 64-byte Ed25519 signature
 * @param data           - The signed data
 * @returns true if the signature is valid
 */
export async function verifyEd25519Signature(
  publicKeyBytes: Uint8Array,
  signatureBytes: Uint8Array,
  data: Uint8Array,
): Promise<boolean> {
  try {
    const key = await importEd25519PublicKey(publicKeyBytes);
    return crypto.subtle.verify(
      { name: 'Ed25519' },
      key,
      signatureBytes as unknown as Uint8Array<ArrayBuffer>,
      data as unknown as Uint8Array<ArrayBuffer>,
    );
  } catch {
    return false;
  }
}

// ---------------------------------------------------------------------------
// Ed25519 signing (server-side receipt signing)
// ---------------------------------------------------------------------------

/**
 * PKCS8 DER prefix for wrapping a 32-byte Ed25519 seed into a valid PKCS8 key.
 *
 * SEQUENCE {
 *   INTEGER 0
 *   SEQUENCE { OID 1.3.101.112 }
 *   OCTET STRING { OCTET STRING { <32 bytes> } }
 * }
 */
const ED25519_PKCS8_PREFIX = new Uint8Array([
  0x30, 0x2e, // SEQUENCE (46 bytes)
  0x02, 0x01, 0x00, // INTEGER 0
  0x30, 0x05, // SEQUENCE (5 bytes)
  0x06, 0x03, 0x2b, 0x65, 0x70, // OID 1.3.101.112 (Ed25519)
  0x04, 0x22, // OCTET STRING (34 bytes)
  0x04, 0x20, // OCTET STRING (32 bytes)
]);

/** Wrap a 32-byte Ed25519 seed in a PKCS8 envelope. */
function wrapSeedAsPkcs8(seed: Uint8Array): Uint8Array {
  const pkcs8 = new Uint8Array(ED25519_PKCS8_PREFIX.length + seed.length);
  pkcs8.set(ED25519_PKCS8_PREFIX);
  pkcs8.set(seed, ED25519_PKCS8_PREFIX.length);
  return pkcs8;
}

/**
 * Import a raw Ed25519 private key for signing.
 *
 * Node.js expects PKCS8 format for Ed25519 private keys.
 * We wrap the 32-byte seed in a minimal PKCS8 envelope.
 *
 * @param privateKeyBase64url - base64url-encoded 32-byte Ed25519 private key seed
 */
export async function importEd25519PrivateKey(privateKeyBase64url: string): Promise<CryptoKey> {
  const seed = base64urlDecode(privateKeyBase64url);
  const pkcs8 = wrapSeedAsPkcs8(seed);

  return crypto.subtle.importKey(
    'pkcs8',
    pkcs8 as unknown as Uint8Array<ArrayBuffer>,
    { name: 'Ed25519' },
    false,
    ['sign'],
  );
}

/**
 * Sign data with an Ed25519 private key.
 *
 * @param privateKeyBase64url - base64url-encoded 32-byte private key seed
 * @param data - data to sign
 * @returns base64url-encoded 64-byte signature
 */
export async function signEd25519(
  privateKeyBase64url: string,
  data: Uint8Array,
): Promise<string> {
  const key = await importEd25519PrivateKey(privateKeyBase64url);
  const signature = await crypto.subtle.sign(
    { name: 'Ed25519' },
    key,
    data as unknown as Uint8Array<ArrayBuffer>,
  );
  return base64urlEncode(new Uint8Array(signature));
}

/**
 * Export the public key from an Ed25519 private key (for JWKS).
 *
 * @param privateKeyBase64url - base64url-encoded 32-byte private key seed
 * @returns base64url-encoded 32-byte public key
 */
export async function deriveEd25519PublicKey(privateKeyBase64url: string): Promise<string> {
  const seed = base64urlDecode(privateKeyBase64url);
  const pkcs8 = wrapSeedAsPkcs8(seed);

  const key = await crypto.subtle.importKey(
    'pkcs8',
    pkcs8 as unknown as Uint8Array<ArrayBuffer>,
    { name: 'Ed25519' },
    true, // extractable — needed to export the public key
    ['sign'],
  );
  // Export as JWK to get the public 'x' parameter
  const jwk = await crypto.subtle.exportKey('jwk', key) as JsonWebKey;
  // The 'x' field in a JWK for OKP/Ed25519 is the base64url-encoded public key
  return jwk.x ?? '';
}

// ---------------------------------------------------------------------------
// JCS (JSON Canonicalization Scheme - RFC 8785)
// ---------------------------------------------------------------------------

/**
 * Canonicalize a JSON value according to JCS (RFC 8785).
 *
 * JCS specifies:
 * - Object keys sorted lexicographically by UTF-16 code units
 * - No whitespace
 * - Numbers serialized using ES2015 Number serialization
 * - Strings serialized with minimal escaping per JSON spec
 */
export function jcsCanonicalise(value: unknown): string {
  if (value === null || value === undefined) {
    return 'null';
  }

  if (typeof value === 'boolean' || typeof value === 'number') {
    return JSON.stringify(value);
  }

  if (typeof value === 'string') {
    return JSON.stringify(value);
  }

  if (Array.isArray(value)) {
    const elements = value.map((v) => jcsCanonicalise(v));
    return '[' + elements.join(',') + ']';
  }

  if (typeof value === 'object') {
    const obj = value as Record<string, unknown>;
    const keys = Object.keys(obj).sort();
    const pairs: string[] = [];
    for (const key of keys) {
      if (obj[key] !== undefined) {
        pairs.push(JSON.stringify(key) + ':' + jcsCanonicalise(obj[key]));
      }
    }
    return '{' + pairs.join(',') + '}';
  }

  return JSON.stringify(value);
}

// ---------------------------------------------------------------------------
// Chain hash computation
// ---------------------------------------------------------------------------

/**
 * Compute the chain hash for an operation.
 *
 * chain_hash = SHA-256(prev_chain_hash + payload_hash + operation_id + issued_at)
 *
 * All inputs are concatenated as UTF-8 strings separated by '|'.
 */
export async function computeChainHash(
  prevChainHash: string,
  payloadHash: string,
  operationId: string,
  issuedAt: number,
): Promise<string> {
  const input = `${prevChainHash}|${payloadHash}|${operationId}|${issuedAt}`;
  return sha256Base64url(input);
}

// ---------------------------------------------------------------------------
// Receipt hash computation
// ---------------------------------------------------------------------------

/**
 * Compute the hash of an EAR receipt for signing.
 *
 * Hashes the canonicalized receipt fields (excluding the signature fields themselves).
 */
export async function computeReceiptHash(receiptFields: {
  receipt_version: string;
  receipt_id: string;
  operation_id: string;
  org_id: string;
  agent_id: string;
  server_received_at: number;
  seq_no: number;
  chain_hash: string;
  queue_message_id: string;
}): Promise<string> {
  const canonical = jcsCanonicalise(receiptFields);
  return sha256Base64url(canonical);
}
