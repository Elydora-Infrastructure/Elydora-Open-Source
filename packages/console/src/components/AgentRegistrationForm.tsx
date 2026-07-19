'use client';

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '@/lib/api';
import { useAuth } from '@/lib/auth';
import { generateAgentCredentials, type AgentCredentials } from '@/features/agent-registration/credentials';
import {
  assertRegistrationResponse,
  RegistrationContractError,
} from '@/features/agent-registration/registration-contract';
import type { IntegrationCatalogItem } from '@/features/agent-registration/integrations';
import RegistrationProgress from './agent-registration/RegistrationProgress';
import IntegrationStep from './agent-registration/IntegrationStep';
import DetailsStep, { type AgentDetails } from './agent-registration/DetailsStep';
import CredentialsStep from './agent-registration/CredentialsStep';
import ConnectStep from './agent-registration/ConnectStep';

interface AgentRegistrationFormProps {
  readonly onRegistered: () => void;
  readonly onDismissibleChange: (dismissible: boolean) => void;
  readonly onSuccess: () => void;
}

type RegistrationState =
  | { readonly step: 'integration' }
  | { readonly step: 'details'; readonly integration: IntegrationCatalogItem }
  | {
      readonly step: 'credentials';
      readonly integration: IntegrationCatalogItem;
      readonly credentials: AgentCredentials;
      readonly token?: string;
    }
  | {
      readonly step: 'connect';
      readonly integration: IntegrationCatalogItem;
      readonly credentials: AgentCredentials;
      readonly token: string;
    };

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

export default function AgentRegistrationForm({
  onRegistered,
  onDismissibleChange,
  onSuccess,
}: AgentRegistrationFormProps) {
  const { t } = useTranslation();
  const { user } = useAuth();
  const [state, setState] = useState<RegistrationState>({ step: 'integration' });
  const [registering, setRegistering] = useState(false);
  const [registrationError, setRegistrationError] = useState<string | null>(null);
  const [registrationBlocked, setRegistrationBlocked] = useState(false);
  const [issuingToken, setIssuingToken] = useState(false);
  const [tokenError, setTokenError] = useState<string | null>(null);

  const register = async (details: AgentDetails) => {
    if (state.step !== 'details') {
      throw new Error('Agent registration can only start from the details step.');
    }

    setRegistering(true);
    setRegistrationError(null);
    setRegistrationBlocked(false);
    onDismissibleChange(false);
    const integration = state.integration;
    try {
      const credentials = await generateAgentCredentials(user?.org_id ?? '');
      const response = await api.agents.register({
        agent_id: credentials.agentId,
        display_name: details.displayName,
        responsible_entity: details.responsibleEntity,
        integration_type: integration.id,
        keys: [{
          kid: credentials.kid,
          public_key: credentials.publicKey,
          algorithm: 'ed25519',
        }],
      });
      assertRegistrationResponse(response, credentials, integration.id);
      onRegistered();
      setState({ step: 'credentials', integration, credentials });
    } catch (error) {
      onDismissibleChange(true);
      setRegistrationBlocked(error instanceof RegistrationContractError);
      setRegistrationError(errorMessage(error, t('agentRegistration.failedToRegister')));
    } finally {
      setRegistering(false);
    }
  };

  const issueToken = async (ttlSeconds: number | null) => {
    if (state.step !== 'credentials') {
      throw new Error('An API token can only be issued from the credentials step.');
    }

    setIssuingToken(true);
    setTokenError(null);
    try {
      const response = await api.auth.issueToken(ttlSeconds);
      if (!response.token.trim()) {
        throw new Error('The token endpoint returned an empty token.');
      }
      setState((current) => current.step === 'credentials'
        ? { ...current, token: response.token }
        : current);
    } catch (error) {
      setTokenError(errorMessage(error, t('agentRegistration.failedToIssueToken')));
    } finally {
      setIssuingToken(false);
    }
  };

  const continueToConnect = () => {
    if (state.step !== 'credentials' || !state.token) {
      throw new Error('A token is required before generating connection instructions.');
    }
    setState({
      step: 'connect',
      integration: state.integration,
      credentials: state.credentials,
      token: state.token,
    });
  };

  return (
    <div>
      <RegistrationProgress current={state.step} />

      {state.step === 'integration' && (
        <IntegrationStep
          onSelect={(integration) => setState({ step: 'details', integration })}
        />
      )}

      {state.step === 'details' && (
        <DetailsStep
          integration={state.integration}
          submitting={registering}
          blocked={registrationBlocked}
          error={registrationError}
          onSubmit={register}
          onBack={() => {
            setRegistrationError(null);
            setRegistrationBlocked(false);
            setState({ step: 'integration' });
          }}
        />
      )}

      {state.step === 'credentials' && (
        <CredentialsStep
          credentials={state.credentials}
          token={state.token}
          issuingToken={issuingToken}
          tokenError={tokenError}
          onIssueToken={issueToken}
          onContinue={continueToConnect}
        />
      )}

      {state.step === 'connect' && (
        <ConnectStep
          integration={state.integration}
          credentials={state.credentials}
          token={state.token}
          onBack={() => setState({
            step: 'credentials',
            integration: state.integration,
            credentials: state.credentials,
            token: state.token,
          })}
          onDone={onSuccess}
        />
      )}
    </div>
  );
}
