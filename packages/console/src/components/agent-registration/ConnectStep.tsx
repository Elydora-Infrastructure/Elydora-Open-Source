'use client';

import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { AgentCredentials } from '@/features/agent-registration/credentials';
import type { IntegrationCatalogItem } from '@/features/agent-registration/integrations';
import {
  buildInstallInstructions,
  SDK_LANGUAGES,
  type SdkLanguage,
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
  const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL ?? 'http://localhost:8787';
  const instructions = useMemo(
    () => buildInstallInstructions(language, { integration, credentials, token, baseUrl }),
    [baseUrl, credentials, integration, language, token],
  );

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

      <section aria-labelledby="setup-command-label">
        <div className="flex items-center justify-between mb-2">
          <h3 id="setup-command-label" className="section-label">
            {integration.mode === 'adapter' ? t('agentRegistration.install') : t('agentRegistration.configure')}
          </h3>
          <CopyControl value={instructions.setup} />
        </div>
        <pre className="p-4 bg-ink text-[#EAEAE5] font-mono text-[10px] leading-relaxed whitespace-pre-wrap break-all overflow-x-auto">
          {instructions.setup}
        </pre>
      </section>

      {instructions.usage && (
        <section aria-labelledby="record-operation-label">
          <div className="flex items-center justify-between mb-2">
            <h3 id="record-operation-label" className="section-label">{t('agentRegistration.record')}</h3>
            <CopyControl value={instructions.usage} />
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
            <CopyControl value={instructions.verify} />
          </div>
          <code className="block px-3 py-2 border-y border-border font-mono text-[11px] text-ink">
            {instructions.verify}
          </code>
        </section>
      )}

      {instructions.postInstall && (
        <ol className="border-t border-border" aria-label={t('agentRegistration.postInstall')}>
          {instructions.postInstall.map((step, index) => (
            <li key={step} className="grid grid-cols-[2rem_1fr] gap-2 py-2 border-b border-border font-mono text-[11px] text-ink">
              <span className="text-ink-dim tabular-nums">{index + 1}</span>
              <span>{step}</span>
            </li>
          ))}
        </ol>
      )}

      <div className="flex items-center justify-between pt-2">
        <button type="button" className="btn-ghost" onClick={onBack}>{t('common.back')}</button>
        <button type="button" className="btn-brutalist" onClick={onDone}>{t('agentRegistration.done')}</button>
      </div>
    </div>
  );
}
