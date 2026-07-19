import type { IntegrationCatalogItem } from './integrations';
import type { AgentCredentials } from './credentials';

export const SDK_LANGUAGES = ['node', 'python', 'go'] as const;
export type SdkLanguage = (typeof SDK_LANGUAGES)[number];

interface InstructionInput {
  readonly integration: IntegrationCatalogItem;
  readonly credentials: AgentCredentials;
  readonly token: string;
  readonly baseUrl: string;
}

export interface InstallInstructions {
  readonly setup: string;
  readonly usage?: string;
  readonly verify?: string;
  readonly postInstall?: readonly string[];
}

function quoted(value: string): string {
  return `"${value.replaceAll('"', '\\"')}"`;
}

function adapterCommand(
  language: SdkLanguage,
  { integration, credentials, token, baseUrl }: InstructionInput,
): string {
  const values = {
    agent: integration.id,
    orgId: quoted(credentials.orgId),
    agentId: quoted(credentials.agentId),
    privateKey: quoted(credentials.privateKey),
    kid: quoted(credentials.kid),
    token: quoted(token),
    baseUrl: quoted(baseUrl),
  };

  if (language === 'go') {
    return [
      'go install github.com/Elydora-Infrastructure/Elydora-Go-SDK/cmd/elydora@latest',
      `elydora install --agent ${values.agent} --org-id ${values.orgId} --agent-id ${values.agentId} --private-key ${values.privateKey} --kid ${values.kid} --token ${values.token} --base-url ${values.baseUrl}`,
    ].join('\n');
  }

  const executable = language === 'node'
    ? 'npx @elydora/sdk install'
    : 'python -m pip install elydora\nelydora install';
  return `${executable} --agent ${values.agent} --org_id ${values.orgId} --agent_id ${values.agentId} --private_key ${values.privateKey} --kid ${values.kid} --token ${values.token} --base_url ${values.baseUrl}`;
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
  const { credentials, token, baseUrl } = input;
  if (language === 'python') {
    return `python -m pip install elydora

from elydora import ElydoraClient

client = ElydoraClient(
    org_id=${JSON.stringify(credentials.orgId)},
    agent_id=${JSON.stringify(credentials.agentId)},
    private_key=${JSON.stringify(credentials.privateKey)},
    base_url=${JSON.stringify(baseUrl)},
    token=${JSON.stringify(token)},
)
client.set_kid(${JSON.stringify(credentials.kid)})`;
  }

  if (language === 'go') {
    return `go get github.com/Elydora-Infrastructure/Elydora-Go-SDK

client, err := elydora.NewClient(&elydora.Config{
    OrgID: ${JSON.stringify(credentials.orgId)},
    AgentID: ${JSON.stringify(credentials.agentId)},
    PrivateKey: ${JSON.stringify(credentials.privateKey)},
    BaseURL: ${JSON.stringify(baseUrl)},
    Token: ${JSON.stringify(token)},
})
if err != nil {
    panic(err)
}`;
  }

  return `npm install @elydora/sdk

import { ElydoraClient } from '@elydora/sdk';

const client = new ElydoraClient({
  orgId: ${JSON.stringify(credentials.orgId)},
  agentId: ${JSON.stringify(credentials.agentId)},
  privateKey: ${JSON.stringify(credentials.privateKey)},
  kid: ${JSON.stringify(credentials.kid)},
  baseUrl: ${JSON.stringify(baseUrl)},
});
client.setToken(${JSON.stringify(token)});`;
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
    };
  }

  return {
    setup: sdkSetup(language, input),
    usage: sdkUsage(language),
  };
}
