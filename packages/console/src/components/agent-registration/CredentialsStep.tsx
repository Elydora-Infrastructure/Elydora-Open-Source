'use client';

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { AgentCredentials } from '@/features/agent-registration/credentials';
import CopyControl from './CopyControl';

const EXPIRATION_OPTIONS = [
  { id: '24hours', seconds: 86_400 },
  { id: '7days', seconds: 604_800 },
  { id: '1month', seconds: 2_592_000 },
  { id: '1year', seconds: 31_536_000 },
  { id: 'custom', seconds: -1 },
  { id: 'neverExpire', seconds: null },
] as const;
const MAX_CUSTOM_DAYS = 365;

type ExpirationId = (typeof EXPIRATION_OPTIONS)[number]['id'];

interface CredentialsStepProps {
  readonly credentials: AgentCredentials;
  readonly token?: string;
  readonly issuingToken: boolean;
  readonly tokenError: string | null;
  readonly onIssueToken: (ttlSeconds: number | null) => void;
  readonly onContinue: () => void;
}

function CredentialRow({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="grid grid-cols-[7rem_minmax(0,1fr)_auto] items-center gap-3 py-2 border-b border-border last:border-b-0">
      <dt className="section-label">{label}</dt>
      <dd className="font-mono text-[11px] text-ink truncate" title={value}>{value}</dd>
      <dd><CopyControl value={value} /></dd>
    </div>
  );
}
export default function CredentialsStep({
  credentials,
  token,
  issuingToken,
  tokenError,
  onIssueToken,
  onContinue,
}: CredentialsStepProps) {
  const { t } = useTranslation();
  const [expiration, setExpiration] = useState<ExpirationId>('24hours');
  const [customDays, setCustomDays] = useState('');
  const [validationError, setValidationError] = useState<string | null>(null);

  const issueToken = () => {
    const option = EXPIRATION_OPTIONS.find(({ id }) => id === expiration);
    if (!option) {
      setValidationError(t('agentRegistration.invalidExpiration'));
      return;
    }

    if (option.seconds === -1) {
      const days = Number.parseInt(customDays, 10);
      if (!Number.isSafeInteger(days) || days < 1 || days > MAX_CUSTOM_DAYS) {
        setValidationError(t('agentRegistration.enterValidDays'));
        return;
      }
      setValidationError(null);
      onIssueToken(days * 86_400);
      return;
    }

    setValidationError(null);
    onIssueToken(option.seconds);
  };

  return (
    <div className="space-y-6">
      <dl>
        <CredentialRow label={t('agentRegistration.credAgentId')} value={credentials.agentId} />
        <CredentialRow label={t('agentRegistration.credKeyId')} value={credentials.kid} />
        <CredentialRow label={t('agentRegistration.credOrgId')} value={credentials.orgId} />
      </dl>

      <section aria-labelledby="private-key-label">
        <div className="flex items-center justify-between mb-2">
          <h3 id="private-key-label" className="section-label text-red-700">
            {t('agentRegistration.privateKeySaveNow')}
          </h3>
          <CopyControl
            value={credentials.privateKey}
            ariaLabel={t('agentRegistration.copyPrivateKey')}
          />
        </div>
        <code className="block p-3 bg-ink text-[#EAEAE5] font-mono text-[10px] break-all select-all">
          {credentials.privateKey}
        </code>
        <p className="font-mono text-[10px] text-ink-dim mt-2">
          {t('agentRegistration.privateKeyWarning')}
        </p>
      </section>

      <section aria-labelledby="api-token-label" className="border-t border-border pt-5">
        <h3 id="api-token-label" className="section-label mb-3">
          {t('agentRegistration.apiToken')}
        </h3>

        {token ? (
          <div>
            <div className="flex items-center justify-between mb-2">
              <span className="font-mono text-[10px] text-ink-dim">
                {t('agentRegistration.tokenIssued')}
              </span>
              <CopyControl
                value={token}
                ariaLabel={t('agentRegistration.copyApiToken')}
              />
            </div>
            <code className="block p-3 bg-ink text-[#EAEAE5] font-mono text-[10px] break-all select-all">
              {token}
            </code>
          </div>
        ) : (
          <div className="flex flex-col sm:flex-row sm:items-end gap-3">
            <div className="flex-1">
              <label htmlFor="token-expiration" className="section-label block mb-1.5">
                {t('agentRegistration.tokenExpiration')}
              </label>
              <select
                id="token-expiration"
                value={expiration}
                onChange={(event) => setExpiration(event.target.value as ExpirationId)}
                className="w-full px-3 py-2 bg-transparent border border-border font-mono text-[12px] text-ink focus:outline-none focus:border-ink"
                disabled={issuingToken}
              >
                {EXPIRATION_OPTIONS.map(({ id }) => (
                  <option key={id} value={id}>{t(`tokenExpiration.${id}`)}</option>
                ))}
              </select>
            </div>
            {expiration === 'custom' && (
              <div className="sm:w-28">
                <label htmlFor="custom-token-days" className="section-label block mb-1.5">
                  {t('agentRegistration.days')}
                </label>
                <input
                  id="custom-token-days"
                  type="number"
                  min="1"
                  step="1"
                  value={customDays}
                  onChange={(event) => setCustomDays(event.target.value)}
                  className="w-full px-3 py-2 bg-transparent border border-border font-mono text-[12px] text-ink focus:outline-none focus:border-ink"
                  disabled={issuingToken}
                />
              </div>
            )}
            <button type="button" className="btn-brutalist" onClick={issueToken} disabled={issuingToken}>
              {issuingToken ? t('agentRegistration.issuing') : t('agentRegistration.issueApiToken')}
            </button>
          </div>
        )}

        {(validationError || tokenError) && (
          <p className="font-mono text-[11px] text-red-600 mt-2" role="alert">
            {validationError ?? tokenError}
          </p>
        )}
      </section>

      <div className="flex justify-end pt-2">
        <button type="button" className="btn-brutalist" onClick={onContinue} disabled={!token}>
          {t('common.continue')}
        </button>
      </div>
    </div>
  );
}
