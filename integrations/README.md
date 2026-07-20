# Integration Catalog

[`catalog.json`](catalog.json) is the canonical inventory for agent integration IDs, official hook contracts, blocking behavior, and SDK delivery state. Backend validation, Console controls, Docs, and SDK registries derive their integration facts from this catalog.

The provider-level contract represents the default stable runtime. `contract_variants` records opt-in or transitional runtimes with their activation command and release channel. Kiro CLI uses this field for its v2 embedded hooks and v3 standalone hooks.

Grok Build adapters write exact `PreToolUse`, `PostToolUse`, and `PostToolUseFailure` groups under `$GROK_HOME/hooks` or `~/.grok/hooks`. The native camelCase payload includes `toolUseId`, `toolInputTruncated`, and `toolResult`; explicit denials use JSON stdout plus exit code `2`, while hook crashes and timeouts fail open. Project `.grok/hooks` files require `/hooks-trust` or `--trust`; `hooks-paths`, plugin, Claude Code, and Cursor sources remain read-only for Elydora.

Cline CLI 3 and Cline IDE use separate file-hook loaders. SDK adapters target CLI 3, write only `$CLINE_DIR/hooks`, preserve its hybrid `tool_call`/`preToolUse` and `tool_result`/`postToolUse` payload, and emit pure JSON cancellation controls. The IDE variant discovers runtime `--hooks-dir`, Documents, and `.clinerules/hooks` roots; Windows selects exact `.ps1` files and macOS/Linux select executable extensionless files. Both runtimes continue after hook crashes, timeouts, and invalid controls while recording the failure.

GitHub Copilot CLI loads user hook files from `$COPILOT_HOME/hooks` or `~/.copilot/hooks` and combines them with policy, repository, inline settings, cross-tool Claude, and plugin sources. Elydora owns one user file with exact `preToolUse`, `postToolUse`, and `postToolUseFailure` handlers and preserves native camelCase fields, including `toolResult` and `error`. Effective settings precedence controls `disableAllHooks`; each managed file also retains its independent disable flag. Command crashes and non-zero exits deny pre-tool execution, while command timeouts continue through the normal permission flow.

Cursor uses the same `preToolUse` and `postToolUse` contract across its CLI and IDE. User, project, and enterprise hook sources merge by documented priority; command exit code `2` denies an action, while per-script `failClosed` controls crash, timeout, and invalid-output behavior.

`delivery_state` is computed from the `node`, `python`, and `go` adapter flags:

| State | Meaning |
| --- | --- |
| `available` | All three SDK adapters exist |
| `partial` | One or two SDK adapters exist |
| `planned` | The provider contract is researched and adapter delivery is pending |

Update workflow:

1. Verify the provider contract against its official `source_url`.
2. Update `verified_on`, hook fields, blocking semantics, and adapter flags.
3. Run `npm run validate:integrations` from the repository root.
4. Synchronize generated consumers in Backend, Console, Docs, and standalone SDK repositories.

[`catalog.schema.json`](catalog.schema.json) freezes the machine-readable contract. [`integration-catalog.test.mjs`](../test/integration-catalog.test.mjs) enforces provider completeness, stable IDs, event fields, delivery-state derivation, and schema enums in CI.
