import crypto from 'node:crypto';
import { base64urlEncode } from './utils.js';

// ---------------------------------------------------------------------------
// JCS Canonicalization (RFC 8785)
// ---------------------------------------------------------------------------

/**
 * Canonicalize a JSON value according to JCS (RFC 8785).
 *
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
// SHA-256
// ---------------------------------------------------------------------------

/**
 * Compute SHA-256 of a UTF-8 string or buffer and return base64url.
 */
export function sha256Base64url(data: string | Buffer): string {
  const input = typeof data === 'string' ? Buffer.from(data, 'utf-8') : data;
  const hash = crypto.createHash('sha256').update(input).digest();
  return base64urlEncode(hash);
}

// ---------------------------------------------------------------------------
// Chain hash computation
// ---------------------------------------------------------------------------

/**
 * Compute the chain hash for an operation.
 *
 * chain_hash = SHA-256(prev_chain_hash + "|" + payload_hash + "|" + operation_id + "|" + issued_at)
 *
 * All inputs concatenated as UTF-8 strings separated by '|'.
 */
export function computeChainHash(
  prevChainHash: string,
  payloadHash: string,
  operationId: string,
  issuedAt: number,
): string {
  const input = `${prevChainHash}|${payloadHash}|${operationId}|${issuedAt}`;
  return sha256Base64url(input);
}

// ---------------------------------------------------------------------------
// Payload hash
// ---------------------------------------------------------------------------

/**
 * Compute SHA-256 hash of JCS-canonicalized payload, returned as base64url.
 */
export function computePayloadHash(
  payload: Record<string, unknown> | string | null,
): string {
  const canonical = jcsCanonicalise(payload);
  return sha256Base64url(canonical);
}

// ---------------------------------------------------------------------------
// Ed25519 signing
// ---------------------------------------------------------------------------

// PKCS8 prefix for wrapping a 32-byte Ed25519 seed
const PKCS8_ED25519_PREFIX = Buffer.from([
  0x30, 0x2e, // SEQUENCE (46 bytes)
  0x02, 0x01, 0x00, // INTEGER 0
  0x30, 0x05, // SEQUENCE (5 bytes)
  0x06, 0x03, 0x2b, 0x65, 0x70, // OID 1.3.101.112 (Ed25519)
  0x04, 0x22, // OCTET STRING (34 bytes)
  0x04, 0x20, // OCTET STRING (32 bytes)
]);

/**
 * Import a base64url-encoded 32-byte Ed25519 seed as a Node.js KeyObject.
 */
function importPrivateKey(privateKeyBase64url: string): crypto.KeyObject {
  const seed = Buffer.from(privateKeyBase64url, 'base64url');
  const pkcs8 = Buffer.concat([PKCS8_ED25519_PREFIX, seed]);
  return crypto.createPrivateKey({
    key: pkcs8,
    format: 'der',
    type: 'pkcs8',
  });
}

/**
 * Sign data with Ed25519 using a base64url-encoded 32-byte seed.
 *
 * @returns base64url-encoded 64-byte signature
 */
export function signEd25519(privateKeyBase64url: string, data: Buffer): string {
  const keyObject = importPrivateKey(privateKeyBase64url);
  const signature = crypto.sign(null, data, keyObject);
  return base64urlEncode(signature);
}

/**
 * Derive the Ed25519 public key from a base64url-encoded 32-byte seed.
 *
 * @returns base64url-encoded 32-byte public key
 */
export function derivePublicKey(privateKeyBase64url: string): string {
  const keyObject = importPrivateKey(privateKeyBase64url);
  const publicKeyObject = crypto.createPublicKey(keyObject);
  const rawPublicKey = publicKeyObject.export({ type: 'spki', format: 'der' });
  // The raw 32-byte public key is the last 32 bytes of the SPKI DER encoding
  const publicKeyBytes = rawPublicKey.subarray(rawPublicKey.length - 32);
  return base64urlEncode(publicKeyBytes);
}

// ---------------------------------------------------------------------------
// Zero chain hash (initial prev_chain_hash for the first operation)
// ---------------------------------------------------------------------------

/**
 * The initial chain hash value: base64url encoding of 32 zero bytes.
 * Must match the backend's GENESIS_CHAIN_HASH constant exactly.
 */
export const ZERO_CHAIN_HASH: string = base64urlEncode(Buffer.alloc(32, 0));
