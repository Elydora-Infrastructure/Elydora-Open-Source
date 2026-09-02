'use client';

import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { AgentCredentials } from '@/features/agent-registration/credentials';
import type { IntegrationCatalogItem } from '@/features/agent-registration/integrations';
import {
  buildInstallInstructions,
  SDK_LANGUAGES,
  SHELLS,
  type SdkLanguage,
  type Shell,
} from '@/features/agent-registration/install-instructions';
import CopyControl from './CopyControl';

interface ConnectStepProps {
  readonly integration: IntegrationCatalogItem;
  readonly credentials: AgentCredentials;
  readonly token: string;
  readonly onBack: () => void;
  readonly onDone: () => void;
}

export default function ConnectStep({
  integration,
  credentials,
  token,
  onBack,
  onDone,
}: ConnectStepProps) {
  const { t } = useTranslation();
  const [language, setLanguage] = useState<SdkLanguage>('node');
  const [shell, setShell] = useState<Shell>('posix');
  const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL ?? 'http://localhost:8787';
  const instructions = useMemo(
    () => buildInstallInstructions(language, {
      integration,
      identity: {
        agentId: credentials.agentId,
        kid: credentials.kid,
        orgId: credentials.orgId,
      },
      baseUrl,
      privateKey: credentials.privateKey,
      token,
    }, shell),
    [baseUrl, credentials, integration, language, shell, token],
  );
  const PostInstallList = instructions.postInstall?.kind === 'alternatives' ? 'ul' : 'ol';

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between border-b border-border pb-3">
        <span className="section-label">{t('agentRegistration.integration')}</span>
        <span className="font-mono text-[12px] text-ink">{integration.name}</span>
      </div>

      <div className="flex border-b border-border" role="tablist" aria-label={t('agentRegistration.sdkLanguage')}>
        {SDK_LANGUAGES.map((item) => (
          <button
            key={item}
            type="button"
            role="tab"
            aria-selected={language === item}
            onClick={() => setLanguage(item)}
            className={`px-4 py-2 font-mono text-[10px] uppercase tracking-wider border-b-2 -mb-px ${
              language === item ? 'border-ink text-ink' : 'border-transparent text-ink-dim hover:text-ink'
            }`}
          >
            {item}
          </button>
        ))}
      </div>

      {integration.mode === 'hooks' && (
        <div className="flex border-b border-border" role="tablist" aria-label={t('agentRegistration.shell')}>
          {SHELLS.map((item) => (
            <button
              key={item}
              type="button"
              role="tab"
              aria-selected={shell === item}
              onClick={() => setShell(item)}
              className={`px-4 py-2 font-mono text-[10px] tracking-wider border-b-2 -mb-px ${
                shell === item ? 'border-ink text-ink' : 'border-transparent text-ink-dim hover:text-ink'
              }`}
            >
              {item === 'posix' ? 'sh' : 'PowerShell'}
            </button>
          ))}
        </div>
      )}

      <section aria-labelledby="setup-command-label">
        <div className="flex items-center justify-between mb-2">
          <h3 id="setup-command-label" className="section-label">
            {integration.mode === 'hooks' ? t('agentRegistration.install') : t('agentRegistration.configure')}
          </h3>
          <CopyControl
            value={instructions.setup}
            ariaLabel={t('agentRegistration.copySetup')}
          />
        </div>
        <pre className="p-4 bg-ink text-[#EAEAE5] font-mono text-[10px] leading-relaxed whitespace-pre-wrap break-all overflow-x-auto">
          {instructions.setup}
        </pre>
      </section>

      <div>
        <div
          className="border-y border-border"
          role="group"
          aria-label={t('agentRegistration.steps.credentials')}
        >
          <div className="flex items-center justify-between py-2 border-b border-border">
            <span className="section-label">{t('agentRegistration.privateKey')}</span>
            <CopyControl
              value={credentials.privateKey}
              ariaLabel={t('agentRegistration.copyPrivateKey')}
            />
          </div>
          <div className="flex items-center justify-between py-2">
            <span className="section-label">{t('agentRegistration.apiToken')}</span>
            <CopyControl
              value={token}
              ariaLabel={t('agentRegistration.copyApiToken')}
            />
          </div>
        </div>
        <p className="mt-2 font-mono text-[10px] text-ink-dim">
          {t(`agentRegistration.secretDelivery.${instructions.secretDelivery}`)}
        </p>
      </div>

      {instructions.usage && (
        <section aria-labelledby="record-operation-label">
          <div className="flex items-center justify-between mb-2">
            <h3 id="record-operation-label" className="section-label">{t('agentRegistration.record')}</h3>
            <CopyControl
              value={instructions.usage}
              ariaLabel={t('agentRegistration.copyUsage')}
            />
          </div>
          <pre className="p-4 bg-ink text-[#EAEAE5] font-mono text-[10px] leading-relaxed whitespace-pre-wrap break-all overflow-x-auto">
            {instructions.usage}
          </pre>
        </section>
      )}

      {instructions.verify && (
        <section aria-labelledby="verify-command-label">
          <div className="flex items-center justify-between mb-2">
            <h3 id="verify-command-label" className="section-label">{t('agentRegistration.verify')}</h3>
            <CopyControl
              value={instructions.verify}
              ariaLabel={t('agentRegistration.copyVerify')}
            />
          </div>
          <code className="block px-3 py-2 border-y border-border font-mono text-[11px] text-ink">
            {instructions.verify}
          </code>
        </section>
      )}

      {instructions.postInstall && (
        <PostInstallList className="border-t border-border" aria-label={t('agentRegistration.postInstall')}>
          {instructions.postInstall.steps.map((step, index) => (
            <li
              key={`${step.key}:${step.command ?? ''}`}
              className="group grid grid-cols-[2rem_minmax(0,1fr)_auto] items-start gap-2 border-b border-border py-2 font-mono text-[11px] text-ink"
            >
              <span className="text-ink-dim tabular-nums">
                {step.variant ?? String(index + 1).padStart(2, '0')}
              </span>
              <span className="min-w-0">
                <span className="block">
                  {t(`agentRegistration.postInstallSteps.${step.key}`, {
                    integration: integration.name,
                  })}
                </span>
                {step.command && (
                  <code className="mt-1 block break-all text-ink-dim">{step.command}</code>
                )}
              </span>
              {step.command && (
                <CopyControl
                  value={step.command}
                  ariaLabel={t('agentRegistration.copyPostInstall', { command: step.command })}
                  className="opacity-100 transition-opacity md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100"
                />
              )}
            </li>
          ))}
        </PostInstallList>
      )}

      <div className="flex items-center justify-between pt-2">
        <button type="button" className="btn-ghost" onClick={onBack}>{t('common.back')}</button>
        <button type="button" className="btn-brutalist" onClick={onDone}>{t('agentRegistration.done')}</button>
      </div>
    </div>
  );
}
