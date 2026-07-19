'use client';

import { useTranslation } from 'react-i18next';

const STEPS = ['integration', 'details', 'credentials', 'connect'] as const;
export type RegistrationStep = (typeof STEPS)[number];

export default function RegistrationProgress({ current }: { current: RegistrationStep }) {
  const { t } = useTranslation();
  const activeIndex = STEPS.indexOf(current);

  return (
    <ol className="grid grid-cols-4 border-b border-border mb-6" aria-label={t('agentRegistration.progress')}>
      {STEPS.map((step, index) => (
        <li
          key={step}
          className={`pb-2 font-mono text-[10px] uppercase tracking-wider border-b -mb-px ${
            index <= activeIndex ? 'border-ink text-ink' : 'border-transparent text-ink-dim'
          }`}
          aria-current={index === activeIndex ? 'step' : undefined}
        >
          <span className="mr-1.5 tabular-nums">{index + 1}</span>
          <span className="hidden sm:inline">{t(`agentRegistration.steps.${step}`)}</span>
        </li>
      ))}
    </ol>
  );
}
