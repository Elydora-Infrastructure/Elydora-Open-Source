-- Require every agent to declare a supported integration type.

BEGIN;

ALTER TABLE agents
  ALTER COLUMN integration_type DROP DEFAULT;

ALTER TABLE agents
  DROP CONSTRAINT IF EXISTS agents_integration_type_check;

ALTER TABLE agents
  ADD CONSTRAINT agents_integration_type_check
  CHECK (integration_type IN (
    'augment', 'claudecode', 'cline', 'codex', 'copilot',
    'cursor', 'droid', 'gemini', 'grok', 'kimi', 'kirocli',
    'kiroide', 'letta', 'opencode', 'qwen', 'enterprise',
    'gui', 'sdk', 'other'
  ));

INSERT INTO schema_versions (version, applied_at, description)
VALUES (
  2,
  EXTRACT(EPOCH FROM NOW())::BIGINT * 1000,
  'Require a supported agent integration type'
)
ON CONFLICT (version) DO NOTHING;

COMMIT;
