import assert from 'node:assert/strict';
import {
  lstat,
  mkdir,
  readFile,
  readdir,
  rm,
  symlink,
  writeFile,
} from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  VALID_PRIVATE_KEY,
  assertNativeHandler,
  cliPath,
  createFixture,
  environment,
  legacyManagedConfig,
  managedHandler,
  readJson,
  registryModuleUrl,
  runHook,
  runNode,
  runPlugin,
  startApiServer,
  writeJson,
} from '../test-support/copilot-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

async function assertNoTransactionFiles(fixture) {
  const names = [];
  for (const directory of [fixture.agentDir, fixture.hooksDir, path.dirname(fixture.legacyPath)]) {
    try {
      names.push(...await readdir(directory));
    } catch (error) {
      if (error?.code !== 'ENOENT') throw error;
    }
  }
  assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false, names.join(', '));
}

test('GitHub Copilot CLI is registered with its native user hook directory', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('copilot'), {
    name: 'GitHub Copilot CLI',
    configDir: '~/.copilot/hooks',
    configFile: 'elydora-audit.json',
  });
});

test('Copilot installs five managed files, preserves valid hooks, and migrates legacy entries', async () => {
  const fixture = await createFixture({
    userConfig: {
      version: 1,
      disableAllHooks: false,
      owner: 'user',
      hooks: {
        sessionStart: [{ type: 'prompt', prompt: '/compact' }],
        preToolUse: [{ type: 'command', command: 'user-pre-hook', timeout: 3 }],
        postToolUse: [{ type: 'command', bash: 'user-post-hook' }],
        postToolUseFailure: [{ command: 'user-failure-hook', matcher: 'powershell' }],
      },
    },
  });
  try {
    await writeJson(fixture.legacyPath, legacyManagedConfig(fixture, {
      notification: [{
        type: 'command',
        command: 'user-notification-hook',
        matcher: 'agent_idle|permission_prompt',
      }],
    }));
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    const managedFiles = [
      fixture.guardScriptPath,
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
      fixture.hookScriptPath,
      fixture.configPath,
    ];
    const snapshot = new Map(await Promise.all(managedFiles.map(async (filePath) => [
      filePath,
      await readFile(filePath, 'utf-8'),
    ])));
    const config = JSON.parse(snapshot.get(fixture.configPath));
    assert.equal(config.owner, 'user');
    assert.equal(config.disableAllHooks, false);
    assert.deepEqual(config.hooks.sessionStart, [{ type: 'prompt', prompt: '/compact' }]);
    assert.equal(config.hooks.preToolUse.length, 2);
    assert.equal(config.hooks.postToolUse.length, 2);
    assert.equal(config.hooks.postToolUseFailure.length, 2);
    assertNativeHandler(managedHandler(config, 'preToolUse', 'guard.js'));
    assertNativeHandler(managedHandler(config, 'postToolUse', 'hook.js'));
    assertNativeHandler(managedHandler(config, 'postToolUseFailure', 'hook.js'));
    assert.deepEqual(await readJson(path.join(fixture.agentDir, 'config.json')), {
      org_id: 'org-1',
      agent_id: fixture.agentId,
      kid: 'kid-1',
      base_url: fixture.baseUrl,
      token: 'token-1',
      agent_name: 'copilot',
    });
    assert.equal(await readFile(path.join(fixture.agentDir, 'private.key'), 'utf-8'), VALID_PRIVATE_KEY);
    assert.deepEqual(await readJson(fixture.legacyPath), {
      version: 1,
      hooks: {
        notification: [{
          type: 'command',
          command: 'user-notification-hook',
          matcher: 'agent_idle|permission_prompt',
        }],
      },
    });

    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    for (const [filePath, source] of snapshot) {
      assert.equal(await readFile(filePath, 'utf-8'), source, filePath);
    }
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Copilot migration removes a legacy file owned entirely by Elydora', async () => {
  const fixture = await createFixture();
  try {
    await writeJson(fixture.legacyPath, legacyManagedConfig(fixture));
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    await assertMissing(fixture.legacyPath);
    assertNativeHandler(managedHandler(await readJson(fixture.configPath), 'preToolUse', 'guard.js'));
  } finally {
    await fixture.close();
  }
});

test('Copilot guard blocks frozen agents and audit records success and failure payloads', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const config = await readJson(fixture.configPath);
    const guard = managedHandler(config, 'preToolUse', 'guard.js');
    const successAudit = managedHandler(config, 'postToolUse', 'hook.js');
    const failureAudit = managedHandler(config, 'postToolUseFailure', 'hook.js');
    const prePayload = {
      sessionId: 'session-1',
      timestamp: 1784486400000,
      cwd: fixture.projectDir,
      toolName: 'powershell',
      toolArgs: { command: 'Get-ChildItem' },
    };
    await writeJson(path.join(fixture.agentDir, 'status-cache.json'), {
      status: 'frozen',
      cached_at: Date.now(),
    });
    let result = await runHook(guard, fixture, JSON.stringify(prePayload));
    assert.equal(result.code, 2, result.stderr);
    assert.equal(result.stdout, '');
    assert.match(result.stderr, /Agent "copilot" is frozen/);

    await writeJson(path.join(fixture.agentDir, 'status-cache.json'), {
      status: 'active',
      cached_at: Date.now(),
    });
    result = await runHook(guard, fixture, JSON.stringify(prePayload));
    assert.equal(result.code, 0, result.stderr);
    assert.equal(result.stdout, '');

    const successPayload = {
      ...prePayload,
      toolResult: { resultType: 'success', textResultForLlm: 'ok' },
    };
    result = await runHook(successAudit, fixture, JSON.stringify(successPayload));
    assert.equal(result.code, 0, result.stderr);
    const failurePayload = { ...prePayload, error: 'command failed' };
    result = await runHook(failureAudit, fixture, JSON.stringify(failurePayload));
    assert.equal(result.code, 0, result.stderr);

    const operations = api.requests
      .filter((request) => request.method === 'POST')
      .map((request) => JSON.parse(request.raw));
    assert.equal(operations.length, 2);
    assert.deepEqual(operations[0].payload, successPayload);
    assert.deepEqual(operations[1].payload, failurePayload);
    assert.deepEqual(operations[1].subject, { session_id: 'session-1' });
    assert.deepEqual(operations[1].action, { tool: 'powershell' });
  } finally {
    await api.close();
    await fixture.close();
  }
});

test('Copilot uses the official default for an empty COPILOT_HOME', async () => {
  const fixture = await createFixture();
  try {
    const result = await fixture.install({}, '');
    assert.equal(result.code, 0, result.stderr);
    const defaultPath = path.join(fixture.homeDir, '.copilot', 'hooks', 'elydora-audit.json');
    assertNativeHandler(managedHandler(await readJson(defaultPath), 'preToolUse', 'guard.js'));
    const status = await runPlugin(fixture, 'status', null, '');
    assert.equal(JSON.parse(status.stdout).configPath, defaultPath);
  } finally {
    await fixture.close();
  }
});

test('Copilot status requires the complete active contract and exact runtime sources', async (t) => {
  const cases = [
    ['missing failure event', 'config', /"hookConfigured":false/],
    ['tampered guard', 'guard', /"installed":false/],
    ['malformed runtime config', 'runtime', /parse Elydora runtime config/i],
    ['invalid private key', 'key', /private key is invalid/i],
  ];
  for (const [label, target, expected] of cases) {
    await t.test(label, async () => {
      const fixture = await createFixture();
      try {
        assert.equal((await fixture.install()).code, 0);
        const healthy = await runPlugin(fixture, 'status', null);
        assert.deepEqual(JSON.parse(healthy.stdout), {
          installed: true,
          agentName: 'copilot',
          displayName: 'GitHub Copilot CLI',
          hookConfigured: true,
          hookScriptExists: true,
          configPath: fixture.configPath,
        });
        if (target === 'config') {
          const config = await readJson(fixture.configPath);
          delete config.hooks.postToolUseFailure;
          await writeJson(fixture.configPath, config);
        } else if (target === 'guard') {
          await writeFile(fixture.guardScriptPath, 'tampered\n');
        } else if (target === 'runtime') {
          await writeFile(path.join(fixture.agentDir, 'config.json'), '{ malformed');
        } else {
          await writeFile(path.join(fixture.agentDir, 'private.key'), 'invalid');
        }
        const status = await runPlugin(fixture, 'status', null);
        assert.match(`${status.stdout}\n${status.stderr}`, expected);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Copilot resolves disableAllHooks through the official settings precedence', async (t) => {
  await t.test('managed file flag blocks installation', async () => {
    const fixture = await createFixture({
      userConfig: { version: 1, disableAllHooks: true, hooks: {} },
      localSettings: { disableAllHooks: false },
    });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /hooks are disabled/i);
      await assertMissing(fixture.agentDir);
    } finally {
      await fixture.close();
    }
  });

  await t.test('local repository settings re-enable user-disabled hooks', async () => {
    const fixture = await createFixture({
      userSettings: '{\n  // user pause\n  "disableAllHooks": true,\n}\n',
      localSettings: { disableAllHooks: false },
    });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 0, result.stderr);
    } finally {
      await fixture.close();
    }
  });

  await t.test('repository settings disable every non-policy source', async () => {
    const fixture = await createFixture({
      userSettings: { disableAllHooks: false },
      repositorySettings: { disableAllHooks: true },
    });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /Copilot repository settings/);
      await assertMissing(fixture.agentDir);
    } finally {
      await fixture.close();
    }
  });

  await t.test('GitHub repository settings override Claude local settings', async () => {
    const fixture = await createFixture({
      claudeLocalSettings: { disableAllHooks: true },
      repositorySettings: { disableAllHooks: false },
    });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 0, result.stderr);
    } finally {
      await fixture.close();
    }
  });
});

test('Copilot rejects malformed current schemas and preserves every source', async () => {
  const cases = [
    '{ malformed',
    '{"version":1,"version":1,"hooks":{}}',
    { version: 2, hooks: {} },
    { version: 1, hooks: { futureEvent: [] } },
    { version: 1, hooks: { preToolUse: [{ type: 'unknown' }] } },
    { version: 1, hooks: { postToolUse: [{ type: 'http', url: 'http://example.com' }] } },
    { version: 1, hooks: { sessionEnd: [{ type: 'prompt', prompt: 'continue' }] } },
  ];
  for (const userConfig of cases) {
    const fixture = await createFixture({ userConfig });
    try {
      const before = await readFile(fixture.configPath, 'utf-8');
      const result = await fixture.install();
      assert.equal(result.code, 1, `${JSON.stringify(userConfig)}\n${result.stderr}`);
      assert.equal(await readFile(fixture.configPath, 'utf-8'), before);
      await assertMissing(fixture.agentDir);
    } finally {
      await fixture.close();
    }
  }
});

test('Copilot uninstall removes exact ownership and preserves adjacent handlers', async () => {
  const fixture = await createFixture({
    userConfig: { version: 1, hooks: { notification: [{ command: 'keep' }] } },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    const config = await readJson(fixture.configPath);
    config.hooks.preToolUse.push({
      type: 'command',
      bash: `'${process.execPath}' '${path.join(fixture.homeDir, '.elydora', 'agent-10', 'guard.js')}'`,
      powershell: 'user-decoy',
      timeoutSec: 10,
    });
    await writeJson(fixture.configPath, config);
    assert.equal((await runPlugin(fixture, 'uninstall', 'agent-10')).code, 0);
    assert.equal((await readJson(fixture.configPath)).hooks.postToolUseFailure.length, 1);

    const result = await runPlugin(fixture, 'uninstall', fixture.agentId);
    assert.equal(result.code, 0, result.stderr);
    const remaining = await readJson(fixture.configPath);
    assert.deepEqual(remaining.hooks.notification, [{ command: 'keep' }]);
    assert.equal(remaining.hooks.postToolUse, undefined);
    assert.equal(remaining.hooks.postToolUseFailure, undefined);
    assert.equal(remaining.hooks.preToolUse.length, 1);
    assert.match(remaining.hooks.preToolUse[0].bash, /agent-10/);
  } finally {
    await fixture.close();
  }
});

test('Copilot rejects linked hook and runtime paths before writes', async (t) => {
  for (const kind of ['home', 'hook', 'runtime']) {
    await t.test(kind, async () => {
      const fixture = await createFixture();
      try {
        const target = path.join(fixture.rootDir, `${kind}-target`);
        await mkdir(target, { recursive: true });
        if (kind === 'home') {
          await rm(fixture.copilotHome, { recursive: true, force: true });
          await symlink(target, fixture.copilotHome, 'junction');
        } else if (kind === 'hook') {
          await mkdir(fixture.hooksDir, { recursive: true });
          const targetFile = path.join(target, 'config.json');
          await writeJson(targetFile, { version: 1, hooks: {} });
          await symlink(targetFile, fixture.configPath, 'file');
        } else {
          await mkdir(fixture.homeDir, { recursive: true });
          await symlink(target, path.join(fixture.homeDir, '.elydora'), 'junction');
        }
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, /physical (directory|file)/i);
      } catch (error) {
        if (error?.code === 'EPERM') {
          t.skip(`symbolic links unavailable: ${error.message}`);
          return;
        }
        throw error;
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Copilot CLI preflight blocks disabled hooks before runtime creation', async () => {
  const fixture = await createFixture({ repositorySettings: { disableAllHooks: true } });
  const privateKeyFile = path.join(fixture.rootDir, 'private-key');
  try {
    await writeFile(privateKeyFile, VALID_PRIVATE_KEY, { mode: 0o600 });
    const result = await runNode([
      '--no-warnings',
      cliPath,
      'install',
      '--agent', 'copilot',
      '--org_id', 'org-1',
      '--agent_id', fixture.agentId,
      '--kid', 'kid-1',
      '--private_key_file', privateKeyFile,
      '--base_url', fixture.baseUrl,
    ], environment(fixture), fixture.projectDir);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /hooks are disabled/i);
    await assertMissing(path.join(fixture.homeDir, '.elydora'));
    await assertMissing(fixture.configPath);
  } finally {
    await fixture.close();
  }
});
