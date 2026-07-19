'use client';

import { useTranslation } from 'react-i18next';
import {
  INTEGRATION_CATALOG,
  type IntegrationCatalogItem,
  type IntegrationMode,
} from '@/features/agent-registration/integrations';

interface IntegrationStepProps {
  readonly onSelect: (integration: IntegrationCatalogItem) => void;
}

function IntegrationGroup({
  mode,
  onSelect,
}: {
  readonly mode: IntegrationMode;
  readonly onSelect: (integration: IntegrationCatalogItem) => void;
}) {
  const { t } = useTranslation();
  const integrations = INTEGRATION_CATALOG.filter((item) => item.mode === mode);

  return (
    <section aria-labelledby={`integration-${mode}`}>
      <h3 id={`integration-${mode}`} className="section-label mb-2">
        {t(`agentRegistration.integrationGroups.${mode}`)}
      </h3>
      <div className="grid sm:grid-cols-2 border-t border-border sm:[&>button:nth-child(odd)]:border-r">
        {integrations.map((integration) => (
          <button
            key={integration.id}
            type="button"
            onClick={() => onSelect(integration)}
            className="group flex items-center justify-between gap-3 px-3 py-3 text-left border-b border-border hover:bg-surface focus-visible:bg-surface focus-visible:outline-none"
          >
            <span className="font-mono text-[12px] text-ink">{integration.name}</span>
            <span className="font-mono text-[10px] text-ink-dim opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 transition-opacity" aria-hidden="true">
              →
            </span>
          </button>
        ))}
      </div>
    </section>
  );
}
export default function IntegrationStep({ onSelect }: IntegrationStepProps) {
  return (
    <div className="space-y-6">
      <IntegrationGroup mode="adapter" onSelect={onSelect} />
      <IntegrationGroup mode="sdk" onSelect={onSelect} />
    </div>
  );
}
