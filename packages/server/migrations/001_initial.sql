-- Elydora PostgreSQL Schema
-- Migration 001: Combined initial schema (agents, auth, operations, epochs, exports)
-- All timestamps are Unix milliseconds (BIGINT)

-- =========================================================================
-- Schema version tracking
-- =========================================================================
CREATE TABLE IF NOT EXISTS schema_versions (
  version     INTEGER PRIMARY KEY,
  applied_at  BIGINT NOT NULL,
  description TEXT   NOT NULL
);

-- =========================================================================
-- Organizations
-- =========================================================================
CREATE TABLE IF NOT EXISTS organizations (
  org_id     TEXT   NOT NULL PRIMARY KEY,
  name       TEXT   NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);

-- =========================================================================
-- Users
-- =========================================================================
CREATE TABLE IF NOT EXISTS users (
  user_id       TEXT   NOT NULL PRIMARY KEY,
  org_id        TEXT   NOT NULL REFERENCES organizations (org_id),
  email         TEXT   NOT NULL UNIQUE,
  password_hash TEXT   NOT NULL,
  display_name  TEXT   NOT NULL,
  role          TEXT   NOT NULL DEFAULT 'org_owner',
  status        TEXT   NOT NULL DEFAULT 'active',
  created_at    BIGINT NOT NULL,
  updated_at    BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_org_id ON users (org_id);
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);

-- =========================================================================
-- Agents
-- =========================================================================
CREATE TABLE IF NOT EXISTS agents (
  agent_id           TEXT   NOT NULL PRIMARY KEY,
  org_id             TEXT   NOT NULL,
  display_name       TEXT   NOT NULL,
  responsible_entity TEXT   NOT NULL,
  integration_type   TEXT   NOT NULL DEFAULT 'sdk',
  status             TEXT   NOT NULL DEFAULT 'active',
  created_at         BIGINT NOT NULL,
  updated_at         BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agents_org_id ON agents (org_id);

-- =========================================================================
-- Agent keys
-- =========================================================================
CREATE TABLE IF NOT EXISTS agent_keys (
  kid        TEXT   NOT NULL PRIMARY KEY,
  agent_id   TEXT   NOT NULL REFERENCES agents (agent_id),
  public_key TEXT   NOT NULL,
  algorithm  TEXT   NOT NULL DEFAULT 'ed25519',
  status     TEXT   NOT NULL DEFAULT 'active',
  created_at BIGINT NOT NULL,
  retired_at BIGINT
);

CREATE INDEX IF NOT EXISTS idx_agent_keys_agent_id ON agent_keys (agent_id);

-- =========================================================================
-- Operations
-- =========================================================================
CREATE TABLE IF NOT EXISTS operations (
  operation_id     TEXT    NOT NULL PRIMARY KEY,
  org_id           TEXT    NOT NULL,
  agent_id         TEXT    NOT NULL REFERENCES agents (agent_id),
  seq_no           INTEGER NOT NULL,
  operation_type   TEXT    NOT NULL,
  issued_at        BIGINT  NOT NULL,
  ttl_ms           INTEGER NOT NULL,
  nonce            TEXT    NOT NULL,
  subject          TEXT    NOT NULL,
  action           TEXT    NOT NULL,
  payload_hash     TEXT    NOT NULL,
  prev_chain_hash  TEXT    NOT NULL,
  chain_hash       TEXT    NOT NULL,
  agent_pubkey_kid TEXT    NOT NULL,
  signature        TEXT    NOT NULL,
  r2_payload_key   TEXT,
  created_at       BIGINT  NOT NULL,
  UNIQUE (agent_id, seq_no)
);

CREATE INDEX IF NOT EXISTS idx_operations_org_created ON operations (org_id, created_at);
CREATE INDEX IF NOT EXISTS idx_operations_agent_seq ON operations (agent_id, seq_no);
CREATE INDEX IF NOT EXISTS idx_operations_type ON operations (operation_type);

-- =========================================================================
-- Receipts
-- =========================================================================
CREATE TABLE IF NOT EXISTS receipts (
  receipt_id     TEXT   NOT NULL PRIMARY KEY,
  operation_id   TEXT   NOT NULL UNIQUE REFERENCES operations (operation_id),
  r2_receipt_key TEXT   NOT NULL,
  created_at     BIGINT NOT NULL
);

-- =========================================================================
-- Epochs
-- =========================================================================
CREATE TABLE IF NOT EXISTS epochs (
  epoch_id     TEXT    NOT NULL PRIMARY KEY,
  org_id       TEXT    NOT NULL,
  start_time   BIGINT  NOT NULL,
  end_time     BIGINT  NOT NULL,
  root_hash    TEXT    NOT NULL,
  leaf_count   INTEGER NOT NULL,
  r2_epoch_key TEXT    NOT NULL,
  created_at   BIGINT  NOT NULL
);

-- =========================================================================
-- Admin events
-- =========================================================================
CREATE TABLE IF NOT EXISTS admin_events (
  event_id    TEXT   NOT NULL PRIMARY KEY,
  org_id      TEXT   NOT NULL,
  actor       TEXT   NOT NULL,
  action      TEXT   NOT NULL,
  target_type TEXT   NOT NULL,
  target_id   TEXT   NOT NULL,
  details     TEXT,
  created_at  BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_admin_events_org_created ON admin_events (org_id, created_at);

-- =========================================================================
-- Exports
-- =========================================================================
CREATE TABLE IF NOT EXISTS exports (
  export_id      TEXT   NOT NULL PRIMARY KEY,
  org_id         TEXT   NOT NULL,
  status         TEXT   NOT NULL DEFAULT 'queued',
  query_params   TEXT   NOT NULL,
  r2_export_key  TEXT,
  created_at     BIGINT NOT NULL,
  completed_at   BIGINT
);

CREATE INDEX IF NOT EXISTS idx_exports_org_status ON exports (org_id, status);

-- =========================================================================
-- Seed schema version
-- =========================================================================
INSERT INTO schema_versions (version, applied_at, description)
VALUES (1, EXTRACT(EPOCH FROM NOW())::BIGINT * 1000, 'Initial schema (combined)')
ON CONFLICT (version) DO NOTHING;
