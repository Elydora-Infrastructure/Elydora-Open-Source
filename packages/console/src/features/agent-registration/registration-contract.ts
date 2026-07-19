import type { IntegrationType, RegisterAgentResponse } from '@elydora/shared';
import type { AgentCredentials } from './credentials';

export class RegistrationContractError extends Error {
  override readonly name = 'RegistrationContractError';
}

function contractError(message: string): never {
  throw new RegistrationContractError(message);
}

export function assertRegistrationResponse(
  response: RegisterAgentResponse,
  credentials: AgentCredentials,
  integrationType: IntegrationType,
): void {
  if (response?.agent?.agent_id !== credentials.agentId) {
    contractError('The registration response returned a different agent ID.');
  }
  if (response?.agent?.org_id !== credentials.orgId) {
    contractError('The registration response returned a different organization ID.');
  }
  if (response?.agent?.integration_type !== integrationType) {
    contractError('The registration response returned a different integration type.');
  }

  const registeredKey = Array.isArray(response?.keys)
    ? response.keys.find(({ kid }) => kid === credentials.kid)
    : undefined;
  if (!registeredKey || registeredKey.public_key !== credentials.publicKey) {
    contractError('The registration response did not confirm the generated public key.');
  }
}
