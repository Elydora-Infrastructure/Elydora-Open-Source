# Elydora Go SDK

Official Go SDK for the [Elydora](https://elydora.com) tamper-evident audit platform. Build cryptographically verifiable audit trails for AI agent operations.

## Installation

```bash
go get github.com/Elydora-Infrastructure/Elydora-Go-SDK
```

Requires Go 1.21+. The CLI uses focused TOML and JSONC parsers to preserve user-owned agent configuration.

## Quick Start

```go
package main

import (
	"fmt"
	"log"

	elydora "github.com/Elydora-Infrastructure/Elydora-Go-SDK"
)

func main() {
	// Initialize the client with your API token.
	// Obtain an API token by signing in via the Elydora console or:
	//   POST /api/auth/sign-in/email  ->  get session token
	//   POST /v1/auth/token           ->  exchange for long-lived API token
	client, err := elydora.NewClient(&elydora.Config{
		OrgID:      "org-123",
		AgentID:    "my-agent-id",
		PrivateKey: "<base64url-encoded-ed25519-seed>",
		Token:      "your-api-token",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create and submit an operation
	eor, err := client.CreateOperation(&elydora.CreateOperationParams{
		OperationType: "data.access",
		Subject:       map[string]interface{}{"user_id": "u-123"},
		Action:        map[string]interface{}{"type": "read"},
		Payload:       map[string]interface{}{"record_id": "rec-456"},
	})
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.SubmitOperation(eor)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Receipt: %s\n", resp.Receipt.ReceiptID)
}
```

## CLI

The SDK includes a CLI for installing audit hooks into AI coding agents.

Agent IDs map to one physical directory directly under `~/.elydora`; portable filename rules and physical directory/config checks apply before writes or recursive removal. Ambiguous uninstall discovery requires an explicit agent ID.

```bash
go install github.com/Elydora-Infrastructure/Elydora-Go-SDK/cmd/elydora@latest

elydora install \
  --agent claudecode \
  --org-id org-123 \
  --agent-id agent-456 \
  --private-key-file /secure/path/private.key \
  --token-file /secure/path/api.token \
  --kid agent-456-key-v1
```

Credential options may be omitted in an interactive terminal; the CLI then reads the private key and optional API token without terminal echo. Credential files must contain one UTF-8 line of at most 64 KiB. Unix credential files require owner-only permissions such as `chmod 600`. Installation commits `config.json`, `private.key`, and `hook.js` as one rollback-capable runtime transaction, and the generated scripts validate those protected files at execution time.

Codex performs a one-time trust review for user hooks. Run `/hooks` in Codex after installation and trust the Elydora `PreToolUse` and `PostToolUse` definitions.

Cursor installation writes native global `preToolUse`, `postToolUse`, and `postToolUseFailure` handlers to `~/.cursor/hooks.json`. Elydora preserves unrelated user hooks, leaves project and enterprise sources unchanged, emits valid JSON hook responses, fails closed when enforcement or audit execution cannot complete, and commits its 10-second hook contract with all managed runtime files as one rollback-capable transaction.

GitHub Copilot CLI installation writes native user hooks to `$COPILOT_HOME/hooks/elydora-audit.json` with `~/.copilot/hooks/elydora-audit.json` as the default. Elydora preserves user hooks, migrates exact Elydora-owned entries from project `.github/hooks/hooks.json`, quotes the resolved Node.js runtime for each shell, and propagates the freeze guard's exit code. Installation requires `disableAllHooks` to be false.

Cline installation writes `PreToolUse.mjs` and `PostToolUse.mjs` to `$CLINE_DIR/hooks` (default `~/.cline/hooks`). Elydora leaves the Documents and workspace hook roots unchanged. The wrappers preserve Cline's official payload and translate a frozen guard into JSON stdout cancellation control.

Kiro CLI installation covers both runtime contracts. Kiro CLI v2 uses the generated custom agent through `kiro-cli --agent elydora-audit`. Kiro CLI v3 loads the global standalone hooks when started with `kiro-cli --v3`.

Kimi installation follows detected runtime evidence. Kimi Code uses `$KIMI_CODE_HOME/config.toml` with `~/.kimi-code/config.toml` as the default. Legacy `kimi-cli` uses `~/.kimi/config.toml`; Elydora updates both contracts only when both runtimes are present.

Grok Build installation writes native global hooks to `$GROK_HOME/hooks/elydora-audit.json` (default `~/.grok/hooks/elydora-audit.json`). Project hooks still follow Grok's `/hooks-trust` workflow; Elydora leaves project, Claude Code, and Cursor compatibility files unchanged.

Auggie installation writes user-level hooks to `~/.augment/settings.json` and creates the `.cmd` or `.sh` wrappers required by its command runner. System and workspace settings remain unchanged. Sessions started with `--augment-cache-dir` load settings from that alternate directory.

Factory Droid installation selects `PreToolUse` and `PostToolUse` independently across `~/.factory/hooks.json`, the legacy `~/.factory/hooks/hooks.json`, and `~/.factory/settings.json`. Elydora preserves JSONC comments and formatting, commits runtime and hook changes as one transaction, and reports disabled hooks as unhealthy. Run `/hooks` in Droid after installation to review the effective hooks.

Qwen Code installation writes native user hooks to `$QWEN_HOME/settings.json` with `~/.qwen/settings.json` as the default. Elydora follows Qwen's `.env` discovery order, preserves JSON-with-comments source formatting, keeps workspace settings read-only, and commits runtime and settings changes as one transaction. Run `/hooks` in Qwen Code after installation to review the effective hooks.

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
| GitHub Copilot CLI | `copilot` |
| Cursor | `cursor` |
| Factory Droid | `droid` |
| Gemini CLI | `gemini` |
| Grok Build | `grok` |
| Kiro CLI | `kirocli` |
| Kiro IDE | `kiroide` |
| Kimi Code | `kimi` |
| Letta Code | `letta` |
| OpenCode | `opencode` |
| Qwen Code | `qwen` |

## API Reference

### Configuration

```go
client, err := elydora.NewClient(&elydora.Config{
	OrgID:      "org-123",       // Organization ID
	AgentID:    "agent-456",     // Agent ID
	PrivateKey: "<seed>",        // Base64url-encoded Ed25519 seed
	BaseURL:    "https://...",   // API base URL (default: https://api.elydora.com)
	TTLMs:      30000,           // Operation TTL in ms (default: 30000)
	MaxRetries: 3,               // Max retries on transient failures (default: 3)
	Token:      "<token>",        // API token for authenticated requests
})
```

### Client Methods

```go
// Update the API token at runtime
client.SetToken("new-api-token")
```

### Authentication

Authentication uses Better Auth. Register and sign in via the Elydora console or the Better Auth endpoints, then issue a long-lived API token for SDK use:

```go
// Sign up (Better Auth) — use the console or call directly:
//   POST /api/auth/sign-up/email  { email, password, name }
//
// Sign in (Better Auth) — get a session token:
//   POST /api/auth/sign-in/email  { email, password }
//
// Issue a long-lived API token from an active session:
tokenResp, err := client.IssueApiToken(&elydora.IssueApiTokenRequest{
	TTLSeconds: &ttlSeconds,
})

// Update the token on an existing client instance at runtime
client.SetToken("new-api-token")
```

### Operations

```go
// Create a signed EOR locally (no network call)
eor, err := client.CreateOperation(&elydora.CreateOperationParams{
	OperationType: "inference",
	Subject:       map[string]interface{}{"model": "gpt-4"},
	Action:        map[string]interface{}{"type": "completion"},
	Payload:       map[string]interface{}{"prompt": "Hello"},
	KID:           "agent-456-key-v1",
})

// Submit to API
resp, err := client.SubmitOperation(eor)

// Retrieve an operation
op, err := client.GetOperation(operationID)

// Verify integrity
result, err := client.VerifyOperation(operationID)
```

### Agent Management

```go
// Register a new agent
agent, err := client.RegisterAgent(&elydora.RegisterAgentRequest{
	AgentID:           "my-agent",
	DisplayName:       "My Agent",
	ResponsibleEntity: "team@example.com",
	IntegrationType:   elydora.IntegrationTypeSDK,
	Keys: []elydora.RegisterAgentKeyInput{
		{KID: "key-v1", PublicKey: "<base64url>", Algorithm: "ed25519"},
	},
})

// Get agent details
details, err := client.GetAgent(agentID)

// Freeze an agent
err := client.FreezeAgent(agentID, "security review")

// Revoke a key
err := client.RevokeKey(agentID, kid, "key rotation")

// List all agents in the organization
agents, err := client.ListAgents()

// Unfreeze a previously frozen agent
err := client.UnfreezeAgent(agentID, "review complete")

// Delete an agent permanently
deleted, err := client.DeleteAgent(agentID)
```

### Audit

```go
results, err := client.QueryAudit(&elydora.AuditQueryRequest{
	AgentID:       "agent-123",
	OperationType: "inference",
	StartTime:     &startTime,
	EndTime:       &endTime,
	Limit:         &limit,
})
```

### Epochs

```go
epochs, err := client.ListEpochs()
epoch, err := client.GetEpoch(epochID)
```

### Exports

```go
export, err := client.CreateExport(&elydora.CreateExportRequest{
	StartTime: startTime,
	EndTime:   endTime,
	Format:    "json",
})

exports, err := client.ListExports()
detail, err := client.GetExport(exportID)

// Download export file data
data, err := client.DownloadExport(exportID)
```

### JWKS

```go
jwks, err := client.GetJWKS()
```

### Health

```go
// Check API health (no authentication required, does not need a client)
health, err := elydora.Health("https://api.elydora.com")
// health.Status, health.Version, health.ProtocolVersion, health.Timestamp
```

### Constants

```go
// Genesis chain hash — initial prev_chain_hash for an agent's first operation
elydora.GenesisChainHash // "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
```

#### Status Enums

| Type | Constants |
|------|-----------|
| `AgentStatus` | `AgentStatusActive`, `AgentStatusFrozen`, `AgentStatusRevoked` |
| `KeyStatus` | `KeyStatusActive`, `KeyStatusRetired`, `KeyStatusRevoked` |
| `ExportStatus` | `ExportStatusQueued`, `ExportStatusRunning`, `ExportStatusDone`, `ExportStatusFailed` |
| `ExportFormat` | `ExportFormatJSON`, `ExportFormatPDF` |
| `RbacRole` | `RbacRoleOrgOwner`, `RbacRoleSecurityAdmin`, `RbacRoleComplianceAuditor`, `RbacRoleReadonlyInvestigator`, `RbacRoleIntegrationEngineer` |
| `ErrorCode` | `ErrorCodeInvalidSignature`, `ErrorCodeUnknownAgent`, `ErrorCodeKeyRevoked`, `ErrorCodeAgentFrozen`, `ErrorCodeTTLExpired`, `ErrorCodeReplayDetected`, `ErrorCodePrevHashMismatch`, `ErrorCodePayloadTooLarge`, `ErrorCodeRateLimited`, `ErrorCodeInternalError`, `ErrorCodeUnauthorized`, `ErrorCodeForbidden`, `ErrorCodeNotFound`, `ErrorCodeValidationError` |

## Error Handling

```go
import "errors"

var apiErr *elydora.ElydoraError
if errors.As(err, &apiErr) {
	fmt.Println(apiErr.Code)       // e.g. "INVALID_SIGNATURE"
	fmt.Println(apiErr.Message)    // Human-readable message
	fmt.Println(apiErr.StatusCode) // HTTP status code
	fmt.Println(apiErr.RequestID)  // Request ID for support
}
```

## License

MIT
