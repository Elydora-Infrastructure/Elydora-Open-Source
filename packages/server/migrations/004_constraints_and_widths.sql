-- Restore the constraints and column widths that the Compose bootstrap applied.

BEGIN;

ALTER TABLE schema_versions ALTER COLUMN version TYPE BIGINT;
ALTER TABLE operations ALTER COLUMN seq_no TYPE BIGINT;
ALTER TABLE operations ALTER COLUMN ttl_ms TYPE BIGINT;
ALTER TABLE epochs ALTER COLUMN leaf_count TYPE BIGINT;

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_org_id_fkey;
ALTER TABLE agents
  ADD CONSTRAINT agents_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations (org_id);

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
  ADD CONSTRAINT users_role_check
  CHECK (role IN ('org_owner', 'security_admin', 'compliance_auditor', 'readonly_investigator', 'integration_engineer'));

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_status_check;
ALTER TABLE users
  ADD CONSTRAINT users_status_check CHECK (status IN ('active', 'suspended'));

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_status_check;
ALTER TABLE agents
  ADD CONSTRAINT agents_status_check CHECK (status IN ('active', 'frozen', 'revoked'));

ALTER TABLE agent_keys DROP CONSTRAINT IF EXISTS agent_keys_algorithm_check;
ALTER TABLE agent_keys
  ADD CONSTRAINT agent_keys_algorithm_check CHECK (algorithm = 'ed25519');

ALTER TABLE agent_keys DROP CONSTRAINT IF EXISTS agent_keys_status_check;
ALTER TABLE agent_keys
  ADD CONSTRAINT agent_keys_status_check CHECK (status IN ('active', 'retired', 'revoked'));

ALTER TABLE exports DROP CONSTRAINT IF EXISTS exports_status_check;
ALTER TABLE exports
  ADD CONSTRAINT exports_status_check CHECK (status IN ('queued', 'running', 'done', 'failed'));

INSERT INTO schema_versions (version, applied_at, description)
VALUES (
  4,
  EXTRACT(EPOCH FROM NOW())::BIGINT * 1000,
  'Restore status constraints, agent org foreign key, and 64-bit counters'
)
ON CONFLICT (version) DO NOTHING;

COMMIT;
