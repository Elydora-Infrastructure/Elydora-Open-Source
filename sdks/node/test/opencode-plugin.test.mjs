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
import { pathToFileURL } from 'node:url';
import {
  VALID_PRIVATE_KEY,
  cliPath,
  contractModuleUrl,
  createFixture,
  environment,
  registryModuleUrl,
  runNode,
  runPlugin,
  startApiServer,
} from '../test-support/opencode-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

async function writeSource(filePath, source) {
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, source, { mode: 0o600 });
}

async function legacySource(fixture) {
  const { buildLegacyOpenCodePlugin } = await import(contractModuleUrl);
  return buildLegacyOpenCodePlugin(fixture.hookScriptPath, fixture.guardScriptPath);
}

async function runGeneratedHook(fixture, name, input, output) {
  const source = `
    const module = await import(${JSON.stringify(pathToFileURL(fixture.pluginPath).href)});
    const hooks = await module.ElydoraAuditPlugin({});
    await hooks[${JSON.stringify(name)}](
      JSON.parse(process.env.ELYDORA_TEST_INPUT),
      JSON.parse(process.env.ELYDORA_TEST_OUTPUT),
    );
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      ...environment(fixture),
      ELYDORA_TEST_INPUT: JSON.stringify(input),
      ELYDORA_TEST_OUTPUT: JSON.stringify(output),
    },
    fixture.projectDir,
  );
}

test('OpenCode installs the discoverable global JavaScript plugin and exact runtime', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('opencode'), {
    name: 'OpenCode',
    configDir: '.config/opencode/plugins',
    configFile: 'elydora-audit.js',
  });
  const fixture = await createFixture();
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /elydora-audit\.js/);
    assert.match(first.stdout, /restart active OpenCode sessions/);
    await assertMissing(fixture.legacyPluginPath);
    const firstSource = await readFile(fixture.pluginPath, 'utf-8');
    assert.match(firstSource, /@elydora-opencode-plugin/);
    assert.match(firstSource, /tool\.execute\.before/);
    assert.match(firstSource, /tool\.execute\.after/);
    assert.doesNotMatch(firstSource, /spawn\('node'/);
    const hooks = await fixture.loadPlugin();
    assert.deepEqual(Object.keys(hooks).sort(), ['tool.execute.after', 'tool.execute.before']);
    assert.deepEqual(
      JSON.parse(await readFile(path.join(fixture.agentDir, 'config.json'), 'utf-8')),
      {
        org_id: 'org-1',
        agent_id: fixture.agentId,
        kid: 'kid-1',
        base_url: fixture.baseUrl,
        token: 'token-1',
        agent_name: 'opencode',
      },
    );
    assert.equal(await readFile(path.join(fixture.agentDir, 'private.key'), 'utf-8'), VALID_PRIVATE_KEY);
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 0, status.stderr);
    assert.deepEqual(JSON.parse(status.stdout), {
      installed: true,
      agentName: 'opencode',
      displayName: 'OpenCode',
      hookConfigured: true,
      hookScriptExists: true,
      configPath: fixture.pluginPath,
    });
    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.pluginPath, 'utf-8'), firstSource);
    const names = [
      ...await readdir(fixture.pluginsDir),
      ...await readdir(fixture.agentDir),
    ];
    assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false);
    if (process.platform !== 'win32') {
      assert.equal((await stat(fixture.pluginPath)).mode & 0o777, 0o600);
      assert.equal((await stat(fixture.guardScriptPath)).mode & 0o777, 0o700);
    }
  } finally {
    await fixture.close();
  }
});

test('OpenCode guard enforces state and audit preserves the complete native event', async () => {
  const api = await startApiServer();
  const fixture = await createFixture({ baseUrl: api.baseUrl });
  try {
    assert.equal((await fixture.install()).code, 0);
    const beforeInput = { tool: 'bash', sessionID: 'session-1', callID: 'call-1' };
    const beforeOutput = { args: { command: 'echo test' } };
    const guard = await runGeneratedHook(
      fixture,
      'tool.execute.before',
      beforeInput,
      beforeOutput,
    );
    assert.equal(guard.code, 0, guard.stderr);
    assert.equal(api.requests.some(({ method }) => method === 'GET'), true);

    const afterInput = { ...beforeInput, args: beforeOutput.args };
    const afterOutput = { title: 'Shell', output: 'test', metadata: { exit: 0 } };
    const audit = await runGeneratedHook(
      fixture,
      'tool.execute.after',
      afterInput,
      afterOutput,
    );
    assert.equal(audit.code, 0, audit.stderr);
    const request = api.requests.find(({ method }) => method === 'POST');
    assert.ok(request);
    const operation = JSON.parse(request.raw);
    assert.deepEqual(operation.action, { tool: 'bash' });
    assert.deepEqual(operation.subject, { session_id: 'session-1' });
    assert.deepEqual(operation.payload, {
      hook_event_name: 'tool.execute.after',
      tool_name: 'bash',
      tool_input: { command: 'echo test' },
      session_id: 'session-1',
      call_id: 'call-1',
      input: afterInput,
      output: afterOutput,
    });

    api.setOperationStatus(503);
    const failedAudit = await runGeneratedHook(
      fixture,
      'tool.execute.after',
      afterInput,
      afterOutput,
    );
    assert.equal(failedAudit.code, 0, failedAudit.stderr);
    assert.match(failedAudit.stderr, /OpenCode audit runtime failed: \[Elydora audit\]/);
    assert.match(
      await readFile(path.join(fixture.agentDir, 'error.log'), 'utf-8'),
      /Audit API returned HTTP 503/,
    );

    await rm(path.join(fixture.agentDir, 'status-cache.json'));
    api.setStatus('frozen');
    const frozen = await runGeneratedHook(
      fixture,
      'tool.execute.before',
      beforeInput,
      beforeOutput,
    );
    assert.equal(frozen.code, 1);
    assert.match(frozen.stderr, /Agent "opencode" is frozen.*Tool execution blocked/s);
  } finally {
    await api.close();
    await fixture.close();
  }
});

test('OpenCode bridge forwards both native arguments and surfaces runtime failures', async () => {
  const fixture = await createFixture();
  const guardCapture = path.join(fixture.projectDir, 'guard-input.json');
  const auditCapture = path.join(fixture.projectDir, 'audit-input.json');
  try {
    assert.equal((await fixture.install()).code, 0);
    await writeFile(fixture.guardScriptPath, `
      const fs = require('node:fs');
      const chunks = [];
      process.stdin.on('data', (chunk) => chunks.push(chunk));
      process.stdin.on('end', () => {
        fs.writeFileSync(${JSON.stringify(guardCapture)}, Buffer.concat(chunks));
        process.stderr.write('[guard warning] degraded status API\\n');
      });
    `);
    const beforeInput = { tool: 'write', sessionID: 'session-2', callID: 'call-2' };
    const beforeOutput = { args: { filePath: 'a.txt', content: 'value' } };
    const guard = await runGeneratedHook(
      fixture,
      'tool.execute.before',
      beforeInput,
      beforeOutput,
    );
    assert.equal(guard.code, 0, guard.stderr);
    assert.match(guard.stderr, /guard warning/);
    assert.deepEqual(JSON.parse(await readFile(guardCapture, 'utf-8')), {
      hook_event_name: 'tool.execute.before',
      tool_name: 'write',
      tool_input: beforeOutput.args,
      session_id: 'session-2',
      call_id: 'call-2',
      input: beforeInput,
      output: beforeOutput,
    });

    await writeFile(fixture.hookScriptPath, `
      const fs = require('node:fs');
      const chunks = [];
      process.stdin.on('data', (chunk) => chunks.push(chunk));
      process.stdin.on('end', () => {
        fs.writeFileSync(${JSON.stringify(auditCapture)}, Buffer.concat(chunks));
        process.stderr.write('audit transport failed\\n');
        process.exitCode = 3;
      });
    `);
    const afterInput = { ...beforeInput, args: beforeOutput.args };
    const afterOutput = { title: 'Write', output: 'saved', metadata: { bytes: 5 } };
    const audit = await runGeneratedHook(
      fixture,
      'tool.execute.after',
      afterInput,
      afterOutput,
    );
    assert.equal(audit.code, 0, audit.stderr);
    assert.match(audit.stderr, /OpenCode audit runtime failed: audit transport failed/);
    assert.deepEqual(JSON.parse(await readFile(auditCapture, 'utf-8')).input, afterInput);

    await rm(fixture.guardScriptPath);
    const missing = await runGeneratedHook(
      fixture,
      'tool.execute.before',
      beforeInput,
      beforeOutput,
    );
    assert.equal(missing.code, 1);
    assert.match(missing.stderr, /Elydora guard failed/);
  } finally {
    await fixture.close();
  }
});

test('OpenCode status requires an exact plugin, runtime identity, and clean migration state', async (t) => {
  await t.test('tampered managed plugin', async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      await writeFile(fixture.pluginPath, `${await readFile(fixture.pluginPath, 'utf-8')}\n// tampered\n`);
      const status = await runPlugin(fixture, 'status', null);
      assert.equal(status.code, 1);
      assert.match(status.stderr, /does not match the managed template/);
    } finally {
      await fixture.close();
    }
  });

  for (const [label, target, contents, expected] of [
    ['guard runtime', 'guard', 'tampered\n', /"installed":false/],
    ['runtime config', 'config', '{ malformed', /parse Elydora runtime config/i],
    ['runtime identity', 'config', JSON.stringify({
      org_id: 'org-1',
      agent_id: 'different-agent',
      kid: 'kid-1',
      base_url: 'http://127.0.0.1:9',
      token: 'token-1',
      agent_name: 'opencode',
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
        assert.match(`${status.stdout}\n${status.stderr}`, expected);
      } finally {
        await fixture.close();
      }
    });
  }

  await t.test('stale owned mjs source', async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      await writeSource(fixture.legacyPluginPath, await legacySource(fixture));
      let status = await runPlugin(fixture, 'status', null);
      assert.equal(JSON.parse(status.stdout).hookConfigured, false);
      assert.equal((await fixture.install()).code, 0);
      await assertMissing(fixture.legacyPluginPath);
      status = await runPlugin(fixture, 'status', null);
      assert.equal(JSON.parse(status.stdout).installed, true);
    } finally {
      await fixture.close();
    }
  });
});

test('OpenCode rejects collisions, invalid inputs, and linked plugin paths before writes', async (t) => {
  await t.test('user plugin collision', async () => {
    const source = 'export const UserPlugin = async () => ({});\n';
    const fixture = await createFixture({ existingPlugin: source });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /owned by another integration/);
      assert.equal(await readFile(fixture.pluginPath, 'utf-8'), source);
      await assertMissing(fixture.guardScriptPath);
    } finally {
      await fixture.close();
    }
  });

  await t.test('non-canonical private key', async () => {
    const fixture = await createFixture();
    try {
      const result = await fixture.install({ privateKey: 'invalid' });
      assert.equal(result.code, 1);
      assert.match(result.stderr, /canonical 32-byte/);
      await assertMissing(fixture.pluginPath);
    } finally {
      await fixture.close();
    }
  });

  await t.test('relative XDG_CONFIG_HOME', async () => {
    const fixture = await createFixture();
    try {
      const source = `
        import { opencodePlugin } from ${JSON.stringify(
          pathToFileURL(path.resolve('dist/plugins/opencode.js')).href,
        )};
        await opencodePlugin.status();
      `;
      const result = await runNode(
        ['--input-type=module', '--eval', source],
        { HOME: fixture.homeDir, USERPROFILE: fixture.homeDir, XDG_CONFIG_HOME: 'relative' },
        fixture.projectDir,
      );
      assert.equal(result.code, 1);
      assert.match(result.stderr, /XDG_CONFIG_HOME must be an absolute path/);
    } finally {
      await fixture.close();
    }
  });

  await t.test('linked plugins directory', async () => {
    const fixture = await createFixture();
    const external = path.join(fixture.rootDir, 'external-plugins');
    try {
      await mkdir(external);
      await mkdir(path.dirname(fixture.pluginsDir), { recursive: true });
      await symlink(external, fixture.pluginsDir, process.platform === 'win32' ? 'junction' : 'dir');
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /plugins directory is not a physical directory/i);
      await assertMissing(fixture.guardScriptPath);
    } finally {
      await fixture.close();
    }
  });
});

test('OpenCode migrates exact legacy ownership and preserves unrelated mjs bytes', async (t) => {
  await t.test('install and uninstall exact ownership', async () => {
    const fixture = await createFixture();
    try {
      await writeSource(fixture.legacyPluginPath, await legacySource(fixture));
      assert.equal((await fixture.install()).code, 0);
      await assertMissing(fixture.legacyPluginPath);
      const uninstall = await runPlugin(fixture, 'uninstall', fixture.agentId);
      assert.equal(uninstall.code, 0, uninstall.stderr);
      await assertMissing(fixture.pluginPath);
    } finally {
      await fixture.close();
    }
  });

  await t.test('unrelated legacy file', async () => {
    const source = 'export const UserPlugin = async () => ({});\n';
    const fixture = await createFixture({ existingLegacy: source });
    try {
      assert.equal((await fixture.install()).code, 0);
      assert.equal(await readFile(fixture.legacyPluginPath, 'utf-8'), source);
      assert.equal((await runPlugin(fixture, 'uninstall', fixture.agentId)).code, 0);
      assert.equal(await readFile(fixture.legacyPluginPath, 'utf-8'), source);
    } finally {
      await fixture.close();
    }
  });

  await t.test('legacy-only uninstall', async () => {
    const fixture = await createFixture();
    try {
      await writeSource(fixture.legacyPluginPath, await legacySource(fixture));
      assert.equal((await runPlugin(fixture, 'uninstall', fixture.agentId)).code, 0);
      await assertMissing(fixture.legacyPluginPath);
    } finally {
      await fixture.close();
    }
  });
});

test('OpenCode CLI completes install, status, and uninstall end to end', async () => {
  const fixture = await createFixture();
  const privateKeyFile = path.join(fixture.projectDir, 'private-key.txt');
  const tokenFile = path.join(fixture.projectDir, 'token.txt');
  try {
    await writeFile(privateKeyFile, `${VALID_PRIVATE_KEY}\n`, { mode: 0o600 });
    await writeFile(tokenFile, 'token-1\n', { mode: 0o600 });
    const install = await runNode([
      cliPath, 'install', '--agent', 'opencode', '--org_id', 'org-1',
      '--agent_id', fixture.agentId, '--kid', 'kid-1',
      '--private_key_file', privateKeyFile, '--token_file', tokenFile,
      '--base_url', fixture.baseUrl,
    ], environment(fixture), fixture.projectDir);
    assert.equal(install.code, 0, install.stderr);
    const status = await runNode([cliPath, 'status'], environment(fixture), fixture.projectDir);
    assert.equal(status.code, 0, status.stderr);
    assert.match(status.stdout, /OpenCode \(opencode\) \[installed\]/);
    const uninstall = await runNode([
      cliPath, 'uninstall', '--agent', 'opencode', '--agent_id', fixture.agentId,
    ], environment(fixture), fixture.projectDir);
    assert.equal(uninstall.code, 0, uninstall.stderr);
    await assertMissing(fixture.pluginPath);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
