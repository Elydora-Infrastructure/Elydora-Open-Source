import { expect, test } from '@playwright/test';
import { buildInstallInstructions, SDK_LANGUAGES } from '@/features/agent-registration/install-instructions';
import {
  INTEGRATION_CATALOG,
  INTEGRATION_TYPES,
} from '@/features/agent-registration/integrations';
import enRegistration from '@/i18n/en.json';
import zhRegistration from '@/i18n/zh.json';
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
  expect(INTEGRATION_CATALOG.filter(({ mode }) => mode === 'hooks')).toHaveLength(15);
  expect(INTEGRATION_CATALOG.filter(({ mode }) => mode === 'sdk')).toHaveLength(4);
  expect(Object.fromEntries(INTEGRATION_CATALOG.map(({ id, name }) => [id, name])))
    .toMatchObject({
      codex: 'Codex CLI',
      cursor: 'Cursor CLI',
      grok: 'Grok Build',
      kimi: 'Kimi Code',
    });
});

test('keeps every native hook activation plan aligned with its provider contract', () => {
  const plans = Object.fromEntries(
    INTEGRATION_CATALOG
      .filter(({ mode }) => mode === 'hooks')
      .map(({ id, postInstall }) => [id, postInstall]),
  );

  expect(plans).toEqual({
    augment: {
      kind: 'sequence',
      steps: [{ key: 'startSession', command: 'auggie' }],
    },
    claudecode: {
      kind: 'sequence',
      steps: [{ key: 'openHooks', command: '/hooks' }],
    },
    cline: {
      kind: 'sequence',
      steps: [{ key: 'startTask' }],
    },
    codex: {
      kind: 'sequence',
      steps: [{ key: 'reviewAndTrustHooks', command: '/hooks' }],
    },
    copilot: {
      kind: 'sequence',
      steps: [
        { key: 'restartSession', command: '/restart' },
        { key: 'inspectEnvironment', command: '/env' },
      ],
    },
    cursor: {
      kind: 'sequence',
      steps: [{ key: 'inspectCursorHooks' }],
    },
    droid: {
      kind: 'sequence',
      steps: [{ key: 'openHooks', command: '/hooks' }],
    },
    gemini: {
      kind: 'sequence',
      steps: [{ key: 'listHooks', command: '/hooks list' }],
    },
    grok: {
      kind: 'sequence',
      steps: [{ key: 'inspectGrokConfig', command: 'grok inspect --json' }],
    },
    kimi: {
      kind: 'sequence',
      steps: [{ key: 'startSession', command: 'kimi' }],
    },
    kirocli: {
      kind: 'alternatives',
      steps: [
        { key: 'launchKiroV2', command: 'kiro-cli --agent elydora-audit', variant: 'V2' },
        { key: 'launchKiroV3', command: 'kiro-cli --v3', variant: 'V3' },
      ],
    },
    kiroide: {
      kind: 'sequence',
      steps: [{ key: 'inspectKiroIdeHooks' }],
    },
    letta: {
      kind: 'sequence',
      steps: [{ key: 'openHooks', command: '/hooks' }],
    },
    opencode: {
      kind: 'sequence',
      steps: [{ key: 'startSession', command: 'opencode' }],
    },
    qwen: {
      kind: 'sequence',
      steps: [{ key: 'listHooks', command: '/hooks list' }],
    },
  });
});

test('translates every activation step in English and Chinese', () => {
  const stepKeys = [...new Set(
    INTEGRATION_CATALOG.flatMap(({ postInstall }) => (
      postInstall?.steps.map(({ key }) => key) ?? []
    )),
  )].sort();

  expect(Object.keys(enRegistration.agentRegistration.postInstallSteps).sort()).toEqual(stepKeys);
  expect(Object.keys(zhRegistration.agentRegistration.postInstallSteps).sort()).toEqual(stepKeys);
});

test('generates every native hook command with the verified SDK flag contract', () => {
  for (const integration of INTEGRATION_CATALOG.filter(({ mode }) => mode === 'hooks')) {
    for (const language of SDK_LANGUAGES) {
      const instructions = buildInstallInstructions(language, {
        integration,
        identity: credentials,
        baseUrl: 'https://api.example.com',
        privateKey: 'private-contract-key',
        token: 'token-contract',
      });

      expect(instructions.setup).toContain(`--agent ${integration.id}`);
      expect(instructions.setup).toContain("'https://api.example.com'");
      expect(instructions.setup).toContain("printf '%s' 'k=$(mktemp \"$HOME/.elydora-agent-contract.XXXXXX\") && trap '\\''rm -f \"$k\"'\\'' EXIT INT TERM && t=$(mktemp \"$HOME/.elydora-agent-contract.XXXXXX\") && trap '\\''rm -f \"$k\" \"$t\"'\\'' EXIT INT TERM && ");
      expect(instructions.setup).toContain("printf '\\''%s\\n'\\'' '\\''private-contract-key'\\'' > \"$k\"");
      expect(instructions.setup).toContain("printf '\\''%s\\n'\\'' '\\''token-contract'\\'' > \"$t\"");
      expect(instructions.setup).not.toContain('chmod');
      expect(instructions.setup.endsWith("' | sh")).toBe(true);
      expect(instructions.secretDelivery).toBe('embedded');
      expect(instructions.postInstall).toEqual(integration.postInstall);

      if (language === 'go') {
        expect(instructions.setup).toContain("--org-id '\\''org-contract'\\''");
        expect(instructions.setup).toContain("--agent-id '\\''agent-contract'\\''");
        expect(instructions.setup).toContain("--base-url '\\''https://api.example.com'\\''");
        expect(instructions.setup).toContain('--private-key-file "$k" --token-file "$t"');
      } else {
        expect(instructions.setup).toContain("--org_id '\\''org-contract'\\''");
        expect(instructions.setup).toContain("--agent_id '\\''agent-contract'\\''");
        expect(instructions.setup).toContain("--base_url '\\''https://api.example.com'\\''");
        expect(instructions.setup).toContain('--private_key_file "$k" --token_file "$t"');
      }
    }
  }
});

test('generates PowerShell native hook commands with the same SDK flag contract', () => {
  for (const integration of INTEGRATION_CATALOG.filter(({ mode }) => mode === 'hooks')) {
    for (const language of SDK_LANGUAGES) {
      const instructions = buildInstallInstructions(language, {
        integration,
        identity: credentials,
        baseUrl: 'https://api.example.com',
        privateKey: 'private-contract-key',
        token: 'token-contract',
      }, 'powershell');

      expect(instructions.setup).toContain(`--agent ${integration.id}`);
      expect(instructions.setup).toContain("$k = Join-Path $HOME '.elydora-agent-contract.key'; $t = Join-Path $HOME '.elydora-agent-contract.token'; try { New-Item -ItemType File -Force -Path $k, $t | Out-Null; if ($IsLinux -or $IsMacOS) { chmod 600 $k $t; if ($LASTEXITCODE -ne 0) { throw \"chmod failed\" } } else { $u = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value; foreach ($f in $k, $t) { icacls $f /inheritance:r /grant:r \"*$($u):F\" | Out-Null; if ($LASTEXITCODE -ne 0) { throw \"icacls failed for $f\" } } }; Set-Content -LiteralPath $k -Value 'private-contract-key'; Set-Content -LiteralPath $t -Value 'token-contract'; ");
      expect(instructions.setup.endsWith('; if ($LASTEXITCODE -ne 0) { throw "elydora install exited with code $LASTEXITCODE" } } finally { Remove-Item -LiteralPath $k, $t -Force -ErrorAction SilentlyContinue }')).toBe(true);
      expect(instructions.secretDelivery).toBe('embedded');

      if (language === 'go') {
        expect(instructions.setup).toContain("--org-id 'org-contract' --agent-id 'agent-contract' --kid 'agent-contract-key-1' --base-url 'https://api.example.com' --private-key-file $k --token-file $t;");
      } else {
        expect(instructions.setup).toContain("--org_id 'org-contract' --agent_id 'agent-contract' --kid 'agent-contract-key-1' --base_url 'https://api.example.com' --private_key_file $k --token_file $t;");
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
      identity: credentials,
      baseUrl: 'https://api.example.com',
      privateKey: 'private-contract-key',
      token: 'token-contract',
    });
    expect(instructions.setup).toContain('agent-contract');
    expect(instructions.setup).toContain('ELYDORA_PRIVATE_KEY');
    expect(instructions.setup).toContain('ELYDORA_API_TOKEN');
    expect(instructions.setup).not.toContain('private-contract-key');
    expect(instructions.setup).not.toContain('token-contract');
    expect(instructions.usage).toContain('ai.tool_use');
    expect(instructions.verify).toBeUndefined();
    expect(instructions.secretDelivery).toBe('environment');
  }
});

test('quotes shell metacharacters and rejects unsupported command values', () => {
  const integration = INTEGRATION_CATALOG.find(({ id }) => id === 'grok');
  expect(integration).toBeDefined();

  const instructions = buildInstallInstructions('go', {
    integration: integration!,
    identity: {
      agentId: 'agent-contract',
      kid: 'agent-contract-key-1',
      orgId: '$(Write-Output injected)',
    },
    baseUrl: 'https://api.example.com/`whoami`',
    privateKey: 'private-contract-key',
    token: 'token-contract',
  });
  expect(instructions.setup).toContain("'$(Write-Output injected)'");
  expect(instructions.setup).toContain("'https://api.example.com/`whoami`'");

  expect(() => buildInstallInstructions('go', {
    integration: integration!,
    identity: { ...credentials, orgId: "org-'unsafe" },
    baseUrl: 'https://api.example.com',
    privateKey: 'private-contract-key',
    token: 'token-contract',
  })).toThrow('Organization ID cannot be represented');
  expect(() => buildInstallInstructions('node', {
    integration: integration!,
    identity: credentials,
    baseUrl: 'https://api.example.com',
    privateKey: "key-'unsafe",
    token: 'token-contract',
  })).toThrow('Private key cannot be represented');
  expect(() => buildInstallInstructions('node', {
    integration: integration!,
    identity: { ...credentials, agentId: 'agent contract' },
    baseUrl: 'https://api.example.com',
    privateKey: 'private-contract-key',
    token: 'token-contract',
  })).toThrow('Agent ID cannot be used as a credential file name.');
});

test('classifies malformed successful responses as blocking contract failures', () => {
  expect(() => assertRegistrationResponse(
    null as never,
    credentials,
    'grok',
  )).toThrow(RegistrationContractError);
});
