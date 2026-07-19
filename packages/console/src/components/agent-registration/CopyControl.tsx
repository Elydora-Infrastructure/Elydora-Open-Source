'use client';

import { useState } from 'react';
import { useTranslation } from 'react-i18next';

interface CopyControlProps {
  readonly value: string;
  readonly className?: string;
  readonly ariaLabel?: string;
}

export default function CopyControl({ value, className = '', ariaLabel }: CopyControlProps) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<'idle' | 'copied' | 'failed'>('idle');

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setStatus('copied');
    } catch {
      setStatus('failed');
    }
  };

  return (
    <span className={`inline-flex items-center gap-2 ${className}`}>
      <button
        type="button"
        onClick={copy}
        aria-label={ariaLabel}
        className="font-mono text-[10px] uppercase tracking-wider text-ink-dim hover:text-ink"
      >
        {status === 'copied' ? t('common.copied') : t('common.copy')}
      </button>
      {status === 'failed' && (
        <span className="font-mono text-[10px] text-red-600" role="alert">
          {t('agentRegistration.copyFailed')}
        </span>
      )}
    </span>
  );
}
