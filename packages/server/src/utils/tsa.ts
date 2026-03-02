/**
 * RFC 3161 Timestamping Authority (TSA) client utilities.
 *
 * Constructs a minimal DER-encoded TimeStampReq and submits it to a
 * public TSA endpoint. No ASN.1 library required — the 59-byte request
 * is hand-assembled using the same DER-prefix pattern as the PKCS8
 * wrapper in crypto.ts.
 */

/** Default public TSA endpoint (Sectigo / Comodo). */
export const DEFAULT_TSA_URL = 'http://timestamp.sectigo.com';

/**
 * Build a minimal RFC 3161 TimeStampReq (DER-encoded, 59 bytes).
 *
 * Structure:
 *   SEQUENCE {
 *     INTEGER 1                            -- version
 *     SEQUENCE {                           -- messageImprint
 *       SEQUENCE {                         -- hashAlgorithm (SHA-256)
 *         OID 2.16.840.1.101.3.4.2.1
 *         NULL
 *       }
 *       OCTET STRING <32 bytes>            -- hashedMessage
 *     }
 *     BOOLEAN TRUE                         -- certReq
 *   }
 */
export function buildTimeStampReq(sha256Hash: Uint8Array): Uint8Array {
  if (sha256Hash.length !== 32) {
    throw new Error(`Expected 32-byte SHA-256 hash, got ${sha256Hash.length}`);
  }

  // DER prefix (24 bytes) — everything before the 32-byte hash
  const prefix = new Uint8Array([
    0x30, 0x39,                                           // SEQUENCE (57 bytes)
    0x02, 0x01, 0x01,                                     // INTEGER 1 (version v1)
    0x30, 0x31,                                           // SEQUENCE (49 bytes) — MessageImprint
    0x30, 0x0d,                                           // SEQUENCE (13 bytes) — AlgorithmIdentifier
    0x06, 0x09,                                           // OID (9 bytes)
    0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01, // SHA-256 (2.16.840.1.101.3.4.2.1)
    0x05, 0x00,                                           // NULL (parameters)
    0x04, 0x20,                                           // OCTET STRING (32 bytes)
  ]);

  // DER suffix (3 bytes) — certReq = TRUE
  const suffix = new Uint8Array([0x01, 0x01, 0xff]);

  // Assemble: prefix + hash + suffix = 24 + 32 + 3 = 59 bytes
  const req = new Uint8Array(prefix.length + sha256Hash.length + suffix.length);
  req.set(prefix);
  req.set(sha256Hash, prefix.length);
  req.set(suffix, prefix.length + sha256Hash.length);

  return req;
}

/**
 * Submit a SHA-256 hash to a TSA and return the raw DER TimeStampResp.
 *
 * @param sha256Hash - 32-byte SHA-256 digest to timestamp
 * @param tsaUrl     - TSA endpoint (defaults to Sectigo)
 * @returns Raw DER-encoded TimeStampResp bytes
 */
export async function requestTimestamp(
  sha256Hash: Uint8Array,
  tsaUrl: string = DEFAULT_TSA_URL,
): Promise<Uint8Array> {
  const body = buildTimeStampReq(sha256Hash);

  const response = await fetch(tsaUrl, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/timestamp-query',
    },
    body: body as unknown as BodyInit,
  });

  if (!response.ok) {
    throw new Error(`TSA request failed: ${response.status} ${response.statusText}`);
  }

  const contentType = response.headers.get('Content-Type') ?? '';
  if (!contentType.includes('application/timestamp-reply')) {
    throw new Error(`Unexpected TSA response content-type: ${contentType}`);
  }

  return new Uint8Array(await response.arrayBuffer());
}
