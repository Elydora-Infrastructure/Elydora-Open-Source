# Elydora Open Source Engineering Contract

## Scope

This repository publishes the open-source server, console, integration catalog, and distributable Node.js, Python, and Go SDK mirrors.

## Sources of Truth

- `integrations/catalog.json` owns provider IDs, official hook contracts, blocking semantics, release variants, and adapter delivery state.
- Official provider documentation owns every external hook API fact.
- The standalone SDK repositories own released package behavior. Mirror a reviewed standalone commit here without semantic drift.
- Backend schemas own persisted data contracts. Shared types and Console consumers must follow those schemas.

## Integration Invariants

- Verify high-drift provider contracts against current official documentation before changing the catalog or an adapter.
- Preserve unrelated user configuration and remove only Elydora-owned entries.
- Parse every affected user configuration before the first write.
- Surface malformed or unreadable configuration with contextual errors and leave the original file intact.
- Use atomic same-directory writes for provider configuration.
- Forward official hook JSON from STDIN without reshaping provider fields.
- Use each provider's documented blocking mechanism and failure mode.
- Report installation as healthy only when a complete contract references all required runtime files.
- Keep stable, legacy, and early-access contracts explicit, including their activation commands.
- Select Kimi Code and legacy `kimi-cli` contracts from runtime evidence. An empty `KIMI_CODE_HOME` uses `~/.kimi-code`; create no cross-runtime migration marker.
- Preserve Kimi TOML comments and unrelated formatting through range-based edits, then parse the complete rendered document before writing it.
- Write Grok Build integrations to a native user hook file under non-empty `GROK_HOME` or `~/.grok`; keep Claude Code and Cursor compatibility sources read-only.
- Write Auggie hooks only to `~/.augment/settings.json`; keep system and workspace settings read-only. Generate `.cmd` wrappers on Windows and `.sh` wrappers on Unix because Auggie dispatches supported script paths, and express hook timeouts in milliseconds.
- Validate Auggie matcher syntax during installation with Node.js `new RegExp`; keep status and uninstall independent from the JavaScript validator so recovery remains available offline.

## Mirror Workflow

1. Complete and push the standalone SDK change with focused and full release gates.
2. Confirm the pre-change mirror blob matches the standalone parent commit.
3. Mirror source, registry, README guidance, and executable regression tests.
4. Confirm the mirrored source and tests match the standalone commit where repository-specific differences are absent.
5. Run the SDK's focused and full gates plus `npm run validate:integrations` at the repository root.
6. Commit and push one root issue before starting the next mirror or product surface.

## Code Quality

- Keep production source files at or below 500 lines.
- Keep functions focused on one ownership boundary.
- Propagate unexpected errors to the CLI or request boundary.
- Keep technical claims executable through tests or generated validation.
- Avoid compatibility shims without a named public contract.
- Preserve the minimum runtime versions declared by each package.

## Verification

Node SDK:

```powershell
cd sdks/node
npm run build
node --test test/<provider>-plugin.test.mjs
npm test
npm audit --omit=dev --audit-level=high
npm pack --dry-run --json
```

Python SDK:

```powershell
cd sdks/python
py -3 -m pytest tests/test_<provider>_plugin.py -q
py -3 -m pytest -q
py -3 -m mypy elydora
py -3 -m pip check
```

Go SDK:

```powershell
cd sdks/go
go test ./cmd/elydora/plugins -run <Provider> -count=1
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

Repository catalog:

```powershell
npm run validate:integrations
git diff --check
```
