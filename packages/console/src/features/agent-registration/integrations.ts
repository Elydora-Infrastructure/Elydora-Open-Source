import {
  INTEGRATION_TYPES,
  type IntegrationType,
} from '../../../../server/src/shared/types/enums';

export { INTEGRATION_TYPES };

export type IntegrationMode = 'adapter' | 'sdk';
export type PostInstallAction = 'review-hooks' | 'start-kiro';

export interface IntegrationCatalogItem {
  readonly id: IntegrationType;
  readonly name: string;
  readonly mode: IntegrationMode;
  readonly postInstall?: PostInstallAction;
}

const INTEGRATION_METADATA: Record<
  IntegrationType,
  Omit<IntegrationCatalogItem, 'id'>
> = {
  augment: { name: 'Augment Code', mode: 'adapter' },
  claudecode: { name: 'Claude Code', mode: 'adapter' },
  cline: { name: 'Cline', mode: 'adapter' },
  codex: { name: 'OpenAI Codex', mode: 'adapter', postInstall: 'review-hooks' },
  copilot: { name: 'GitHub Copilot CLI', mode: 'adapter' },
  cursor: { name: 'Cursor', mode: 'adapter' },
  droid: { name: 'Factory Droid', mode: 'adapter', postInstall: 'review-hooks' },
  gemini: { name: 'Gemini CLI', mode: 'adapter' },
  grok: { name: 'Grok Build', mode: 'adapter' },
  kimi: { name: 'Kimi CLI', mode: 'adapter', postInstall: 'review-hooks' },
  kirocli: { name: 'Kiro CLI', mode: 'adapter', postInstall: 'start-kiro' },
  kiroide: { name: 'Kiro IDE', mode: 'adapter' },
  letta: { name: 'Letta Code', mode: 'adapter' },
  opencode: { name: 'OpenCode', mode: 'adapter' },
  qwen: { name: 'Qwen Code', mode: 'adapter', postInstall: 'review-hooks' },
  enterprise: { name: 'Enterprise Agent', mode: 'sdk' },
  gui: { name: 'GUI Agent', mode: 'sdk' },
  sdk: { name: 'SDK', mode: 'sdk' },
  other: { name: 'Other', mode: 'sdk' },
};

export const INTEGRATION_CATALOG: readonly IntegrationCatalogItem[] =
  INTEGRATION_TYPES.map((id) => ({ id, ...INTEGRATION_METADATA[id] }));

export const ADAPTER_INTEGRATION_IDS: ReadonlySet<IntegrationType> = new Set(
  INTEGRATION_CATALOG.filter(({ mode }) => mode === 'adapter').map(({ id }) => id),
);
