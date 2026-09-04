import crypto from 'node:crypto';
import { base64urlEncode } from './utils.js';

// RFC 8785 JSON Canonicalization Scheme
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

export function sha256Base64url(data: string | Buffer): string {
  const input = typeof data === 'string' ? Buffer.from(data, 'utf-8') : data;
  const hash = crypto.createHash('sha256').update(input).digest();
  return base64urlEncode(hash);
}

// chain_hash = SHA-256("prev|payload_hash|operation_id|issued_at")
export function computeChainHash(
  prevChainHash: string,
  payloadHash: string,
  operationId: string,
  issuedAt: number,
): string {
  const input = `${prevChainHash}|${payloadHash}|${operationId}|${issuedAt}`;
  return sha256Base64url(input);
}

export function computePayloadHash(
  payload: Record<string, unknown> | string | null,
): string {
  const canonical = jcsCanonicalise(payload);
  return sha256Base64url(canonical);
}

// PKCS8 envelope: SEQUENCE { INTEGER 0, SEQUENCE { OID 1.3.101.112 }, OCTET STRING { OCTET STRING seed } }
const PKCS8_ED25519_PREFIX = Buffer.from([
  0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20,
]);

function importPrivateKey(privateKeyBase64url: string): crypto.KeyObject {
  const seed = Buffer.from(privateKeyBase64url, 'base64url');
  const pkcs8 = Buffer.concat([PKCS8_ED25519_PREFIX, seed]);
  return crypto.createPrivateKey({
    key: pkcs8,
    format: 'der',
    type: 'pkcs8',
  });
}

export function signEd25519(privateKeyBase64url: string, data: Buffer): string {
  const keyObject = importPrivateKey(privateKeyBase64url);
  const signature = crypto.sign(null, data, keyObject);
  return base64urlEncode(signature);
}

export function derivePublicKey(privateKeyBase64url: string): string {
  const keyObject = importPrivateKey(privateKeyBase64url);
  const publicKeyObject = crypto.createPublicKey(keyObject);
  const rawPublicKey = publicKeyObject.export({ type: 'spki', format: 'der' });
  // The raw key is the last 32 bytes of the SPKI DER encoding.
  const publicKeyBytes = rawPublicKey.subarray(rawPublicKey.length - 32);
  return base64urlEncode(publicKeyBytes);
}

// 32 zero bytes; equals the Backend GENESIS_CHAIN_HASH.
export const ZERO_CHAIN_HASH: string = base64urlEncode(Buffer.alloc(32, 0));
