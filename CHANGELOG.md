# Changelog

All notable changes to the Elydora Open Source project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed
- Console registration wizard synced with the hosted Console: native hook catalog with activation steps, a one-command install that embeds both credentials in owner-only files (created exclusively with `mktemp` on POSIX; created under `$HOME` and protected with `chmod 600` or an `icacls` owner-only ACL in PowerShell) and deletes them afterwards, and a never-expiring default API token.
- Server issues opaque API tokens: `POST /v1/auth/token` honours `ttl_seconds` (`null` never expires), stores only SHA-256 hashes (migration 003), and resolves bearer values as API tokens before Better Auth sessions.
- Server commits operations before enqueueing; a `seq_no` race now returns `400 PREV_HASH_MISMATCH` with the stored chain hash, and every queue producer names its durable message id.
- SDK mirrors rebuilt from the standalone repositories at Node 2.1.0, Python 2.1.0, and Go v2.1.0 plus the unreleased fixes listed below; npm serves 2.0.1 and PyPI serves 2.0.2 until the next publish. The mirrors live under `github.com/Elydora-Infrastructure/Elydora-Go-SDK/v2` (hook retry on `PREV_HASH_MISMATCH` within a 4-second budget, durable managed-file transactions, Kiro IDE adapter, shared managed-runtime modules).
- Compose and Helm apply `packages/server/migrations` through one runner (`node dist/migrate.js`); migrations already recorded in `schema_versions` are skipped. Migration 004 restores the status constraints, the agent organization foreign key, and the 64-bit counters that the previous inline Compose schema carried.
- Server emits `AGENT_REVOKED` and `KEY_RETIRED` instead of reusing `AGENT_FROZEN` and `KEY_REVOKED`, matching the hosted API and the protocol specification.

### Fixed
- SDK `deepHealth()` returns the degraded report that the API sends with HTTP 503 instead of raising.
- SDKs no longer replay non-idempotent requests after a 429, a 5xx, or a lost connection, so a retried token rotation cannot discard the replacement token.
- `RotateApiTokenResponse` carries `previous_token_grace_until`, and agent IDs are percent-encoded in request paths.

## [0.1.0] - 2025-03-02

### Added
- Initial open-source release of Elydora
- API server with Ed25519 signed operation records (EOR)
- Chain-hashed audit trails with SHA-256
- Merkle tree epoch rollups with RFC 3161 TSA anchoring
- Multi-tenant RBAC with 5 roles
- Compliance exports (JSON/PDF)
- Next.js web management console
- Node.js/TypeScript SDK (v1.2.0) with CLI
- Python SDK (v1.2.0) with sync and async clients
- Go SDK (v0.1.0) with CLI
- Docker Compose one-command deploy
- Kubernetes Helm chart
- CONTRIBUTING.md with development setup guide
