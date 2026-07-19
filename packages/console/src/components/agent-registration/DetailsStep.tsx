'use client';

import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import type { IntegrationCatalogItem } from '@/features/agent-registration/integrations';

export interface AgentDetails {
  readonly displayName: string;
  readonly responsibleEntity?: string;
}

interface DetailsStepProps {
  readonly integration: IntegrationCatalogItem;
  readonly submitting: boolean;
  readonly blocked: boolean;
  readonly error: string | null;
  readonly onSubmit: (details: AgentDetails) => void;
  readonly onBack: () => void;
}

export default function DetailsStep({
  integration,
  submitting,
  blocked,
  error,
  onSubmit,
  onBack,
}: DetailsStepProps) {
  const { t } = useTranslation();
  const [displayName, setDisplayName] = useState('');
  const [responsibleEntity, setResponsibleEntity] = useState('');
  const [validationError, setValidationError] = useState<string | null>(null);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const normalizedName = displayName.trim();
    if (!normalizedName) {
      setValidationError(t('agentRegistration.displayNameRequired'));
      return;
    }
    setValidationError(null);
    onSubmit({
      displayName: normalizedName,
      responsibleEntity: responsibleEntity.trim() || undefined,
    });
  };

  return (
    <form onSubmit={submit} className="space-y-5">
      <div className="flex items-center justify-between border-b border-border pb-3">
        <span className="section-label">{t('agentRegistration.integration')}</span>
        <span className="font-mono text-[12px] text-ink">{integration.name}</span>
      </div>

      {(validationError || error) && (
        <div className="px-3 py-2 border border-red-300 bg-red-50 text-red-700 font-mono text-[11px]" role="alert">
          {validationError ?? error}
        </div>
      )}

      <div>
        <label htmlFor="agent-display-name" className="section-label block mb-1.5">
          {t('agentRegistration.displayName')}
        </label>
        <input
          id="agent-display-name"
          value={displayName}
          onChange={(event) => setDisplayName(event.target.value)}
          className="w-full px-3 py-2 bg-transparent border border-border font-mono text-[13px] text-ink placeholder:text-ink-dim focus:outline-none focus:border-ink"
          placeholder={t('agentRegistration.displayNamePlaceholder')}
          disabled={submitting || blocked}
          required
          autoFocus
        />
      </div>

      <div>
        <label htmlFor="agent-responsible-entity" className="section-label block mb-1.5">
          {t('agentRegistration.responsibleEntity')}
        </label>
        <input
          id="agent-responsible-entity"
          value={responsibleEntity}
          onChange={(event) => setResponsibleEntity(event.target.value)}
          className="w-full px-3 py-2 bg-transparent border border-border font-mono text-[13px] text-ink placeholder:text-ink-dim focus:outline-none focus:border-ink"
          placeholder={t('agentRegistration.responsibleEntityPlaceholder')}
          disabled={submitting || blocked}
        />
      </div>

      <div className="flex items-center justify-between pt-2">
        <button type="button" className="btn-ghost" onClick={onBack} disabled={submitting || blocked}>
          {t('common.back')}
        </button>
        <button type="submit" className="btn-brutalist" disabled={submitting || blocked}>
          {submitting ? t('agentRegistration.submitting') : t('agentRegistration.submit')}
        </button>
      </div>
    </form>
  );
}
