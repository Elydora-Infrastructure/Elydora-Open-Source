# Integration Catalog

[`catalog.json`](catalog.json) is the canonical inventory for agent integration IDs, official hook contracts, blocking behavior, and SDK delivery state. Backend validation, Console controls, Docs, and SDK registries derive their integration facts from this catalog.

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
