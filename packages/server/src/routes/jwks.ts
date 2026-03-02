/**
 * JWKS route — public key discovery endpoint.
 *
 * Returns the JSON Web Key Set containing the Elydora server's public
 * keys used for signing attestation receipts (EAR) and epoch roots (EER).
 * External verifiers can use these keys to independently validate
 * signatures without contacting the Elydora API.
 *
 * This endpoint requires NO authentication (per OpenAPI spec and RFC 7517).
 */

import { Hono } from 'hono';
import type { Env, AppVariables } from '../types.js';
import type { JWKSResponse } from '../shared/index.js';
import { deriveEd25519PublicKey } from '../utils/crypto.js';

const jwks = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// ---------------------------------------------------------------------------
// GET /.well-known/elydora/jwks.json — Retrieve the platform JWKS
// ---------------------------------------------------------------------------
jwks.get('/', async (c) => {
  // Derive the public key from the server signing key
  const publicKeyBase64url = await deriveEd25519PublicKey(c.env.ELYDORA_SIGNING_KEY);

  const response: JWKSResponse = {
    keys: [
      {
        kty: 'OKP',
        crv: 'Ed25519',
        x: publicKeyBase64url,
        kid: 'elydora-server-key-v1',
        use: 'sig',
        alg: 'EdDSA',
      },
    ],
  };

  c.header('Cache-Control', 'public, max-age=3600');
  return c.json(response, 200);
});

export { jwks };
