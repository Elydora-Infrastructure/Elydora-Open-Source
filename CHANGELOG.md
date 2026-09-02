# Changelog

All notable changes to the Elydora Open Source project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed
- Console registration wizard synced with the hosted Console: native hook catalog with activation steps, a one-command install that embeds both credentials in owner-only files (created exclusively with `mktemp` on POSIX; created under `$HOME` and protected with `chmod 600` or an `icacls` owner-only ACL in PowerShell) and deletes them afterwards, and a never-expiring default API token.
- Server issues opaque API tokens: `POST /v1/auth/token` honours `ttl_seconds` (`null` never expires), stores only SHA-256 hashes (migration 003), and resolves bearer values as API tokens before Better Auth sessions.
- Server commits operations before enqueueing; a `seq_no` race now returns `400 PREV_HASH_MISMATCH` with the stored chain hash, and every queue producer names its durable message id.
- SDK mirrors rebuilt from the released SDKs: Node 2.0.1, Python 2.0.2, Go v2.0.1 under `github.com/Elydora-Infrastructure/Elydora-Go-SDK/v2` (hook retry on `PREV_HASH_MISMATCH` within a 4-second budget, durable managed-file transactions, Kiro IDE adapter).

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
