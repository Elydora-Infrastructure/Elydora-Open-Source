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
  findHook,
  installConfig,
  readJson,
  registryModuleUrl,
  runNode,
  runPlugin,
  runShell,
  startApiServer,
} from '../test-support/kiroide-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

function userHook() {
  return {
    name: 'workspace-context',
    description: 'Load workspace context',
    trigger: 'SessionStart',
    action: { type: 'agent', prompt: 'Read AGENTS.md' },
    enabled: true,
  };
}

function legacyDocument(fixture, agentId = fixture.agentId) {
  const agentDirectory = path.join(fixture.homeDir, '.elydora', agentId);
  return {
    name: 'Elydora Audit',
    description: 'Sends tool-use events to the Elydora tamper-evident audit platform',
    version: '1.0.0',
    hooks: {
      pre_tool_use: {
        command: `node "${path.join(agentDirectory, 'guard.js')}"`,
        timeout_ms: 5000,
      },
      post_tool_use: {
        command: `node "${path.join(agentDirectory, 'hook.js')}"`,
        timeout_ms: 5000,
      },
    },
  };
}

async function writeJson(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
}

test('Kiro IDE uses the workspace v1 hook contract', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('kiroide'), {
    name: 'Kiro IDE',
    configDir: '.kiro/hooks',
    configFile: 'elydora-audit.json',
  });

  const fixture = await createFixture({
    existingConfig: { version: 'v1', owner: 'workspace', hooks: [userHook()] },
  });
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /workspace hooks/);
    assert.match(first.stdout, /Agent Hooks panel/);
    const firstSource = await readFile(fixture.configPath, 'utf-8');
    const document = JSON.parse(firstSource);
    assert.equal(document.version, 'v1');
    assert.equal(document.owner, 'workspace');
    assert.deepEqual(document.hooks[0], userHook());
    assert.deepEqual(findHook(document, 'elydora-guard'), {
      name: 'elydora-guard',
      description: 'Block tool use when the Elydora agent is frozen',
      trigger: 'PreToolUse',
      matcher: '.*',
      action: {
        type: 'command',
        command: findHook(document, 'elydora-guard').action.command,
      },
      timeout: 10,
      enabled: true,
    });
    assert.deepEqual(findHook(document, 'elydora-audit'), {
      name: 'elydora-audit',
      description: 'Record tool use in the Elydora audit trail',
      trigger: 'PostToolUse',
      matcher: '.*',
      action: {
        type: 'command',
        command: findHook(document, 'elydora-audit').action.command,
      },
      timeout: 10,
      enabled: true,
    });
    assert.deepEqual(await readJson(path.join(fixture.agentDir, 'config.json')), {
      org_id: 'org-1',
      agent_id: fixture.agentId,
      kid: 'kid-1',
      base_url: fixture.baseUrl,
      token: 'token-1',
      agent_name: 'kiroide',
    });
    assert.equal(await readFile(path.join(fixture.agentDir, 'private.key'), 'utf-8'), VALID_PRIVATE_KEY);

    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.configPath, 'utf-8'), firstSource);
    const transactionFiles = [
      ...await readdir(fixture.hooksDir),
      ...await readdir(fixture.agentDir),
    ];
    assert.equal(transactionFiles.some((name) => /\.(tmp|rollback)$/.test(name)), false);
    await assertMissing(path.join(fixture.homeDir, '.kiro', 'hooks', 'elydora-audit.json'));
    if (process.platform !== 'win32') {
      assert.equal((await stat(fixture.agentDir)).mode & 0o777, 0o700);
      assert.equal((await stat(fixture.configPath)).mode & 0o777, 0o600);
      assert.equal((await stat(fixture.guardScriptPath)).mode & 0o777, 0o700);
    }
  } finally {
    await fixture.close();
  }
});

test('Kiro IDE commands block frozen agents and preserve the native event', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const document = await readJson(fixture.configPath);
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
      findHook(document, 'elydora-guard').action.command,
      environment(fixture),
      fixture.projectDir,
      JSON.stringify(payload),
    );
    assert.equal(guard.code, 2, guard.stderr);
    assert.match(guard.stderr, /Tool execution blocked/);

    await rm(path.join(fixture.agentDir, 'status-cache.json'));
    payload.hook_event_name = 'postToolUse';
    payload.tool_response = { success: true, result: '12 passed' };
    const audit = await runShell(
      findHook(document, 'elydora-audit').action.command,
      environment(fixture),
      fixture.projectDir,
      JSON.stringify(payload),
    );
    assert.equal(audit.code, 0, audit.stderr);
    const request = api.requests.find(({ method }) => method === 'POST');
    assert.ok(request);
    const operation = JSON.parse(request.raw);
    assert.deepEqual(operation.payload, payload);
    assert.deepEqual(operation.action, { tool: 'execute_bash' });
    assert.deepEqual(operation.subject, { session_id: 'session-1' });
  } finally {
    await api.close();
    await fixture.close();
  }
});

test('Kiro IDE status requires the exact enabled contract and physical runtime', async (t) => {
  await t.test('disabled hook is inactive and reinstall repairs it', async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      const document = await readJson(fixture.configPath);
      findHook(document, 'elydora-guard').enabled = false;
      await writeJson(fixture.configPath, document);
      let status = await runPlugin(fixture, 'status', null);
      assert.deepEqual(JSON.parse(status.stdout), {
        installed: false,
        agentName: 'kiroide',
        displayName: 'Kiro IDE',
        hookConfigured: false,
        hookScriptExists: false,
        configPath: fixture.configPath,
      });
      assert.equal((await fixture.install()).code, 0);
      status = await runPlugin(fixture, 'status', null);
      assert.equal(JSON.parse(status.stdout).installed, true);
    } finally {
      await fixture.close();
    }
  });

  await t.test('recognized altered hook degrades status', async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      const document = await readJson(fixture.configPath);
      document.hooks.push({ ...findHook(document, 'elydora-guard'), enabled: false });
      await writeJson(fixture.configPath, document);
      const status = await runPlugin(fixture, 'status', null);
      assert.equal(JSON.parse(status.stdout).hookConfigured, false);
    } finally {
      await fixture.close();
    }
  });

  for (const [label, target, contents, expected] of [
    ['guard runtime', 'guard', 'tampered\n', /"installed":false/],
    ['runtime config', 'config', '{ malformed', /parse Elydora runtime config/i],
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
        assert.match(`${status.stdout}\n${status.stderr}`, expected);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Kiro IDE rejects malformed files, name collisions, and unsafe workspaces before writes', async (t) => {
  await t.test('malformed hook file', async () => {
    const fixture = await createFixture({ existingConfig: '{ malformed' });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /parse Kiro IDE hooks/i);
      assert.equal(await readFile(fixture.configPath, 'utf-8'), '{ malformed');
      await assertMissing(fixture.agentDir);
    } finally {
      await fixture.close();
    }
  });

  await t.test('managed name collision', async () => {
    const existing = {
      version: 'v1',
      hooks: [{
        name: 'elydora-guard',
        trigger: 'PreToolUse',
        action: { type: 'command', command: 'user-command' },
      }],
    };
    const fixture = await createFixture({ existingConfig: existing });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /conflicts with the Elydora contract/i);
      assert.deepEqual(await readJson(fixture.configPath), existing);
      await assertMissing(fixture.agentDir);
    } finally {
      await fixture.close();
    }
  });

  await t.test('non-canonical private key', async () => {
    const fixture = await createFixture();
    try {
      const result = await fixture.install({ privateKey: `${VALID_PRIVATE_KEY}=` });
      assert.equal(result.code, 1);
      assert.match(result.stderr, /canonical 32-byte base64url/i);
      await assertMissing(fixture.agentDir);
      await assertMissing(fixture.configPath);
    } finally {
      await fixture.close();
    }
  });

  await t.test('linked .kiro directory', async () => {
    const fixture = await createFixture();
    const external = path.join(fixture.rootDir, 'external-kiro');
    const kiroDirectory = path.join(fixture.projectDir, '.kiro');
    try {
      await mkdir(external);
      await symlink(external, kiroDirectory, process.platform === 'win32' ? 'junction' : 'dir');
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /not a physical directory/i);
      await assertMissing(path.join(external, 'hooks', 'elydora-audit.json'));
      await assertMissing(fixture.agentDir);
    } finally {
      await fixture.close();
    }
  });
});

test('Kiro IDE migrates only the matching legacy global hook', async () => {
  const fixture = await createFixture();
  try {
    await writeJson(fixture.legacyConfigPath, legacyDocument(fixture));
    assert.equal((await fixture.install()).code, 0);
    await assertMissing(fixture.legacyConfigPath);

    await writeJson(fixture.legacyConfigPath, legacyDocument(fixture, 'agent-2'));
    assert.equal((await fixture.install()).code, 0);
    assert.equal((await readJson(fixture.legacyConfigPath)).version, '1.0.0');
  } finally {
    await fixture.close();
  }
});

test('Kiro IDE uninstall removes exact ownership and preserves workspace hooks', async () => {
  const fixture = await createFixture({
    existingConfig: { version: 'v1', owner: 'workspace', hooks: [userHook()] },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    assert.equal((await runPlugin(fixture, 'uninstall', 'agent-2')).code, 0);
    assert.equal((await readJson(fixture.configPath)).hooks.length, 3);
    assert.equal((await runPlugin(fixture, 'uninstall', fixture.agentId)).code, 0);
    assert.deepEqual(await readJson(fixture.configPath), {
      version: 'v1',
      owner: 'workspace',
      hooks: [userHook()],
    });
  } finally {
    await fixture.close();
  }

  const owned = await createFixture();
  try {
    assert.equal((await owned.install()).code, 0);
    assert.equal((await runPlugin(owned, 'uninstall', owned.agentId)).code, 0);
    await assertMissing(owned.configPath);
  } finally {
    await owned.close();
  }

  const source = '{ "version": "v1", "hooks": [{ "name": "user", "trigger": "SessionStart", "action": { "type": "command", "command": "echo user" } }] }\n';
  const unrelated = await createFixture({ existingConfig: source });
  try {
    assert.equal((await runPlugin(unrelated, 'uninstall', unrelated.agentId)).code, 0);
    assert.equal(await readFile(unrelated.configPath, 'utf-8'), source);
  } finally {
    await unrelated.close();
  }
});

test('Kiro IDE CLI completes install, status, and uninstall in the current workspace', async () => {
  const fixture = await createFixture();
  const privateKeyFile = path.join(fixture.rootDir, 'private.key');
  const tokenFile = path.join(fixture.rootDir, 'token');
  try {
    await writeFile(privateKeyFile, VALID_PRIVATE_KEY, { mode: 0o600 });
    await writeFile(tokenFile, 'token-1', { mode: 0o600 });
    const install = await runNode([
      '--no-warnings',
      cliPath,
      'install',
      '--agent', 'kiroide',
      '--org_id', 'org-1',
      '--agent_id', fixture.agentId,
      '--kid', 'kid-1',
      '--private_key_file', privateKeyFile,
      '--token_file', tokenFile,
      '--base_url', fixture.baseUrl,
    ], environment(fixture), fixture.projectDir);
    assert.equal(install.code, 0, install.stderr);
    assert.match(install.stdout, /Kiro IDE workspace hooks/);
    const status = await runNode(
      ['--no-warnings', cliPath, 'status'],
      environment(fixture),
      fixture.projectDir,
    );
    assert.equal(status.code, 0, status.stderr);
    assert.match(status.stdout, /Kiro IDE \(kiroide\) \[installed\]/);
    const uninstall = await runNode([
      '--no-warnings',
      cliPath,
      'uninstall',
      '--agent', 'kiroide',
      '--agent_id', fixture.agentId,
    ], environment(fixture), fixture.projectDir);
    assert.equal(uninstall.code, 0, uninstall.stderr);
    await assertMissing(fixture.configPath);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
