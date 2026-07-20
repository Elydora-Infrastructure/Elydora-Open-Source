# Elydora Node SDK

Official Node.js/TypeScript SDK for the [Elydora](https://elydora.com) tamper-evident audit platform. Build cryptographically verifiable audit trails for AI agent operations.

## Installation

```bash
npm install @elydora/sdk
```

Requires Node.js 18+ (uses built-in `crypto` module with Ed25519 support).

## Quick Start

```typescript
import { ElydoraClient } from '@elydora/sdk';

// Initialize the client with your API token.
// Obtain an API token by signing in via the Elydora console or:
//   POST /api/auth/sign-in/email  →  get session token
//   POST /v1/auth/token           →  exchange for long-lived API token
const client = new ElydoraClient({
  orgId: 'org-123',
  agentId: 'my-agent-id',
  privateKey: '<base64url-encoded-32-byte-ed25519-seed>',
  baseUrl: 'https://api.elydora.com',
  token: 'your-api-token',
});

// Create and submit an operation
const eor = client.createOperation({
  operationType: 'data.access',
  subject: { user_id: 'u-123', resource: 'patient-record' },
  action: { type: 'read', scope: 'full' },
  payload: { record_id: 'rec-456' },
});

const { receipt } = await client.submitOperation(eor);
console.log('Operation submitted:', receipt.operation_id);
```

## CLI

The SDK includes a CLI for installing audit hooks into AI coding agents.

```bash
npx elydora install \
  --agent claudecode \
  --org_id org-123 \
  --agent_id agent-456 \
  --kid agent-456-key-v1
```

The CLI reads the private key and optional API token through hidden terminal prompts. For non-interactive installation, store each secret in an owner-only file and pass `--private_key_file <path>` and `--token_file <path>`. Secret values are rejected as command-line arguments because process listings and shell history can expose them. Agent IDs map to one physical directory directly under `~/.elydora`; portable filename rules and physical-directory checks apply before writes or recursive removal. Ambiguous uninstall discovery requires an explicit agent ID.

Codex performs a one-time trust review for user hooks. Run `/hooks` in Codex after installation and trust the Elydora `PreToolUse` and `PostToolUse` definitions.

Cline installation writes `PreToolUse.mjs` and `PostToolUse.mjs` to `$CLINE_DIR/hooks` (default `~/.cline/hooks`). Elydora leaves the Documents and workspace hook roots unchanged. The guard translates a frozen agent into Cline's JSON stdout cancellation control.

Factory Droid installation preserves the active user source across `~/.factory/hooks.json`, the `hooks` field in `~/.factory/settings.json`, and the legacy `~/.factory/hooks/hooks.json` path. Hook files store events at the document root; settings stores the same event map under `hooks`. Project and organization sources remain unchanged. Droid snapshots hooks for each session, so run `/hooks` after installation to review and apply the external change.

Kimi installation writes the strict hook contract to each detected runtime: Kimi Code's `$KIMI_CODE_HOME/config.toml` (default `~/.kimi-code/config.toml`) and the migrating Python CLI's `~/.kimi/config.toml`. A fresh installation targets current Kimi Code, avoiding cross-runtime migration markers. Both runtimes load the hooks globally; run `/hooks` to inspect them.

Grok Build installation writes native global hooks to `$GROK_HOME/hooks/elydora-audit.json` (default `~/.grok/hooks/elydora-audit.json`). Project hooks still follow Grok's `/hooks-trust` workflow; Elydora leaves project, Claude Code, and Cursor compatibility files unchanged.

Qwen Code installation writes user hooks to `$QWEN_HOME/settings.json` (default `~/.qwen/settings.json`). User-level `.qwen/.env` takes precedence over `~/.env` when it defines `QWEN_HOME`; explicit process environment values take highest precedence. Workspace settings remain unchanged. Run `/hooks` to review the definitions. `disableAllHooks` and `--safe-mode` suspend hook execution.

Auggie installation writes user-level hooks to `~/.augment/settings.json` and creates the `.cmd` or `.sh` wrappers required by its command runner. System and workspace settings remain unchanged. Sessions started with `--augment-cache-dir` load settings from that alternate directory.

Kiro CLI installation covers both runtime contracts. Kiro CLI v2 uses the generated custom agent through `kiro-cli --agent elydora-audit`. Kiro CLI v3 loads the global standalone hooks when started with `kiro-cli --v3`.

### Commands

| Command | Description |
|---------|-------------|
| `elydora install` | Install Elydora audit hook for a coding agent |
| `elydora uninstall` | Remove Elydora audit hook for a coding agent |
| `elydora status` | Show installation status for all agents |
| `elydora agents` | List supported coding agents |

### Supported Agents

| Agent | Key |
|-------|-----|
| Augment Code CLI | `augment` |
| Claude Code | `claudecode` |
| OpenAI Codex | `codex` |
| Cline | `cline` |
| Factory Droid | `droid` |
| Kimi Code | `kimi` |
| Grok Build | `grok` |
| Qwen Code | `qwen` |
| Cursor | `cursor` |
| Gemini CLI | `gemini` |
| Kiro CLI | `kirocli` |
| Kiro IDE | `kiroide` |
| OpenCode | `opencode` |
| GitHub Copilot CLI | `copilot` |
| Letta Code | `letta` |

## API Reference

### Configuration

```typescript
const client = new ElydoraClient({
  orgId: string,        // Organization ID
  agentId: string,      // Agent ID
  privateKey: string,   // Base64url-encoded Ed25519 seed
  baseUrl?: string,     // API base URL (default: https://api.elydora.com)
  ttlMs?: number,       // Operation TTL in ms (default: 30000)
  maxRetries?: number,  // Max retries on transient failures (default: 3)
  kid?: string,         // Key ID (default: {agentId}-key-v1)
});
```

### Authentication

```typescript
// Register a new user and organization
const reg = await ElydoraClient.register(baseUrl, email, password, displayName?, orgName?);

// Login and receive a session token
const auth = await ElydoraClient.login(baseUrl, email, password);

// Set token on client
client.setToken(auth.token);

// Get current authenticated user profile
const { user } = await client.getMe();

// Issue a new API token (with optional TTL in seconds)
const { token, expires_at } = await client.issueApiToken(3600);
```

### Operations

```typescript
// Create a signed EOR locally (synchronous, no network call)
const eor = client.createOperation({
  operationType: 'inference',
  subject: { model: 'gpt-4' },
  action: { type: 'completion' },
  payload: { prompt: 'Hello' },
});

// Submit to API
const { receipt } = await client.submitOperation(eor);

// Retrieve an operation
const op = await client.getOperation(operationId);

// Verify integrity (signature, chain, receipt, merkle)
const result = await client.verifyOperation(operationId);
```

### Agent Management

```typescript
// Register a new agent
const agent = await client.registerAgent({
  agent_id: 'my-agent',
  integration_type: 'codex',
  display_name: 'My Agent',
  responsible_entity: 'team@example.com',
  keys: [{ kid: 'key-v1', public_key: '<base64url>', algorithm: 'ed25519' }],
});

// Get agent details
const details = await client.getAgent(agentId);

// Freeze an agent
await client.freezeAgent(agentId, 'security review');

// Revoke a key
await client.revokeKey(agentId, kid, 'key rotation');

// List all agents in the organization
const { agents } = await client.listAgents();

// Unfreeze a previously frozen agent
await client.unfreezeAgent(agentId, 'review complete');

// Delete an agent permanently
const { deleted } = await client.deleteAgent(agentId);
```

### Audit

```typescript
const results = await client.queryAudit({
  agent_id: 'agent-123',
  operation_type: 'inference',
  start_time: Date.now() - 86400000,
  end_time: Date.now(),
  limit: 50,
});
```

### Epochs

```typescript
const { epochs } = await client.listEpochs();
const { epoch } = await client.getEpoch(epochId);
```

### Exports

```typescript
const { export: exp } = await client.createExport({
  start_time: Date.now() - 86400000,
  end_time: Date.now(),
  format: 'json',
});

const { exports } = await client.listExports();
const { export: detail, download_url } = await client.getExport(exportId);

// Download export file data
const data = await client.downloadExport(exportId);
```

### JWKS

```typescript
const { keys } = await client.getJWKS();
```

### Health

```typescript
// Check API health (no authentication required)
const health = await client.health();
// health.status, health.version, health.protocol_version, health.timestamp
```

### Client State

```typescript
// Get the current chain hash (useful for debugging/inspection)
const chainHash = client.getChainHash();

// Get the Ed25519 public key derived from the configured private key
const publicKey = client.getPublicKey();
```

### Crypto Utilities

The SDK exports low-level cryptographic primitives for advanced use:

```typescript
import {
  jcsCanonicalise,     // RFC 8785 JSON Canonicalization
  sha256Base64url,     // SHA-256 hash as base64url
  computeChainHash,    // Chain hash computation
  computePayloadHash,  // Payload hash computation
  signEd25519,         // Ed25519 signing
  derivePublicKey,     // Derive public key from private seed
  ZERO_CHAIN_HASH,     // Genesis chain hash constant
} from '@elydora/sdk';
```

### Utility Functions

```typescript
import {
  uuidv7,           // Generate a UUIDv7 (time-ordered, RFC 9562)
  generateNonce,     // Generate a 16-byte random nonce (base64url)
  base64urlEncode,   // Encode Buffer/Uint8Array to base64url (no padding)
  base64urlDecode,   // Decode base64url string to Buffer
} from '@elydora/sdk';
```

## Error Handling

```typescript
import { ElydoraError } from '@elydora/sdk';

try {
  await client.submitOperation(eor);
} catch (err) {
  if (err instanceof ElydoraError) {
    console.error(err.code);       // e.g. 'INVALID_SIGNATURE'
    console.error(err.message);    // Human-readable message
    console.error(err.statusCode); // HTTP status code
    console.error(err.requestId);  // Request ID for support
  }
}
```

## License

MIT
