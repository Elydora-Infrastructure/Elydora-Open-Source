export interface AgentCredentials {
  readonly agentId: string;
  readonly kid: string;
  readonly publicKey: string;
  readonly privateKey: string;
  readonly orgId: string;
}

function requireJwkValue(value: string | undefined, field: 'd' | 'x'): string {
  if (!value) {
    throw new Error(`Web Crypto did not export the Ed25519 JWK ${field} value.`);
  }
  return value;
}

export async function generateAgentCredentials(orgId: string): Promise<AgentCredentials> {
  const normalizedOrgId = orgId.trim();
  if (!normalizedOrgId) {
    throw new Error('An active organization is required to register an agent.');
  }

  const agentId = `agent-${crypto.randomUUID()}`;
  const kid = `${agentId}-key-1`;
  const keyPair = await crypto.subtle.generateKey('Ed25519', true, ['sign', 'verify']);
  const [privateJwk, publicJwk] = await Promise.all([
    crypto.subtle.exportKey('jwk', keyPair.privateKey),
    crypto.subtle.exportKey('jwk', keyPair.publicKey),
  ]);

  return {
    agentId,
    kid,
    publicKey: requireJwkValue(publicJwk.x, 'x'),
    privateKey: requireJwkValue(privateJwk.d, 'd'),
    orgId: normalizedOrgId,
  };
}
