import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);

async function readJson(path) {
  return JSON.parse(await readFile(new URL(path, root), 'utf8'));
}

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
  assert.deepEqual(providers.get('claudecode').config_paths, [
    '$CLAUDE_CONFIG_DIR/settings.json',
    '~/.claude/settings.json',
    '.claude/settings.json',
    '.claude/settings.local.json',
  ]);
  assert.deepEqual(providers.get('claudecode').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
    after_tool_failure: 'PostToolUseFailure',
  });
  assert.deepEqual(providers.get('claudecode').event_fields, {
    name: 'tool_name',
    input: 'tool_input',
    session: 'session_id',
    call_id: 'tool_use_id',
    output: 'tool_response',
    error: 'error',
  });
  assert.deepEqual(providers.get('claudecode').blocking, {
    mechanism: 'exit_code_2',
    failure_mode: 'fail_open',
  });
  assert.deepEqual(providers.get('codex').config_paths, [
    '$CODEX_HOME/hooks.json',
    '~/.codex/hooks.json',
    '.codex/hooks.json',
  ]);
  assert.deepEqual(providers.get('codex').contract_variants, [
    {
      id: 'inlinetoml',
      release_channel: 'stable',
      activation: 'Hooks configured inline in an active Codex config.toml or managed requirements.toml layer',
      config_format: 'toml',
      config_paths: [
        '$CODEX_HOME/config.toml',
        '~/.codex/config.toml',
        '.codex/config.toml',
        'requirements.toml',
      ],
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
      source_url: 'https://learn.chatgpt.com/docs/hooks',
    },
    {
      id: 'pluginbundle',
      release_channel: 'stable',
      activation: 'An enabled Codex plugin supplies hooks through its manifest or default hooks file',
      config_format: 'plugin',
      config_paths: [
        '.codex-plugin/plugin.json',
        'hooks/hooks.json',
      ],
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
      source_url: 'https://learn.chatgpt.com/docs/hooks',
    },
  ]);
  assert.deepEqual(providers.get('copilot').config_paths, [
    '$COPILOT_HOME/hooks/*.json',
    '~/.copilot/hooks/*.json',
    '.github/hooks/*.json',
  ]);
  assert.deepEqual(providers.get('copilot').events, {
    before_tool: 'preToolUse',
    after_tool: 'postToolUse',
    after_tool_failure: 'postToolUseFailure',
  });
  assert.deepEqual(providers.get('copilot').event_fields, {
    name: 'toolName',
    input: 'toolArgs',
    session: 'sessionId',
    output: 'toolResult',
    error: 'error',
  });
  assert.deepEqual(providers.get('copilot').blocking, {
    mechanism: 'any_nonzero_exit',
    failure_mode: 'fail_closed',
    timeout_failure_mode: 'fail_open',
  });
  assert.equal(providers.get('cursor').surface, 'cli_and_ide');
  assert.deepEqual(providers.get('cursor').config_paths, [
    '/Library/Application Support/Cursor/hooks.json',
    '/etc/cursor/hooks.json',
    'C:\\ProgramData\\Cursor\\hooks.json',
    '.cursor/hooks.json',
    '~/.cursor/hooks.json',
  ]);
  assert.deepEqual(providers.get('cursor').events, {
    before_tool: 'preToolUse',
    after_tool: 'postToolUse',
  });
  assert.deepEqual(providers.get('cursor').event_fields, {
    name: 'tool_name',
    input: 'tool_input',
    session: 'conversation_id',
  });
  assert.deepEqual(providers.get('cursor').blocking, {
    mechanism: 'exit_code_2',
    failure_mode: 'configurable',
  });
  assert.deepEqual(providers.get('droid').config_paths, [
    '~/.factory/hooks.json',
    '~/.factory/settings.json',
    '.factory/hooks.json',
    '.factory/settings.json',
  ]);
  assert.deepEqual(providers.get('droid').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
  });
  assert.deepEqual(providers.get('droid').event_fields, {
    name: 'tool_name',
    input: 'tool_input',
    session: 'session_id',
  });
  assert.deepEqual(providers.get('droid').blocking, {
    mechanism: 'exit_code_2',
    failure_mode: 'fail_open',
  });
  assert.deepEqual(providers.get('droid').contract_variants, [
    {
      id: 'nestedhooks',
      release_channel: 'legacy',
      activation: 'An existing nested hooks file remains active until Droid saves and archives it',
      config_format: 'json',
      config_paths: [
        '~/.factory/hooks/hooks.json',
        '.factory/hooks/hooks.json',
      ],
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
      source_url: 'https://docs.factory.ai/reference/hooks-reference',
    },
  ]);
  assert.deepEqual(providers.get('gemini').config_paths, [
    '$GEMINI_CLI_HOME/.gemini/settings.json',
    '~/.gemini/settings.json',
    '.gemini/settings.json',
    '$GEMINI_CLI_SYSTEM_DEFAULTS_PATH',
    '$GEMINI_CLI_SYSTEM_SETTINGS_PATH',
    '/Library/Application Support/GeminiCli/system-defaults.json',
    '/Library/Application Support/GeminiCli/settings.json',
    '/etc/gemini-cli/system-defaults.json',
    '/etc/gemini-cli/settings.json',
    'C:\\ProgramData\\gemini-cli\\system-defaults.json',
    'C:\\ProgramData\\gemini-cli\\settings.json',
  ]);
  assert.deepEqual(providers.get('gemini').events, {
    before_tool: 'BeforeTool',
    after_tool: 'AfterTool',
  });
  assert.deepEqual(providers.get('gemini').event_fields, {
    name: 'tool_name',
    input: 'tool_input',
    session: 'session_id',
    output: 'tool_response',
    error: 'tool_response.error',
  });
  assert.deepEqual(providers.get('gemini').blocking, {
    mechanism: 'exit_code_2',
    failure_mode: 'fail_open',
  });
  assert.equal(
    providers.get('gemini').source_url,
    'https://geminicli.com/docs/hooks/reference/',
  );
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
    name: 'tool_call.name/preToolUse.toolName/tool_result.name/postToolUse.toolName',
    input: 'tool_call.input/preToolUse.parameters/tool_result.input/postToolUse.parameters',
    session: 'taskId',
    call_id: 'tool_call.id/tool_result.id',
    output: 'tool_result.output/postToolUse.result',
    error: 'tool_result.error',
  });
  assert.deepEqual(providers.get('cline').blocking, {
    mechanism: 'json_stdout_cancel',
    failure_mode: 'fail_open',
  });
  assert.deepEqual(providers.get('cline').contract_variants, [
    {
      id: 'idefilehooks',
      release_channel: 'stable',
      activation:
        'Hooks enabled in Cline IDE settings; Windows uses <HookName>.ps1 and macOS/Linux use executable extensionless files',
      config_format: 'script',
      config_paths: [
        '--hooks-dir <path>',
        '~/Documents/Cline/Hooks/',
        '.clinerules/hooks/',
      ],
      events: {
        before_tool: 'PreToolUse',
        after_tool: 'PostToolUse',
      },
      event_fields: {
        name: 'preToolUse.toolName/postToolUse.toolName',
        input: 'preToolUse.parameters/postToolUse.parameters',
        session: 'taskId',
        output: 'postToolUse.result',
      },
      blocking: {
        mechanism: 'json_stdout_cancel',
        failure_mode: 'fail_open',
      },
      source_url:
        'https://github.com/cline/cline/blob/main/apps/vscode/src/core/hooks/hook-factory.ts',
    },
  ]);
  assert.deepEqual(providers.get('kimi').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
    after_tool_failure: 'PostToolUseFailure',
  });
  assert.deepEqual(providers.get('kimi').event_fields, {
    name: 'tool_name',
    input: 'tool_input',
    session: 'session_id',
    call_id: 'tool_call_id',
    output: 'tool_output',
    error: 'error',
  });
  assert.equal(providers.get('kimi').config_format, 'toml');
  assert.equal(
    providers.get('kimi').source_url,
    'https://www.kimi.com/code/docs/en/kimi-code-cli/customization/hooks.html',
  );
  assert.deepEqual(providers.get('kimi').config_paths, [
    '$KIMI_CODE_HOME/config.toml',
    '~/.kimi-code/config.toml',
  ]);
  assert.deepEqual(providers.get('kimi').contract_variants, [
    {
      id: 'pythoncli',
      release_channel: 'legacy',
      activation: 'An existing ~/.kimi home activates the legacy kimi-cli contract',
      config_format: 'toml',
      config_paths: ['~/.kimi/config.toml'],
      events: {
        before_tool: 'PreToolUse',
        after_tool: 'PostToolUse',
        after_tool_failure: 'PostToolUseFailure',
      },
      event_fields: {
        name: 'tool_name',
        input: 'tool_input',
        session: 'session_id',
        call_id: 'tool_call_id',
        output: 'tool_output',
        error: 'error',
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
    call_id: 'toolUseId',
    output: 'toolResult',
    error: 'toolResult',
  });
  assert.deepEqual(providers.get('grok').config_paths, [
    '$GROK_HOME/hooks/*.json',
    '~/.grok/hooks/*.json',
    '~/.grok/hooks-paths',
    '.grok/hooks/*.json',
    '.claude/settings.json',
    '.cursor/hooks.json',
  ]);
  assert.deepEqual(providers.get('grok').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
    after_tool_failure: 'PostToolUseFailure',
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
  assert.equal(providers.get('qwen').config_format, 'json');
  assert.deepEqual(providers.get('qwen').config_paths, [
    '$QWEN_CODE_SYSTEM_DEFAULTS_PATH',
    '/Library/Application Support/QwenCode/system-defaults.json',
    '/etc/qwen-code/system-defaults.json',
    'C:\\ProgramData\\qwen-code\\system-defaults.json',
    '$QWEN_HOME/settings.json',
    '~/.qwen/settings.json',
    '.qwen/settings.json',
    '$QWEN_CODE_SYSTEM_SETTINGS_PATH',
    '/Library/Application Support/QwenCode/settings.json',
    '/etc/qwen-code/settings.json',
    'C:\\ProgramData\\qwen-code\\settings.json',
    '$QWEN_CODE_TRUSTED_FOLDERS_PATH',
    '$QWEN_HOME/trustedFolders.json',
    '~/.qwen/trustedFolders.json',
  ]);
  assert.deepEqual(providers.get('qwen').events, {
    before_tool: 'PreToolUse',
    after_tool: 'PostToolUse',
    after_tool_failure: 'PostToolUseFailure',
  });
  assert.deepEqual(providers.get('qwen').event_fields, {
    name: 'tool_name',
    input: 'tool_input',
    session: 'session_id',
    call_id: 'tool_use_id/tool_call_id',
    output: 'tool_response',
    error: 'error',
  });
  assert.deepEqual(providers.get('qwen').blocking, {
    mechanism: 'exit_code_2',
    failure_mode: 'fail_open',
  });
});
