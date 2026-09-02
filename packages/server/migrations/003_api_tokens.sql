-- Opaque API tokens stored as SHA-256 hashes.

BEGIN;

CREATE TABLE IF NOT EXISTS api_tokens (
  token_id   TEXT   NOT NULL PRIMARY KEY,
  user_id    TEXT   NOT NULL REFERENCES users (user_id),
  org_id     TEXT   NOT NULL REFERENCES organizations (org_id),
  token_hash TEXT   NOT NULL UNIQUE,
  issued_at  BIGINT NOT NULL,
  expires_at BIGINT
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens (user_id);

INSERT INTO schema_versions (version, applied_at, description)
VALUES (
  3,
  EXTRACT(EPOCH FROM NOW())::BIGINT * 1000,
  'Opaque API tokens'
)
ON CONFLICT (version) DO NOTHING;

COMMIT;
