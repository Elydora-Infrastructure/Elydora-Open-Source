import { expect, test } from '@playwright/test';
import { buildInstallInstructions, SDK_LANGUAGES } from '@/features/agent-registration/install-instructions';
import {
  INTEGRATION_CATALOG,
  INTEGRATION_TYPES,
} from '@/features/agent-registration/integrations';
import {
  assertRegistrationResponse,
  RegistrationContractError,
} from '@/features/agent-registration/registration-contract';

const EXPECTED_INTEGRATIONS = [
  'augment',
  'claudecode',
  'cline',
  'codex',
  'copilot',
  'cursor',
  'droid',
  'gemini',
  'grok',
  'kimi',
  'kirocli',
  'kiroide',
  'letta',
  'opencode',
  'qwen',
  'enterprise',
  'gui',
  'sdk',
  'other',
] as const;

const credentials = {
  agentId: 'agent-contract',
  kid: 'agent-contract-key-1',
  publicKey: 'public-key',
  privateKey: 'private-key',
  orgId: 'org-contract',
};

test('keeps the complete integration catalog in canonical order', () => {
  expect(INTEGRATION_TYPES).toEqual(EXPECTED_INTEGRATIONS);
  expect(INTEGRATION_CATALOG.map(({ id }) => id)).toEqual(EXPECTED_INTEGRATIONS);
  expect(new Set(INTEGRATION_CATALOG.map(({ id }) => id)).size).toBe(19);
  expect(INTEGRATION_CATALOG.filter(({ mode }) => mode === 'adapter')).toHaveLength(15);
  expect(INTEGRATION_CATALOG.filter(({ mode }) => mode === 'sdk')).toHaveLength(4);
});

test('generates every adapter command with the verified SDK flag contract', () => {
  for (const integration of INTEGRATION_CATALOG.filter(({ mode }) => mode === 'adapter')) {
    for (const language of SDK_LANGUAGES) {
      const instructions = buildInstallInstructions(language, {
        integration,
        credentials,
        token: 'token-contract',
        baseUrl: 'https://api.example.com',
      });

      expect(instructions.setup).toContain(`--agent ${integration.id}`);
      expect(instructions.setup).toContain('"private-key"');
      expect(instructions.setup).toContain('"token-contract"');
      expect(instructions.setup).toContain('"https://api.example.com"');

      if (language === 'go') {
        expect(instructions.setup).toContain('--org-id "org-contract"');
        expect(instructions.setup).toContain('--agent-id "agent-contract"');
        expect(instructions.setup).toContain('--private-key "private-key"');
        expect(instructions.setup).toContain('--base-url "https://api.example.com"');
      } else {
        expect(instructions.setup).toContain('--org_id "org-contract"');
        expect(instructions.setup).toContain('--agent_id "agent-contract"');
        expect(instructions.setup).toContain('--private_key "private-key"');
        expect(instructions.setup).toContain('--base_url "https://api.example.com"');
      }
    }
  }
});

test('generates direct SDK setup and operation code for each language', () => {
  const integration = INTEGRATION_CATALOG.find(({ id }) => id === 'sdk');
  expect(integration).toBeDefined();

  for (const language of SDK_LANGUAGES) {
    const instructions = buildInstallInstructions(language, {
      integration: integration!,
      credentials,
      token: 'token-contract',
      baseUrl: 'https://api.example.com',
    });
    expect(instructions.setup).toContain('agent-contract');
    expect(instructions.setup).toContain('token-contract');
    expect(instructions.usage).toContain('ai.tool_use');
    expect(instructions.verify).toBeUndefined();
  }
});

test('classifies malformed successful responses as blocking contract failures', () => {
  expect(() => assertRegistrationResponse(
    null as never,
    credentials,
    'grok',
  )).toThrow(RegistrationContractError);
});
