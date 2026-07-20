# Elydora Open Source Engineering Contract

## Scope

This repository publishes the open-source server, console, integration catalog, and distributable Node.js, Python, and Go SDK mirrors.

## Sources of Truth

- `integrations/catalog.json` owns provider IDs, official hook contracts, blocking semantics, release variants, and adapter delivery state.
- The server exports that catalog as `INTEGRATION_TYPES`; registration requires an explicit member before any database access.
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
- Write Cline file hooks to `$CLINE_DIR/hooks` with `~/.cline/hooks` as the default; keep Documents and workspace hook roots read-only. Use supported event filenames, preserve official hook input byte-for-byte, use `process.execPath` in Node and Go wrappers, and resolve Python runtimes from absolute shebangs. Translate guard exit code `2` into JSON stdout with `cancel: true`. Treat hook errors, timeouts, and invalid control JSON as fail-open.
- Resolve Codex user hooks through `$CODEX_HOME/hooks.json` with `~/.codex/hooks.json` as the default, matching Codex's existing-directory canonicalization rule. Model user JSON, inline TOML layers, project files, managed `requirements.toml`, and plugin-bundled hooks as additive stable sources. SDK adapters write only the user JSON source, register exact `PreToolUse` and `PostToolUse` match-all command groups, preserve the complete native payload, propagate freeze and revocation through exit code `2`, keep guard lookup and audit delivery fail-open with observable errors, commit user hooks and all four runtime artifacts through one rollback-capable transaction, and require `/hooks` trust review for both definition hashes.
- Write GitHub Copilot CLI hooks only to `$COPILOT_HOME/hooks/elydora-audit.json` with `~/.copilot/hooks/elydora-audit.json` as the default. Migrate exact Elydora-owned entries from project `.github/hooks/hooks.json`, preserve native camelCase input, require `disableAllHooks` to be false, and commit runtime plus hook documents in one rollback-capable transaction.
- Model Cursor as one CLI-and-IDE hook contract. Preserve user, project, and enterprise sources in documented priority order; forward `tool_name`, `tool_input`, and `conversation_id`; use exit code `2` for denial; record `failClosed` as the per-script failure-mode control.
- Write Node Cursor hooks only to `~/.cursor/hooks.json`; keep project and enterprise sources read-only. Register `preToolUse`, `postToolUse`, and `postToolUseFailure`, preserve the complete native payload, emit valid native JSON responses, retain PowerShell exit codes, and commit guard, runtime metadata, private key, audit runtime, and user hooks in one rollback-capable transaction.
- Write Go Cursor hooks only to `~/.cursor/hooks.json`; keep project and enterprise sources read-only. Preserve user hooks, migrate the prior versionless Elydora contract, audit successful and failed tool calls, emit valid native JSON responses, retain PowerShell exit codes, and commit guard, runtime metadata, private key, audit runtime, and user hooks in one rollback-capable transaction.
- Write Python Cursor hooks only to `~/.cursor/hooks.json`; keep project and enterprise sources read-only. Preserve user hooks, migrate the prior versionless Elydora contract, and own exact native `preToolUse`, `postToolUse`, and `postToolUseFailure` commands. Forward native success and failure payloads, retain PowerShell exit code `2`, set every handler to `failClosed` with a bounded timeout, and commit the guard, audit script, runtime identity, private key, and hook configuration as one rollback-capable transaction.
- Preserve Factory Droid's scope-root `hooks.json` precedence, legacy nested hook source, and per-event `settings.json` fallback. Scope-root and legacy files store event keys at the document root; settings stores the same map under `hooks`. Edit only effective user sources; keep project and organization sources read-only. Preserve JSONC comments through syntax-tree edits, use SDK-native absolute runtime commands, commit generated runtime artifacts and hook sources as one recoverable transaction, and require `/hooks` review after external changes.
- Qwen Code `0.20.0` resolves user settings through explicit `QWEN_HOME`, then user-level `.qwen/.env`, then `~/.env`, with `~/.qwen/settings.json` as the default. Keep workspace settings read-only. Preserve JSON comments, reject trailing commas and duplicate keys before writes, express timeouts in milliseconds, propagate native exit code `2` through PowerShell, and require `/hooks` review.
- Commit Qwen Code runtime config, private key, audit runtime, and user settings in one rollback-capable transaction. Status requires enabled `PreToolUse` and `PostToolUse` hooks with matching managed runtime files.

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
- Keep the mirrored Python package, distribution, and CLI version in `sdks/python/elydora/_version.py`; Setuptools metadata must read that literal dynamically.
- Ship `sdks/python/elydora/py.typed` in every mirrored wheel and verify public annotations from an installed-wheel consumer.
- Interpret Python SDK `max_retries` as retries after the initial attempt and reject negative or non-integer values.
- Retry RFC-idempotent Python SDK requests automatically; retry non-idempotent requests only when the transport proves the request was never sent.
- Honor valid `Retry-After` delay-seconds and HTTP-date values, and release retryable responses before sleeping.
- Avoid compatibility shims without a named public contract.
- Preserve the minimum runtime versions declared by each package.
- Keep private keys and API tokens out of process arguments and generated setup commands. Accept them through hidden terminal input or owner-only credential files.
- Keep Console instruction generation limited to agent identity and public configuration. Adapter commands use hidden CLI prompts; direct SDK examples read named runtime environment variables. Expose credentials through explicit copy controls only.
- Persist credential-bearing files through owner-only same-directory temporary files and atomic rename.
- Read runtime config, private keys, status cache, chain state, and error logs through physical-file descriptors with identity checks. Write cache and validated chain state atomically, and append error logs through no-follow owner-only descriptors. Preserve rollback artifacts when recovery cannot safely restore an original file and include the recovery path in the surfaced error.
- Go CLI install credentials come from terminal-echo-disabled input or physical owner-only files containing one UTF-8 line of at most 64 KiB. Runtime config, private key, and audit script form one rollback-capable transaction, and generated runtimes validate file identity, size, and Unix permissions before reads.
- Resolve every agent runtime directory as one physical child of `~/.elydora`; reject separators, traversal segments, cross-platform reserved names, symbolic-link directories, and linked identity configs before writes or recursive removal. Validate stored directory identity before changing host CLI configuration, and require an explicit agent ID when discovery is ambiguous.

## Verification

Server:

```powershell
cd packages/server
npm test
npm run typecheck
npm audit --omit=dev --audit-level=high
```

Console:

```powershell
cd packages/console
npm run typecheck
npm run build
npm run test:e2e
```

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
