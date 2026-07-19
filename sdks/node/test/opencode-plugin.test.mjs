import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { pathToFileURL } from 'node:url';
import test from 'node:test';

function runNode(args, env) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, args, {
      env: { ...process.env, ...env },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.once('error', reject);
    child.once('close', (code) => resolve({ code, stdout, stderr }));
  });
}

async function createFixture({ guardSource, hookSource }) {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-opencode-'));
  const agentDir = path.join(homeDir, '.elydora', 'agent-1');
  await mkdir(agentDir, { recursive: true });
  const guardScriptPath = path.join(agentDir, 'guard.cjs');
  const hookScriptPath = path.join(agentDir, 'hook.cjs');
  if (guardSource !== undefined) await writeFile(guardScriptPath, guardSource);
  await writeFile(hookScriptPath, hookSource ?? 'process.exit(0);\n');
  const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/opencode.js')).href;
  const installScript = `
    import { opencodePlugin } from ${JSON.stringify(pluginModuleUrl)};
    await opencodePlugin.install(JSON.parse(process.env.ELYDORA_TEST_CONFIG));
  `;
  const installResult = await runNode(
    ['--input-type=module', '--eval', installScript],
    {
      HOME: homeDir,
      USERPROFILE: homeDir,
      ELYDORA_TEST_CONFIG: JSON.stringify({ guardScriptPath, hookScriptPath }),
    },
  );
  assert.equal(installResult.code, 0, installResult.stderr);
  const generatedPath = path.join(
    homeDir,
    '.config',
    'opencode',
    'plugins',
    'elydora-audit.mjs',
  );
  const generatedPlugin = await import(`${pathToFileURL(generatedPath).href}?test=${Date.now()}`);
  const hooks = await generatedPlugin.ElydoraAuditPlugin({ project: { name: 'project-fallback' } });
  return {
    guardScriptPath,
    homeDir,
    hooks,
    async close() {
      await rm(homeDir, { recursive: true, force: true });
    },
  };
}

async function readPluginStatus(homeDir) {
  const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/opencode.js')).href;
  const statusScript = `
    import { opencodePlugin } from ${JSON.stringify(pluginModuleUrl)};
    console.log(JSON.stringify(await opencodePlugin.status()));
  `;
  const result = await runNode(
    ['--input-type=module', '--eval', statusScript],
    { HOME: homeDir, USERPROFILE: homeDir },
  );
  assert.equal(result.code, 0, result.stderr);
  return JSON.parse(result.stdout);
}

test('OpenCode rejects tool execution when the Elydora guard blocks', async () => {
  const fixture = await createFixture({
    guardSource: "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n",
  });
  try {
    await assert.rejects(
      fixture.hooks['tool.execute.before'](
        { tool: 'bash', sessionID: 'session-1', callID: 'call-1' },
        { args: { command: 'echo test' } },
      ),
      /Agent is frozen by Elydora/,
    );
  } finally {
    await fixture.close();
  }
});

test('OpenCode blocks when the guard process cannot start', async () => {
  const fixture = await createFixture({});
  try {
    await assert.rejects(
      fixture.hooks['tool.execute.before'](
        { tool: 'bash', sessionID: 'session-1', callID: 'call-1' },
        { args: { command: 'echo test' } },
      ),
      /Elydora guard failed/,
    );
  } finally {
    await fixture.close();
  }
});

test('OpenCode forwards the current tool event contract to the audit hook', async () => {
  const capturePath = path.join(os.tmpdir(), `elydora-event-${process.pid}-${Date.now()}.json`);
  const hookSource = `
    const fs = require('node:fs');
    const chunks = [];
    process.stdin.on('data', (chunk) => chunks.push(chunk));
    process.stdin.on('end', () => fs.writeFileSync(${JSON.stringify(capturePath)}, Buffer.concat(chunks)));
  `;
  const fixture = await createFixture({
    guardSource: 'process.exit(0);\n',
    hookSource,
  });
  try {
    await fixture.hooks['tool.execute.after'](
      {
        tool: 'bash',
        sessionID: 'session-1',
        callID: 'call-1',
        args: { command: 'echo test' },
      },
      { title: 'Shell', output: 'test' },
    );
    let payload;
    const deadline = Date.now() + 3000;
    while (Date.now() < deadline) {
      try {
        payload = await readFile(capturePath, 'utf-8');
        break;
      } catch (error) {
        if (error.code !== 'ENOENT') throw error;
        await delay(20);
      }
    }
    assert(payload, 'audit hook did not receive the tool event');
    assert.deepEqual(JSON.parse(payload), {
      tool_name: 'bash',
      tool_input: { command: 'echo test' },
      session_id: 'session-1',
    });
  } finally {
    await fixture.close();
    await rm(capturePath, { force: true });
  }
});

test('OpenCode status requires both generated runtime scripts', async () => {
  const fixture = await createFixture({ guardSource: 'process.exit(0);\n' });
  try {
    assert.equal((await readPluginStatus(fixture.homeDir)).installed, true);
    await rm(fixture.guardScriptPath);
    assert.equal((await readPluginStatus(fixture.homeDir)).installed, false);
  } finally {
    await fixture.close();
  }
});
