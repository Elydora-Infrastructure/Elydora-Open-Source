/** JWKS route: public key discovery endpoint. */

import { Hono } from 'hono';
import type { Env, AppVariables } from '../types.js';
import type { JWKSResponse } from '../shared/index.js';
import { deriveEd25519PublicKey } from '../utils/crypto.js';

const jwks = new Hono<{ Bindings: Env; Variables: AppVariables }>();

// ---------------------------------------------------------------------------
// GET /.well-known/elydora/jwks.json: Retrieve the platform JWKS
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
