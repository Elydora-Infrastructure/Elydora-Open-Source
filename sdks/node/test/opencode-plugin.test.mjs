import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
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

test('OpenCode rejects tool execution when the Elydora guard blocks', async () => {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-opencode-'));
  try {
    const agentDir = path.join(homeDir, '.elydora', 'agent-1');
    await mkdir(agentDir, { recursive: true });
    const guardScriptPath = path.join(agentDir, 'guard.cjs');
    const hookScriptPath = path.join(agentDir, 'hook.cjs');
    await writeFile(
      guardScriptPath,
      "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n",
    );
    await writeFile(hookScriptPath, 'process.exit(0);\n');
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
    const hooks = await generatedPlugin.ElydoraAuditPlugin({ project: { name: 'test' } });
    await assert.rejects(
      hooks['tool.execute.before']({ tool: 'bash' }, { args: { command: 'echo test' } }),
      /Agent is frozen by Elydora/,
    );
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});
