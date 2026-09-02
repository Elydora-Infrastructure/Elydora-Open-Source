import assert from 'node:assert/strict';
import {
  lstat,
  mkdir,
  readFile,
  readdir,
  rm,
  stat,
  symlink,
  writeFile,
} from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  VALID_PRIVATE_KEY,
  cliPath,
  createFixture,
  environment,
  findV3Hook,
  managedV2Document,
  readJson,
  registryModuleUrl,
  runNode,
  runPlugin,
  runShell,
  startApiServer,
} from '../test-support/kirocli-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

async function writeJson(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
}

function userV3Hook() {
  return {
    name: 'global-context',
    trigger: 'SessionStart',
    action: { type: 'agent', prompt: 'Read AGENTS.md' },
    enabled: true,
  };
}

function legacyCommand(scriptPath) {
  if (process.platform === 'win32') return `"${process.execPath}" "${scriptPath}"`;
  const quote = (value) => `'${value.replaceAll("'", `'"'"'`)}'`;
  return `${quote(process.execPath)} ${quote(scriptPath)}`;
}

function legacyDocuments(fixture) {
  const guard = legacyCommand(fixture.guardScriptPath);
  const audit = legacyCommand(fixture.hookScriptPath);
  return {
    v2: managedV2Document({
      hooks: {
        preToolUse: [{ matcher: '*', command: guard, timeout_ms: 5000 }],
        postToolUse: [{ matcher: '*', command: audit, timeout_ms: 5000 }],
      },
    }),
    v3: {
      version: 'v1',
      hooks: [
        {
          name: 'elydora-guard',
          description: 'Block tool use when the Elydora agent is frozen',
          trigger: 'PreToolUse',
          matcher: '.*',
          action: { type: 'command', command: guard },
          timeout: 5,
          enabled: true,
        },
        {
          name: 'elydora-audit',
          description: 'Record tool use in the Elydora audit trail',
          trigger: 'PostToolUse',
          matcher: '.*',
          action: { type: 'command', command: audit },
          timeout: 5,
          enabled: true,
        },
      ],
    },
  };
}

test('Kiro CLI installs exact v2 and 2.13 v3 global contracts atomically', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('kirocli'), {
    name: 'Kiro CLI',
    configDir: '~/.kiro/hooks',
    configFile: 'elydora-audit.json',
  });
  const fixture = await createFixture({
    existingV2: managedV2Document({
      owner: 'user',
      hooks: {
        agentSpawn: [{ command: 'existing-spawn' }],
        preToolUse: [{ matcher: 'read', command: 'existing-v2' }],
      },
    }),
    existingV3: { version: 'v1', owner: 'user', hooks: [userV3Hook()] },
  });
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /2\.13\.0\+/);
    assert.match(first.stdout, /agent validate/);
    assert.match(first.stdout, /--agent elydora-audit/);
    const firstV2Source = await readFile(fixture.v2Path, 'utf-8');
    const firstV3Source = await readFile(fixture.v3Path, 'utf-8');
    const v2 = JSON.parse(firstV2Source);
    assert.equal(v2.owner, 'user');
    assert.deepEqual(v2.hooks.agentSpawn, [{ command: 'existing-spawn' }]);
    assert.equal(v2.hooks.preToolUse.length, 2);
    assert.deepEqual(v2.hooks.preToolUse[1], {
      matcher: '*',
      command: v2.hooks.preToolUse[1].command,
      timeout_ms: 10_000,
    });
    assert.deepEqual(v2.hooks.postToolUse[0], {
      matcher: '*',
      command: v2.hooks.postToolUse[0].command,
      timeout_ms: 10_000,
    });
    const v3 = JSON.parse(firstV3Source);
    assert.equal(v3.owner, 'user');
    assert.deepEqual(v3.hooks[0], userV3Hook());
    assert.deepEqual(findV3Hook(v3, 'elydora-guard'), {
      name: 'elydora-guard',
      description: 'Block tool use when the Elydora agent is frozen',
      trigger: 'PreToolUse',
      matcher: '.*',
      action: { type: 'command', command: findV3Hook(v3, 'elydora-guard').action.command },
      timeout: 10,
      enabled: true,
    });
    assert.equal(findV3Hook(v3, 'elydora-audit').trigger, 'PostToolUse');
    assert.deepEqual(await readJson(path.join(fixture.agentDir, 'config.json')), {
      org_id: 'org-1',
      agent_id: fixture.agentId,
      kid: 'kid-1',
      base_url: fixture.baseUrl,
      token: 'token-1',
      agent_name: 'kirocli',
    });
    assert.equal(await readFile(path.join(fixture.agentDir, 'private.key'), 'utf-8'), VALID_PRIVATE_KEY);
    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.v2Path, 'utf-8'), firstV2Source);
    assert.equal(await readFile(fixture.v3Path, 'utf-8'), firstV3Source);
    const names = [
      ...await readdir(path.dirname(fixture.v2Path)),
      ...await readdir(path.dirname(fixture.v3Path)),
      ...await readdir(fixture.agentDir),
    ];
    assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false);
    if (process.platform !== 'win32') {
      assert.equal((await stat(fixture.v2Path)).mode & 0o777, 0o600);
      assert.equal((await stat(fixture.guardScriptPath)).mode & 0o777, 0o700);
    }
  } finally {
    await fixture.close();
  }
});

test('Kiro CLI commands block frozen agents and preserve native events', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const v2 = await readJson(fixture.v2Path);
    const v3 = await readJson(fixture.v3Path);
    assert.equal(v2.hooks.preToolUse[0].command, findV3Hook(v3, 'elydora-guard').action.command);
    const payload = {
      hook_event_name: 'preToolUse',
      cwd: fixture.projectDir,
      session_id: 'session-1',
      tool_name: 'execute_bash',
      tool_input: { command: 'npm test' },
      future_field: { retained: true },
    };
    await writeJson(path.join(fixture.agentDir, 'status-cache.json'), {
      status: 'frozen',
      cached_at: Date.now(),
    });
    const guard = await runShell(
      findV3Hook(v3, 'elydora-guard').action.command,
      environment(fixture),
      fixture.projectDir,
      JSON.stringify(payload),
    );
    assert.equal(guard.code, 2, guard.stderr);
    await rm(path.join(fixture.agentDir, 'status-cache.json'));
    payload.hook_event_name = 'postToolUse';
    payload.tool_response = { success: true, result: '12 passed' };
    const audit = await runShell(
      findV3Hook(v3, 'elydora-audit').action.command,
      environment(fixture),
      fixture.projectDir,
      JSON.stringify(payload),
    );
    assert.equal(audit.code, 0, audit.stderr);
    const operation = JSON.parse(api.requests.find(({ method }) => method === 'POST').raw);
    assert.deepEqual(operation.payload, payload);
    assert.deepEqual(operation.action, { tool: 'execute_bash' });
    assert.deepEqual(operation.subject, { session_id: 'session-1' });
  } finally {
    await api.close();
    await fixture.close();
  }
});

test('Kiro CLI status requires both exact contracts and physical runtime identity', async (t) => {
  await t.test('missing home is an empty installation', async () => {
    const fixture = await createFixture();
    try {
      await rm(fixture.homeDir, { recursive: true });
      const status = await runPlugin(fixture, 'status', null);
      assert.equal(status.code, 0, status.stderr);
      assert.equal(JSON.parse(status.stdout).installed, false);
    } finally {
      await fixture.close();
    }
  });
  await t.test('missing or disabled contract is degraded and reinstall repairs it', async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      let status = await runPlugin(fixture, 'status', null);
      assert.equal(JSON.parse(status.stdout).installed, true);
      const v3 = await readJson(fixture.v3Path);
      findV3Hook(v3, 'elydora-guard').enabled = false;
      await writeJson(fixture.v3Path, v3);
      status = await runPlugin(fixture, 'status', null);
      assert.equal(JSON.parse(status.stdout).installed, false);
      assert.equal((await fixture.install()).code, 0);
      await rm(fixture.v2Path);
      status = await runPlugin(fixture, 'status', null);
      assert.equal(JSON.parse(status.stdout).hookConfigured, false);
    } finally {
      await fixture.close();
    }
  });
  await t.test('recognized altered hooks degrade status', async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      const v2 = await readJson(fixture.v2Path);
      v2.hooks.preToolUse.push({ ...v2.hooks.preToolUse[0], timeout_ms: 5000 });
      await writeJson(fixture.v2Path, v2);
      let status = await runPlugin(fixture, 'status', null);
      assert.equal(JSON.parse(status.stdout).hookConfigured, false);

      assert.equal((await fixture.install()).code, 0);
      const v3 = await readJson(fixture.v3Path);
      v3.hooks.push({ ...findV3Hook(v3, 'elydora-guard'), enabled: false });
      await writeJson(fixture.v3Path, v3);
      status = await runPlugin(fixture, 'status', null);
      assert.equal(JSON.parse(status.stdout).hookConfigured, false);
    } finally {
      await fixture.close();
    }
  });
  for (const [label, target, contents, expected] of [
    ['guard runtime', 'guard', 'tampered\n', /"installed":false/],
    ['runtime config', 'config', '{ malformed', /parse Elydora runtime config/i],
    ['runtime identity', 'config', JSON.stringify({
      org_id: 'org-1',
      agent_id: 'agent-1',
      kid: 'kid-1',
      base_url: 'http://127.0.0.1:9',
      token: 'token-1',
      agent_name: 'kiroide',
    }), /runtime identity/i],
    ['private key', 'key', 'invalid', /private key is invalid/i],
  ]) {
    await t.test(label, async () => {
      const fixture = await createFixture();
      try {
        assert.equal((await fixture.install()).code, 0);
        const targets = {
          guard: fixture.guardScriptPath,
          config: path.join(fixture.agentDir, 'config.json'),
          key: path.join(fixture.agentDir, 'private.key'),
        };
        await writeFile(targets[target], contents);
        const status = await runPlugin(fixture, 'status', null);
        assert.match(`${status.stdout}${status.stderr}`, expected);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Kiro CLI rejects malformed, colliding, invalid, and linked inputs before writes', async (t) => {
  for (const [label, options, pattern] of [
    ['duplicate v2', { existingV2: '{"name":"elydora-audit","name":"other"}' }, /duplicate/i],
    ['v2 path collision', { existingV2: { name: 'user-agent', hooks: {} } }, /v2 agent path conflicts/i],
    ['v3 name collision', { existingV3: { version: 'v1', hooks: [{ name: 'elydora-guard', trigger: 'PreToolUse', action: { type: 'command', command: 'user-command' } }] } }, /hook name.*conflicts/i],
  ]) {
    await t.test(label, async () => {
      const fixture = await createFixture(options);
      try {
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, pattern);
        await assertMissing(fixture.guardScriptPath);
      } finally {
        await fixture.close();
      }
    });
  }
  await t.test('non-canonical private key', async () => {
    const fixture = await createFixture();
    try {
      const result = await fixture.install({ privateKey: 'invalid' });
      assert.equal(result.code, 1);
      assert.match(result.stderr, /canonical 32-byte/);
      await assertMissing(fixture.v2Path);
      await assertMissing(fixture.v3Path);
    } finally {
      await fixture.close();
    }
  });
  await t.test('linked Kiro directory', async () => {
    const fixture = await createFixture();
    const external = path.join(fixture.rootDir, 'external');
    try {
      await mkdir(external);
      await symlink(external, path.join(fixture.homeDir, '.kiro'), 'junction');
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /not a physical directory|symbolic link/i);
      await assertMissing(path.join(external, 'agents', 'elydora-audit.json'));
    } finally {
      await fixture.close();
    }
  });
});

test('Kiro CLI migrates exact legacy commands and repairs both schemas', async () => {
  const fixture = await createFixture();
  try {
    const legacy = legacyDocuments(fixture);
    await writeJson(fixture.v2Path, legacy.v2);
    await writeJson(fixture.v3Path, legacy.v3);
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    const v2 = await readJson(fixture.v2Path);
    const v3 = await readJson(fixture.v3Path);
    assert.equal(v2.hooks.preToolUse.length, 1);
    assert.equal(v2.hooks.preToolUse[0].timeout_ms, 10_000);
    assert.equal(findV3Hook(v3, 'elydora-guard').timeout, 10);
    assert.equal(findV3Hook(v3, 'elydora-guard').action.command, v2.hooks.preToolUse[0].command);
  } finally {
    await fixture.close();
  }
});

test('Kiro CLI uninstall preserves user hooks and removes fully owned files', async (t) => {
  await t.test('mixed files', async () => {
    const fixture = await createFixture({
      existingV2: managedV2Document({ hooks: { agentSpawn: [{ command: 'existing-spawn' }] } }),
      existingV3: { version: 'v1', hooks: [userV3Hook()] },
    });
    try {
      assert.equal((await fixture.install()).code, 0);
      const result = await runPlugin(fixture, 'uninstall', fixture.agentId);
      assert.equal(result.code, 0, result.stderr);
      const v2 = await readJson(fixture.v2Path);
      assert.deepEqual(v2.hooks, { agentSpawn: [{ command: 'existing-spawn' }] });
      const v3 = await readJson(fixture.v3Path);
      assert.deepEqual(v3.hooks, [userV3Hook()]);
    } finally {
      await fixture.close();
    }
  });
  await t.test('fully owned files', async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      const result = await runPlugin(fixture, 'uninstall', fixture.agentId);
      assert.equal(result.code, 0, result.stderr);
      await assertMissing(fixture.v2Path);
      await assertMissing(fixture.v3Path);
    } finally {
      await fixture.close();
    }
  });
  await t.test('legacy files', async () => {
    const fixture = await createFixture();
    try {
      const legacy = legacyDocuments(fixture);
      await writeJson(fixture.v2Path, legacy.v2);
      await writeJson(fixture.v3Path, legacy.v3);
      const result = await runPlugin(fixture, 'uninstall', fixture.agentId);
      assert.equal(result.code, 0, result.stderr);
      await assertMissing(fixture.v2Path);
      await assertMissing(fixture.v3Path);
    } finally {
      await fixture.close();
    }
  });
  await t.test('unrelated v3 source bytes', async () => {
    const source = '{ "version": "v1", "hooks": [{ "name": "user", "trigger": "SessionStart", "action": { "type": "command", "command": "echo user" } }] }\n';
    const fixture = await createFixture({ existingV3: source });
    try {
      const result = await runPlugin(fixture, 'uninstall', fixture.agentId);
      assert.equal(result.code, 0, result.stderr);
      assert.equal(await readFile(fixture.v3Path, 'utf-8'), source);
    } finally {
      await fixture.close();
    }
  });
});

test('Kiro CLI command completes install, status, and uninstall end to end', async () => {
  const fixture = await createFixture();
  const privateKeyFile = path.join(fixture.projectDir, 'private-key.txt');
  const tokenFile = path.join(fixture.projectDir, 'token.txt');
  try {
    await writeFile(privateKeyFile, `${VALID_PRIVATE_KEY}\n`, { mode: 0o600 });
    await writeFile(tokenFile, 'token-1\n', { mode: 0o600 });
    const install = await runNode([
      cliPath, 'install', '--agent', 'kirocli', '--org_id', 'org-1',
      '--agent_id', fixture.agentId, '--kid', 'kid-1',
      '--private_key_file', privateKeyFile, '--token_file', tokenFile,
      '--base_url', fixture.baseUrl,
    ], environment(fixture), fixture.projectDir);
    assert.equal(install.code, 0, install.stderr);
    const status = await runNode([cliPath, 'status'], environment(fixture), fixture.projectDir);
    assert.equal(status.code, 0, status.stderr);
    assert.match(status.stdout, /Kiro CLI \(kirocli\) \[installed\]/);
    const uninstall = await runNode([
      cliPath, 'uninstall', '--agent', 'kirocli', '--agent_id', fixture.agentId,
    ], environment(fixture), fixture.projectDir);
    assert.equal(uninstall.code, 0, uninstall.stderr);
    await assertMissing(fixture.v2Path);
    await assertMissing(fixture.v3Path);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
