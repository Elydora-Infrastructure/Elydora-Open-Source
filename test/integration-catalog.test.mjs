import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);

const expectedProviderIds = [
  'augment',
  'claudecode',
  'cline',
  'codex',
  'copilot',
  'cursor',
  'droid',
  'gemini',
  'grok',
  'kimi',
  'kirocli',
  'kiroide',
  'letta',
  'opencode',
  'qwen',
];

const expectedCustomIntegrationIds = ['enterprise', 'gui', 'other', 'sdk'];
const expectedAdapterKeys = ['go', 'node', 'python'];
const requiredProviderFields = [
  'id',
  'name',
  'vendor',
  'surface',
  'integration_mode',
  'config_format',
  'config_paths',
  'events',
  'blocking',
  'adapters',
  'delivery_state',
  'source_url',
];

async function readJson(path) {
  return JSON.parse(await readFile(new URL(path, root), 'utf8'));
}

function deliveryState(adapters) {
  const availableCount = Object.values(adapters).filter(Boolean).length;
  if (availableCount === expectedAdapterKeys.length) return 'available';
  if (availableCount === 0) return 'planned';
  return 'partial';
}

test('catalog contains the complete, ordered integration inventory', async () => {
  const catalog = await readJson('integrations/catalog.json');

  assert.equal(catalog.schema_version, 1);
  assert.match(catalog.verified_on, /^\d{4}-\d{2}-\d{2}$/);
  assert.deepEqual(
    catalog.providers.map(({ id }) => id),
    expectedProviderIds,
  );
  assert.deepEqual(
    catalog.custom_integrations.map(({ id }) => id),
    expectedCustomIntegrationIds,
  );
});

test('every provider exposes a complete, machine-readable hook contract', async () => {
  const catalog = await readJson('integrations/catalog.json');

  for (const provider of catalog.providers) {
    for (const field of requiredProviderFields) {
      assert.ok(field in provider, `${provider.id} is missing ${field}`);
    }

    assert.match(provider.id, /^[a-z][a-z0-9]*$/);
    assert.equal(typeof provider.name, 'string');
    assert.equal(typeof provider.vendor, 'string');
    assert.ok(['cli', 'ide', 'cli_and_ide'].includes(provider.surface));
    assert.ok(['command_hooks', 'plugin_api'].includes(provider.integration_mode));
    assert.ok(['json', 'toml', 'plugin'].includes(provider.config_format));
    assert.ok(Array.isArray(provider.config_paths));
    assert.equal(typeof provider.events.before_tool, 'string');
    assert.equal(typeof provider.events.after_tool, 'string');
    assert.ok([
      'any_nonzero_exit',
      'exception',
      'exit_code_2',
      'hook_policy',
    ].includes(provider.blocking.mechanism));
    assert.ok([
      'adapter_controlled',
      'configurable',
      'fail_closed',
      'fail_open',
    ].includes(provider.blocking.failure_mode));
    assert.deepEqual(Object.keys(provider.adapters).sort(), expectedAdapterKeys);
    assert.ok(Object.values(provider.adapters).every((value) => typeof value === 'boolean'));
    assert.equal(provider.delivery_state, deliveryState(provider.adapters));
    assert.match(provider.source_url, /^https:\/\//);

    if (provider.integration_mode === 'command_hooks') {
      assert.ok(provider.config_paths.length > 0, `${provider.id} requires config paths`);
    }

    if (provider.event_fields) {
      assert.equal(typeof provider.event_fields.name, 'string');
      assert.equal(typeof provider.event_fields.input, 'string');
      assert.equal(typeof provider.event_fields.session, 'string');
    }
  }
});

test('schema freezes the provider contract and supported enums', async () => {
  const schema = await readJson('integrations/catalog.schema.json');
  const providerSchema = schema.properties.providers.items;

  assert.equal(schema.$schema, 'https://json-schema.org/draft/2020-12/schema');
  assert.equal(schema.additionalProperties, false);
  assert.deepEqual([...providerSchema.required].sort(), [...requiredProviderFields].sort());
  assert.deepEqual(providerSchema.properties.surface.enum, ['cli', 'ide', 'cli_and_ide']);
  assert.deepEqual(providerSchema.properties.integration_mode.enum, ['command_hooks', 'plugin_api']);
  assert.deepEqual(providerSchema.properties.delivery_state.enum, ['available', 'partial', 'planned']);
});

test('high-drift providers retain their verified hook contracts', async () => {
  const catalog = await readJson('integrations/catalog.json');
  const providers = new Map(catalog.providers.map((provider) => [provider.id, provider]));

  assert.deepEqual(providers.get('codex').config_paths, [
    '~/.codex/hooks.json',
    '.codex/hooks.json',
  ]);
  assert.deepEqual(providers.get('kimi').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
  });
  assert.equal(providers.get('kimi').config_format, 'toml');
  assert.deepEqual(providers.get('grok').event_fields, {
    name: 'toolName',
    input: 'toolInput',
    session: 'sessionId',
  });
  assert.deepEqual(providers.get('kirocli').config_paths, [
    '~/.kiro/agents/*.json',
    '.kiro/agents/*.json',
  ]);
  assert.deepEqual(providers.get('kiroide').config_paths, [
    '.kiro/hooks/*.json',
  ]);
  assert.deepEqual(providers.get('kiroide').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
  });
  assert.deepEqual(providers.get('kiroide').blocking, {
    mechanism: 'exit_code_2',
    failure_mode: 'fail_open',
  });
});
