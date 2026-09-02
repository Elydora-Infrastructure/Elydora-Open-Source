import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';
import {
  createFixture,
  environment,
  managedHandler,
  readSettings,
  runProcess,
  startApiServer,
} from '../test-support/letta.mjs';

const sourceRoot = process.env.ELYDORA_LETTA_SOURCE;
const bunBinary = process.env.ELYDORA_BUN_BINARY ?? 'bun';

function moduleUrl(relativePath) {
  return pathToFileURL(path.join(sourceRoot, relativePath)).href;
}

function resultJson(stdout) {
  const line = stdout.trim().split(/\r?\n/).at(-1);
  return JSON.parse(line);
}

async function runOfficialProbe(fixture, mode) {
  const settings = await readSettings(fixture.globalPath);
  const commands = {
    guard: managedHandler(settings, 'PreToolUse').command,
    audit: managedHandler(settings, 'PostToolUse').command,
    failure: managedHandler(settings, 'PostToolUseFailure').command,
  };
  const source = `
    import { settingsManager } from ${JSON.stringify(moduleUrl('src/settings-manager.ts'))};
    import { getHooksForEvent } from ${JSON.stringify(moduleUrl('src/hooks/loader.ts'))};
    import { executeCommandHook } from ${JSON.stringify(moduleUrl('src/hooks/executor.ts'))};
    import { runPostToolUseHooks } from ${JSON.stringify(moduleUrl('src/hooks/index.ts'))};
    const workspace = process.env.ELYDORA_OFFICIAL_WORKSPACE;
    const commands = JSON.parse(process.env.ELYDORA_OFFICIAL_COMMANDS);
    await settingsManager.initialize();
    await settingsManager.loadProjectSettings(workspace);
    await settingsManager.loadLocalProjectSettings(workspace);
    const pre = await getHooksForEvent('PreToolUse', 'Bash', workspace);
    const post = await getHooksForEvent('PostToolUse', 'Bash', workspace);
    const failure = await getHooksForEvent('PostToolUseFailure', 'Bash', workspace);
    const managed = (hooks, command) => hooks.find((hook) => (
      hook.type === 'command' && hook.command === command
    ));
    if (process.env.ELYDORA_OFFICIAL_MODE === 'active') {
      const guardResult = await executeCommandHook(
        managed(pre, commands.guard),
        {
          event_type: 'PreToolUse',
          working_directory: workspace,
          tool_name: 'Bash',
          tool_input: { command: 'echo official' },
          tool_call_id: 'official-pre',
          agent_id: 'letta-agent',
        },
        workspace,
      );
      const postResult = await executeCommandHook(
        managed(post, commands.audit),
        {
          event_type: 'PostToolUse',
          working_directory: workspace,
          tool_name: 'Bash',
          tool_input: { command: 'echo official' },
          tool_result: { status: 'success', output: 'official' },
          tool_call_id: 'official-post',
        },
        workspace,
      );
      const failureResult = await executeCommandHook(
        managed(failure, commands.failure),
        {
          event_type: 'PostToolUseFailure',
          working_directory: workspace,
          tool_name: 'Bash',
          tool_input: { command: 'exit 1' },
          error_message: 'official failure',
          error_type: 'ProcessError',
          tool_call_id: 'official-failure',
        },
        workspace,
      );
      console.log(JSON.stringify({
        counts: [pre.length, post.length, failure.length],
        exits: [guardResult.exitCode, postResult.exitCode, failureResult.exitCode],
      }));
    } else {
      const result = await runPostToolUseHooks(
        'Bash',
        { command: 'echo unavailable' },
        { status: 'success', output: 'unavailable' },
        'official-post-error',
        workspace,
      );
      console.log(JSON.stringify({
        blocked: result.blocked,
        errored: result.errored,
        exits: result.results.map((entry) => entry.exitCode),
      }));
    }
    await settingsManager.flush();
  `;
  return runProcess(
    bunBinary,
    ['--silent', '--eval', source],
    {
      ...environment(fixture),
      LETTA_SKIP_KEYCHAIN_CHECK: '1',
      ELYDORA_OFFICIAL_COMMANDS: JSON.stringify(commands),
      ELYDORA_OFFICIAL_MODE: mode,
      ELYDORA_OFFICIAL_WORKSPACE: fixture.projectDir,
    },
    sourceRoot,
  );
}

test('official Letta Code 0.28.13 loads and executes all managed hooks', {
  skip: sourceRoot ? false : 'set ELYDORA_LETTA_SOURCE to the official Letta Code source tree',
  timeout: 60_000,
}, async () => {
  const manifest = JSON.parse(await readFile(path.join(sourceRoot, 'package.json'), 'utf-8'));
  assert.equal(manifest.version, '0.28.13');
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  let apiClosed = false;
  try {
    assert.equal((await fixture.install()).code, 0);
    const active = await runOfficialProbe(fixture, 'active');
    assert.equal(active.code, 0, active.stderr);
    assert.deepEqual(resultJson(active.stdout), {
      counts: [1, 1, 1],
      exits: [0, 0, 0],
    });
    assert.equal(api.requests.filter((request) => request.method === 'POST').length, 2);
    await api.close();
    apiClosed = true;
    const unavailable = await runOfficialProbe(fixture, 'unavailable');
    assert.equal(unavailable.code, 0, unavailable.stderr);
    assert.deepEqual(resultJson(unavailable.stdout), {
      blocked: false,
      errored: true,
      exits: [1],
    });
  } finally {
    await fixture.close();
    if (!apiClosed) await api.close();
  }
});
