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
- Resolve stable Kimi hooks through `$KIMI_CODE_HOME/config.toml` with `~/.kimi-code/config.toml` as the default, and treat an empty override as the default. Activate `~/.kimi/config.toml` only when the legacy home exists, ignore executable lookup as activation evidence, and collapse equal stable and legacy paths to the stable contract. Register exact `PreToolUse`, `PostToolUse`, and `PostToolUseFailure` rules with ten-second commands; stable Kimi supports its current sixteen-event schema and legacy kimi-cli supports its thirteen-event schema. Preserve the native snake_case payload, propagate exit code `2`, and keep audit delivery fail-open with owner-only error logs. Use encoded PowerShell commands on Windows and exact two-argument shell commands on Unix. Commit every detected config, generated runtime, runtime config, and private key through one rollback-capable transaction. Status requires strict runtime metadata, a canonical private key, exact identity, and non-empty physical scripts.
- Preserve Kimi TOML comments, array style, and unrelated formatting through syntax-preserving edits, then parse every selected document before writing any file.
- Resolve Grok Build user hooks through `$GROK_HOME/hooks/*.json`, with `~/.grok/hooks/*.json` selected when the variable is absent. Reject an empty `GROK_HOME` and direct the operator to unset it or provide an absolute home directory; Grok Build 0.2.106 otherwise resolves the empty value to cwd-relative `hooks`. Keep Claude Code, Cursor, project `.grok/hooks`, plugin, and `hooks-paths` sources read-only. Reject matcher-bearing `SessionStart`, `SessionEnd`, `Stop`, and `UserPromptSubmit` groups because the stable loader drops them; preserve matchers on the remaining supported events. Accept a null handler `env` as an empty map. Register exact matchless `PreToolUse`, `PostToolUse`, and `PostToolUseFailure` groups with ten-second command handlers, preserve the complete native camelCase payload, and emit Grok's documented deny JSON plus exit code `2` for frozen or revoked agents. Use encoded PowerShell commands on Windows and exact two-argument shell commands on Unix. Commit each SDK's Grok hook file, generated runtimes, runtime config, and private key through one rollback-capable transaction. Status requires the exact event triple, strict runtime metadata, a canonical private key, exact identity, and non-empty physical scripts. Verify discovery with `grok inspect --json` and the in-session `/hooks` view.
- Resolve Claude Code user hooks through `$CLAUDE_CONFIG_DIR/settings.json` with `~/.claude/settings.json` when the variable is absent. Match Claude Code's path resolution exactly: relative and empty values resolve from the current working directory, and literal tildes remain path segments. Keep project, local, managed, plugin, skill, and agent hook sources read-only. Validate the complete shipped handler schema, including command exec form, async rewake metadata, HTTP, MCP tool, prompt, and agent handlers. Register exact matchless `PreToolUse`, `PostToolUse`, and `PostToolUseFailure` groups with ten-second exec-form command handlers. Preserve the complete native snake_case payload and propagate freeze and revocation through exit code `2`. Commit each SDK's Claude Code user settings, generated runtimes, runtime config, and private key through one rollback-capable transaction. Reject installation while the user source sets `disableAllHooks`, then require `/hooks` and `claude doctor` verification for higher-scope policy effects.
- Resolve Gemini CLI user hooks through `$GEMINI_CLI_HOME/.gemini/settings.json` with `~/.gemini/settings.json` when the variable is absent or empty. Preserve relative and literal-tilde home values as Gemini CLI does. Keep workspace, system defaults, system overrides, and extension hooks read-only. Preserve JSON comments while rejecting trailing commas and duplicate keys. Register exact matchless `BeforeTool` and `AfterTool` groups named `elydora-guard` and `elydora-audit` with ten-second command handlers. Preserve the complete native snake_case payload, emit valid JSON on exit code `0`, and propagate freeze and revocation through exit code `2`. Use encoded PowerShell commands on Windows. Commit each SDK's Gemini CLI user settings, generated runtimes, runtime config, and private key through one rollback-capable transaction. Respect `hooksConfig.enabled` and `hooksConfig.disabled`, then require `/hooks list` verification for higher-scope policy effects.
- Resolve Auggie user hooks through `~/.augment/settings.json`; keep system, workspace, local workspace, and alternate `--augment-cache-dir` settings read-only. Validate the shipped Auggie 0.33 hook schema: `PreToolUse`, `PostToolUse`, `Stop`, `SessionStart`, `SessionEnd`, `Notification`, and `PromptSubmit`; regex matchers belong to tool events, command handlers use string-array arguments and positive millisecond timeouts, and metadata flags are booleans. Generate `.cmd` wrappers on Windows and `.sh` wrappers on Unix, preserve the complete native snake_case payload, and propagate freeze and revocation through exit code `2`.
- All three SDKs commit Auggie settings, both wrappers, generated runtimes, runtime config, and private key through one rollback-capable transaction. Their status checks require exact runtime identity, canonical private keys, physical files, and exact wrapper sources. Installation validates matcher syntax with JavaScript `RegExp`; status and uninstall stay available for offline recovery. Verify the effective configuration with `auggie tools list`.
- Treat Cline 3 CLI file hooks as the SDK adapter contract. Resolve hooks additively from `~/Documents/Cline/Hooks`, `$CLINE_DIR/hooks` with `~/.cline/hooks` as the default, `.clinerules/hooks`, and `.cline/hooks`; write only the `$CLINE_DIR` source. Preserve the complete hybrid CLI payload and use supported event filename extensions. Commit both wrappers, generated runtimes, runtime config, and private key through one rollback-capable transaction. Status requires physical files, canonical private keys, exact runtime identity, and exact generated runtime and wrapper sources. Node and Go wrappers use `process.execPath`; Python wrappers resolve the runtime executable from an absolute shebang. Translate guard exit code `2` into one-line JSON stdout with `cancel: true`; hook errors, timeouts, and invalid control JSON fail open. Model Cline IDE as a separate variant with runtime `--hooks-dir`, Documents, and `.clinerules/hooks` roots, exact `.ps1` filenames on Windows, and executable extensionless filenames on macOS and Linux.
- Resolve Codex user hooks through `$CODEX_HOME/hooks.json` with `~/.codex/hooks.json` as the default, matching Codex's existing-directory canonicalization rule. Model user JSON, inline TOML layers, project files, managed `requirements.toml`, and plugin-bundled hooks as additive stable sources. SDK adapters write only the user JSON source, register exact `PreToolUse` and `PostToolUse` match-all command groups, preserve the complete native payload, propagate freeze and revocation through exit code `2`, keep guard lookup and audit delivery fail-open with observable errors, commit user hooks and all four runtime artifacts through one rollback-capable transaction, and require `/hooks` trust review for both definition hashes.
- Resolve GitHub Copilot CLI user hooks through `$COPILOT_HOME/hooks/elydora-audit.json` with `~/.copilot/hooks/elydora-audit.json` as the default, and treat an empty override as the default. Keep policy, repository, inline settings, cross-tool Claude, and plugin hook sources read-only. Evaluate `disableAllHooks` through legacy user config, user settings, Claude repository and local settings, then GitHub repository and local settings; the managed hook file flag remains independently authoritative. Register exact `preToolUse`, `postToolUse`, and `postToolUseFailure` command handlers with ten-second timeouts, preserve the complete native camelCase payload, and propagate frozen or revoked state through exit code `2`. Commit each SDK's user hook file, exact legacy migration, generated runtimes, runtime config, and private key through one rollback-capable transaction. Require physical source paths, strict hook schemas, exact runtime sources, canonical private keys, and matching runtime identity during status checks, then verify discovery with the current official Copilot native hook loader and restart active CLI sessions.
- Model Cursor as one CLI-and-IDE hook contract. Preserve user, project, and enterprise sources in documented priority order; forward `tool_name`, `tool_input`, and `conversation_id`; use exit code `2` for denial; record `failClosed` as the per-script failure-mode control.
- Write Node Cursor hooks only to `~/.cursor/hooks.json`; keep project and enterprise sources read-only. Register `preToolUse`, `postToolUse`, and `postToolUseFailure`, preserve the complete native payload, emit valid native JSON responses, retain PowerShell exit codes, and commit guard, runtime metadata, private key, audit runtime, and user hooks in one rollback-capable transaction.
- Write Go Cursor hooks only to `~/.cursor/hooks.json`; keep project and enterprise sources read-only. Preserve user hooks, migrate the prior versionless Elydora contract, audit successful and failed tool calls, emit valid native JSON responses, retain PowerShell exit codes, and commit guard, runtime metadata, private key, audit runtime, and user hooks in one rollback-capable transaction.
- Write Python Cursor hooks only to `~/.cursor/hooks.json`; keep project and enterprise sources read-only. Preserve user hooks, migrate the prior versionless Elydora contract, and own exact native `preToolUse`, `postToolUse`, and `postToolUseFailure` commands. Forward native success and failure payloads, retain PowerShell exit code `2`, set every handler to `failClosed` with a bounded timeout, and commit the guard, audit script, runtime identity, private key, and hook configuration as one rollback-capable transaction.
- Resolve Factory Droid 0.175.0 user hooks through `~/.factory/hooks.json`, the legacy `~/.factory/hooks/hooks.json`, then the user `settings.local.json` and `settings.json` hook containers. Current hook files use a top-level `hooks` object; retain direct event maps as an explicit legacy read contract until Factory migrates them. Register exact `PreToolUse` and `PostToolUse` groups with ten-second absolute command handlers in one active source, preserve the native snake_case payload, and propagate freeze or revocation through exit code `2`. Use PowerShell's call operator with single-quoted absolute arguments and explicit `$LASTEXITCODE` propagation on Windows, and migrate the legacy double-quoted command form through exact ownership checks. Remove exact Elydora ownership from inactive user sources while preserving JSONC comments and unknown event arrays. Keep project, folder, plugin, and organization hook definitions read-only. Resolve local settings over their matching base file, then apply system, project, folder, and user `hooksDisabled` through Factory's extension-only scalar precedence; reject system-managed `allowManagedHooksOnly`. Commit user hook sources, generated runtimes, runtime config, and private key through one rollback-capable transaction, precondition every parsed source and policy file, require exact generated runtime sources during status checks, and require `/hooks` review for the session snapshot and remote organization policy.
- Resolve Qwen Code 0.20.0 user settings through explicit `QWEN_HOME`, then the initial Qwen home `.env`, then the OS home `.env`; when bootstrap discovers a redirected home, read that home's `.env` as well. Treat empty overrides as unset while preserving their process-level ownership semantics. Read system defaults, user settings, the active trusted workspace, system overrides, and trusted-folder rules with Qwen's documented precedence; write only the user source. Validate the current twenty-one event names and command, HTTP, and prompt handler schemas while preserving future event payloads. Register exact matchless `PreToolUse`, `PostToolUse`, and `PostToolUseFailure` groups named `elydora-guard` and `elydora-audit` with ten-second millisecond timeouts. Preserve the complete native snake_case payload, propagate freeze and revocation through exit code `2`, and use PowerShell's call operator with explicit `$LASTEXITCODE` propagation on Windows.
- Commit Qwen Code user settings, generated runtimes, runtime config, and private key through one rollback-capable transaction, with snapshot preconditions for every consulted routing, policy, trust, and settings source. Status requires physical files, exact runtime sources, a canonical private key, matching runtime identity, and an effective enabled state. Verify discovery with the official 0.20.0 loader, review `/hooks`, and restart active Qwen Code sessions.
- Resolve Python Letta Code 0.28.13 global hooks through `${HOME:-~}/.letta/settings.json`, matching Letta's `HOME || homedir()` behavior. Keep Elydora runtimes under the shared CLI root from `os.path.expanduser("~")`, independent from Letta's configurable settings home. Keep the active workspace `.letta/settings.json` and `.letta/settings.local.json` sources read-only, deduplicate the project source when the workspace is the home directory, and apply the official `hooks.disabled` precedence: an explicit global `false` enables hooks, global `true` disables them, then project and project-local `true` values disable them. Validate all eleven current events plus command and prompt handler shapes while preserving future event payloads. Register exact `PreToolUse`, `PostToolUse`, and `PostToolUseFailure` groups with `*` matchers and ten-second millisecond timeouts. Preserve the complete native snake_case payload, propagate frozen or revoked state through exit code `2`, and surface audit delivery failures through stderr, owner-only `error.log`, and exit code `1`; Letta post events retain their documented nonblocking semantics. Use PowerShell's call operator with explicit `$LASTEXITCODE` propagation on Windows and exact two-argument shell commands on Unix. Commit global settings, generated runtimes, runtime config, and private key through one rollback-capable transaction guarded by every consulted source snapshot. Migrate only exact legacy Python command ownership, require physical sources, canonical private keys, exact runtime sources, executable identity, matching runtime identity, and an effective enabled state during status checks, then run `/hooks` and restart active sessions.

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
