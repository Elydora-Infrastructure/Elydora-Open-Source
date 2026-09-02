import {
  INTEGRATION_TYPES,
  type IntegrationType,
} from '../../../../server/src/shared/types/enums';

export { INTEGRATION_TYPES };

export type IntegrationMode = 'hooks' | 'sdk';
export type PostInstallStepKey =
  | 'inspectEnvironment'
  | 'inspectCursorHooks'
  | 'inspectGrokConfig'
  | 'inspectKiroIdeHooks'
  | 'launchKiroV2'
  | 'launchKiroV3'
  | 'listHooks'
  | 'openHooks'
  | 'reviewAndTrustHooks'
  | 'restartSession'
  | 'startSession'
  | 'startTask';

export interface PostInstallStep {
  readonly key: PostInstallStepKey;
  readonly command?: string;
  readonly variant?: string;
}

export interface PostInstallPlan {
  readonly kind: 'sequence' | 'alternatives';
  readonly steps: readonly PostInstallStep[];
}

export interface IntegrationCatalogItem {
  readonly id: IntegrationType;
  readonly name: string;
  readonly mode: IntegrationMode;
  readonly postInstall?: PostInstallPlan;
}

const INTEGRATION_METADATA: Record<
  IntegrationType,
  Omit<IntegrationCatalogItem, 'id'>
> = {
  augment: {
    name: 'Augment Code',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'startSession', command: 'auggie' }],
    },
  },
  claudecode: {
    name: 'Claude Code',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'openHooks', command: '/hooks' }],
    },
  },
  cline: {
    name: 'Cline',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'startTask' }],
    },
  },
  codex: {
    name: 'Codex CLI',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'reviewAndTrustHooks', command: '/hooks' }],
    },
  },
  copilot: {
    name: 'GitHub Copilot CLI',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [
        { key: 'restartSession', command: '/restart' },
        { key: 'inspectEnvironment', command: '/env' },
      ],
    },
  },
  cursor: {
    name: 'Cursor CLI',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'inspectCursorHooks' }],
    },
  },
  droid: {
    name: 'Factory Droid',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'openHooks', command: '/hooks' }],
    },
  },
  gemini: {
    name: 'Gemini CLI',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'listHooks', command: '/hooks list' }],
    },
  },
  grok: {
    name: 'Grok Build',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'inspectGrokConfig', command: 'grok inspect --json' }],
    },
  },
  kimi: {
    name: 'Kimi Code',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'startSession', command: 'kimi' }],
    },
  },
  kirocli: {
    name: 'Kiro CLI',
    mode: 'hooks',
    postInstall: {
      kind: 'alternatives',
      steps: [
        { key: 'launchKiroV2', command: 'kiro-cli --agent elydora-audit', variant: 'V2' },
        { key: 'launchKiroV3', command: 'kiro-cli --v3', variant: 'V3' },
      ],
    },
  },
  kiroide: {
    name: 'Kiro IDE',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'inspectKiroIdeHooks' }],
    },
  },
  letta: {
    name: 'Letta Code',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'openHooks', command: '/hooks' }],
    },
  },
  opencode: {
    name: 'OpenCode',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'startSession', command: 'opencode' }],
    },
  },
  qwen: {
    name: 'Qwen Code',
    mode: 'hooks',
    postInstall: {
      kind: 'sequence',
      steps: [{ key: 'listHooks', command: '/hooks list' }],
    },
  },
  enterprise: { name: 'Enterprise Agent', mode: 'sdk' },
  gui: { name: 'GUI Agent', mode: 'sdk' },
  sdk: { name: 'SDK', mode: 'sdk' },
  other: { name: 'Other', mode: 'sdk' },
};

export const INTEGRATION_CATALOG: readonly IntegrationCatalogItem[] =
  INTEGRATION_TYPES.map((id) => ({ id, ...INTEGRATION_METADATA[id] }));

export const HOOK_INTEGRATION_IDS: ReadonlySet<IntegrationType> = new Set(
  INTEGRATION_CATALOG.filter(({ mode }) => mode === 'hooks').map(({ id }) => id),
);
