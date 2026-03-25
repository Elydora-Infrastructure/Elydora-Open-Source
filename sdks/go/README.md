# Elydora Go SDK

Official Go SDK for the [Elydora](https://elydora.com) tamper-evident audit platform. Build cryptographically verifiable audit trails for AI agent operations.

## Installation

```bash
go get github.com/Elydora-Infrastructure/Elydora-Go-SDK
```

Requires Go 1.21+. Zero third-party dependencies (stdlib only).

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

```bash
go install github.com/Elydora-Infrastructure/Elydora-Go-SDK/cmd/elydora@latest

elydora install \
  --agent claudecode \
  --org-id org-123 \
  --agent-id agent-456 \
  --private-key <key> \
  --kid agent-456-key-v1
```

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
| Claude Code | `claudecode` |
| Copilot CLI | `copilot` |
| Cursor | `cursor` |
| Gemini CLI | `gemini` |
| Kiro CLI | `kirocli` |
| Kiro IDE | `kiroide` |
| Letta Code | `letta` |
| OpenCode | `opencode` |

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
