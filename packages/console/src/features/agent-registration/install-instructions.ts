import type { IntegrationCatalogItem } from './integrations';

export const SDK_LANGUAGES = ['node', 'python', 'go'] as const;
export type SdkLanguage = (typeof SDK_LANGUAGES)[number];
type SecretDelivery = 'hidden-prompts' | 'environment';

interface AgentInstallIdentity {
  readonly agentId: string;
  readonly kid: string;
  readonly orgId: string;
}

interface InstructionInput {
  readonly integration: IntegrationCatalogItem;
  readonly identity: AgentInstallIdentity;
  readonly baseUrl: string;
}

export interface InstallInstructions {
  readonly setup: string;
  readonly usage?: string;
  readonly verify?: string;
  readonly postInstall?: readonly string[];
  readonly secretDelivery: SecretDelivery;
}

function shellQuoted(value: string, label: string): string {
  if (!value || /['\u0000-\u001f\u007f]/u.test(value)) {
    throw new Error(`${label} cannot be represented in the generated shell command.`);
  }
  return `'${value}'`;
}

function adapterCommand(
  language: SdkLanguage,
  { integration, identity, baseUrl }: InstructionInput,
): string {
  const values = {
    agent: integration.id,
    orgId: shellQuoted(identity.orgId, 'Organization ID'),
    agentId: shellQuoted(identity.agentId, 'Agent ID'),
    kid: shellQuoted(identity.kid, 'Key ID'),
    baseUrl: shellQuoted(baseUrl, 'API base URL'),
  };

  if (language === 'go') {
    return [
      'go install github.com/Elydora-Infrastructure/Elydora-Go-SDK/cmd/elydora@latest',
      `elydora install --agent ${values.agent} --org-id ${values.orgId} --agent-id ${values.agentId} --kid ${values.kid} --base-url ${values.baseUrl}`,
    ].join('\n');
  }

  const executable = language === 'node'
    ? 'npx @elydora/sdk install'
    : 'python -m pip install elydora\nelydora install';
  return `${executable} --agent ${values.agent} --org_id ${values.orgId} --agent_id ${values.agentId} --kid ${values.kid} --base_url ${values.baseUrl}`;
}

function postInstallSteps(integration: IntegrationCatalogItem): readonly string[] | undefined {
  if (integration.postInstall === 'review-hooks') {
    return ['Open the agent and run /hooks to review the active Elydora hooks.'];
  }
  if (integration.postInstall === 'start-kiro') {
    return [
      'Kiro CLI v2: kiro-cli --agent elydora-audit',
      'Kiro CLI v3: kiro-cli --v3',
    ];
  }
  return undefined;
}

function sdkSetup(language: SdkLanguage, input: InstructionInput): string {
  const { identity, baseUrl } = input;
  if (language === 'python') {
    return `python -m pip install elydora

import os

from elydora import ElydoraClient

def require_env(name: str) -> str:
    value = os.environ.get(name)
    if not value:
        raise RuntimeError(f"{name} is required")
    return value

client = ElydoraClient(
    org_id=${JSON.stringify(identity.orgId)},
    agent_id=${JSON.stringify(identity.agentId)},
    private_key=require_env("ELYDORA_PRIVATE_KEY"),
    base_url=${JSON.stringify(baseUrl)},
    token=require_env("ELYDORA_API_TOKEN"),
)
client.set_kid(${JSON.stringify(identity.kid)})`;
  }

  if (language === 'go') {
    return `go get github.com/Elydora-Infrastructure/Elydora-Go-SDK

requireEnv := func(name string) string {
    value, ok := os.LookupEnv(name)
    if !ok || value == "" {
        panic(name + " is required")
    }
    return value
}

client, err := elydora.NewClient(&elydora.Config{
    OrgID: ${JSON.stringify(identity.orgId)},
    AgentID: ${JSON.stringify(identity.agentId)},
    PrivateKey: requireEnv("ELYDORA_PRIVATE_KEY"),
    BaseURL: ${JSON.stringify(baseUrl)},
    Token: requireEnv("ELYDORA_API_TOKEN"),
})
if err != nil {
    panic(err)
}`;
  }

  return `npm install @elydora/sdk

import { ElydoraClient } from '@elydora/sdk';

const privateKey = process.env.ELYDORA_PRIVATE_KEY;
const token = process.env.ELYDORA_API_TOKEN;
if (!privateKey || !token) {
  throw new Error('ELYDORA_PRIVATE_KEY and ELYDORA_API_TOKEN are required');
}

const client = new ElydoraClient({
  orgId: ${JSON.stringify(identity.orgId)},
  agentId: ${JSON.stringify(identity.agentId)},
  privateKey,
  kid: ${JSON.stringify(identity.kid)},
  baseUrl: ${JSON.stringify(baseUrl)},
});
client.setToken(token);`;
}

function sdkUsage(language: SdkLanguage): string {
  if (language === 'python') {
    return `operation = client.create_operation(
    operation_type="ai.tool_use",
    subject={"session_id": "session-123"},
    action={"tool": "shell"},
    payload={"command": "example"},
)
receipt = client.submit_operation(operation)`;
  }

  if (language === 'go') {
    return `operation, err := client.CreateOperation(&elydora.CreateOperationParams{
    OperationType: "ai.tool_use",
    Subject: map[string]any{"session_id": "session-123"},
    Action: map[string]any{"tool": "shell"},
    Payload: map[string]any{"command": "example"},
})
if err != nil {
    panic(err)
}
_, err = client.SubmitOperation(operation)
if err != nil {
    panic(err)
}`;
  }

  return `const operation = client.createOperation({
  operationType: 'ai.tool_use',
  subject: { session_id: 'session-123' },
  action: { tool: 'shell' },
  payload: { command: 'example' },
});
await client.submitOperation(operation);`;
}

export function buildInstallInstructions(
  language: SdkLanguage,
  input: InstructionInput,
): InstallInstructions {
  if (input.integration.mode === 'adapter') {
    return {
      setup: adapterCommand(language, input),
      verify: language === 'node' ? 'npx @elydora/sdk status' : 'elydora status',
      postInstall: postInstallSteps(input.integration),
      secretDelivery: 'hidden-prompts',
    };
  }

  return {
    setup: sdkSetup(language, input),
    usage: sdkUsage(language),
    secretDelivery: 'environment',
  };
}
