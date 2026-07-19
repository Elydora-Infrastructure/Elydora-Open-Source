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
const adapterSourcePaths = {
  go: (id) => `sdks/go/cmd/elydora/plugins/${id}.go`,
  node: (id) => `sdks/node/src/plugins/${id}.ts`,
  python: (id) => `sdks/python/elydora/plugins/${id}.py`,
};
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

async function sourceExists(path) {
  try {
    await readFile(new URL(path, root));
    return true;
  } catch (error) {
    if (error?.code === 'ENOENT') return false;
    throw error;
  }
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
    assert.ok(['json', 'toml', 'plugin', 'script'].includes(provider.config_format));
    assert.ok(Array.isArray(provider.config_paths));
    assert.equal(typeof provider.events.before_tool, 'string');
    assert.equal(typeof provider.events.after_tool, 'string');
    assert.ok([
      'any_nonzero_exit',
      'exception',
      'exit_code_2',
      'hook_policy',
      'json_stdout_cancel',
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

    for (const variant of provider.contract_variants ?? []) {
      assert.match(variant.id, /^[a-z][a-z0-9]*$/);
      assert.ok(['stable', 'early_access', 'legacy'].includes(variant.release_channel));
      assert.equal(typeof variant.activation, 'string');
      assert.ok(['json', 'toml', 'plugin', 'script'].includes(variant.config_format));
      assert.ok(Array.isArray(variant.config_paths) && variant.config_paths.length > 0);
      assert.equal(typeof variant.events.before_tool, 'string');
      assert.equal(typeof variant.events.after_tool, 'string');
      assert.equal(typeof variant.blocking.mechanism, 'string');
      assert.equal(typeof variant.blocking.failure_mode, 'string');
      assert.match(variant.source_url, /^https:\/\//);
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
  assert.deepEqual(providerSchema.properties.config_format.enum, [
    'json',
    'toml',
    'plugin',
    'script',
  ]);
  assert.ok(
    providerSchema.properties.blocking.properties.mechanism.enum.includes(
      'json_stdout_cancel',
    ),
  );
  assert.deepEqual(providerSchema.properties.delivery_state.enum, ['available', 'partial', 'planned']);
  assert.equal(providerSchema.properties.contract_variants.items.$ref, '#/$defs/contractVariant');
  assert.deepEqual(schema.$defs.contractVariant.properties.release_channel.enum, [
    'stable',
    'early_access',
    'legacy',
  ]);
});

test('adapter delivery claims resolve to source files in every SDK mirror', async () => {
  const catalog = await readJson('integrations/catalog.json');

  for (const provider of catalog.providers) {
    for (const [language, delivered] of Object.entries(provider.adapters)) {
      const resolveSourcePath = adapterSourcePaths[language];
      assert.ok(resolveSourcePath, `unknown adapter language: ${language}`);
      const sourcePath = resolveSourcePath(provider.id);
      assert.equal(
        await sourceExists(sourcePath),
        delivered,
        `${provider.id} ${language} delivery state disagrees with ${sourcePath}`,
      );
    }
  }
});

test('high-drift providers retain their verified hook contracts', async () => {
  const catalog = await readJson('integrations/catalog.json');
  const providers = new Map(catalog.providers.map((provider) => [provider.id, provider]));

  assert.deepEqual(providers.get('augment').config_paths, [
    '/etc/augment/settings.json',
    'C:\\ProgramData\\Augment\\settings.json',
    '.augment/settings.local.json',
    '.augment/settings.json',
    '~/.augment/settings.json',
  ]);
  assert.deepEqual(providers.get('augment').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
  });
  assert.deepEqual(providers.get('augment').event_fields, {
    name: 'tool_name',
    input: 'tool_input',
    session: 'conversation_id',
  });
  assert.deepEqual(providers.get('augment').blocking, {
    mechanism: 'exit_code_2',
    failure_mode: 'fail_open',
  });
  assert.deepEqual(providers.get('codex').config_paths, [
    '~/.codex/hooks.json',
    '.codex/hooks.json',
  ]);
  assert.equal(providers.get('cline').integration_mode, 'command_hooks');
  assert.equal(providers.get('cline').config_format, 'script');
  assert.deepEqual(providers.get('cline').config_paths, [
    '~/Documents/Cline/Hooks/',
    '$CLINE_DIR/hooks/',
    '.clinerules/hooks/',
    '.cline/hooks/',
  ]);
  assert.deepEqual(providers.get('cline').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
  });
  assert.deepEqual(providers.get('cline').event_fields, {
    name: 'tool_call.name/tool_result.name',
    input: 'tool_call.input/tool_result.input',
    session: 'taskId',
  });
  assert.deepEqual(providers.get('cline').blocking, {
    mechanism: 'json_stdout_cancel',
    failure_mode: 'fail_open',
  });
  assert.deepEqual(providers.get('kimi').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
  });
  assert.equal(providers.get('kimi').config_format, 'toml');
  assert.deepEqual(providers.get('kimi').config_paths, [
    '$KIMI_CODE_HOME/config.toml',
    '~/.kimi-code/config.toml',
  ]);
  assert.deepEqual(providers.get('kimi').contract_variants, [
    {
      id: 'pythoncli',
      release_channel: 'legacy',
      activation: 'kimi-cli',
      config_format: 'toml',
      config_paths: ['~/.kimi/config.toml'],
      events: {
        before_tool: 'PreToolUse',
        after_tool: 'PostToolUse',
      },
      event_fields: {
        name: 'tool_name',
        input: 'tool_input',
        session: 'session_id',
      },
      blocking: {
        mechanism: 'exit_code_2',
        failure_mode: 'fail_open',
      },
      source_url: 'https://moonshotai.github.io/kimi-cli/en/customization/hooks.html',
    },
  ]);
  assert.deepEqual(providers.get('grok').event_fields, {
    name: 'toolName',
    input: 'toolInput',
    session: 'sessionId',
  });
  assert.deepEqual(providers.get('grok').config_paths, [
    '$GROK_HOME/hooks/*.json',
    '~/.grok/hooks/*.json',
    '.grok/hooks/*.json',
  ]);
  assert.deepEqual(providers.get('grok').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
  });
  assert.deepEqual(providers.get('grok').blocking, {
    mechanism: 'exit_code_2',
    failure_mode: 'fail_open',
  });
  assert.deepEqual(providers.get('kirocli').config_paths, [
    '~/.kiro/agents/*.json',
    '.kiro/agents/*.json',
  ]);
  assert.deepEqual(providers.get('kirocli').contract_variants, [
    {
      id: 'v3',
      release_channel: 'early_access',
      activation: 'kiro-cli --v3',
      config_format: 'json',
      config_paths: ['~/.kiro/hooks/*.json', '.kiro/hooks/*.json'],
      events: {
        before_tool: 'PreToolUse',
        after_tool: 'PostToolUse',
      },
      event_fields: {
        name: 'tool_name',
        input: 'tool_input',
        session: 'session_id',
      },
      blocking: {
        mechanism: 'exit_code_2',
        failure_mode: 'fail_open',
      },
      source_url: 'https://kiro.dev/docs/cli/v3/hooks/',
    },
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
