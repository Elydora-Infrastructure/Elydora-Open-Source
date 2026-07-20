# Integration Catalog

[`catalog.json`](catalog.json) is the canonical inventory for agent integration IDs, official hook contracts, blocking behavior, and SDK delivery state. Backend validation, Console controls, Docs, and SDK registries derive their integration facts from this catalog.

The provider-level contract represents the default stable runtime. `contract_variants` records opt-in or transitional runtimes with their activation command and release channel. Kiro CLI uses this field for its v2 embedded hooks and v3 standalone hooks.

Grok Build adapters write native user hook files under `$GROK_HOME/hooks` or `~/.grok/hooks`. Project `.grok/hooks` files require `/hooks-trust` or `--trust`; Claude Code and Cursor hook files are read-only compatibility sources for Elydora.

GitHub Copilot command `preToolUse` hooks deny on crashes and every non-zero exit. Command timeouts continue through the normal permission flow, so `timeout_failure_mode` records that explicit exception when it differs from `failure_mode`.

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
