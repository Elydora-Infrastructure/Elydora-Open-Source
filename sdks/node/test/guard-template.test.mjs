import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { generateGuardScript } from '../dist/plugins/hook-template.js';

const AGENT_ID = 'agent-1';

async function createFixture(status) {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-guard-'));
  const agentDir = path.join(homeDir, '.elydora', AGENT_ID);
  await mkdir(agentDir, { recursive: true });
  const server = http.createServer((_request, response) => {
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ agent: { status } }));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert(address && typeof address === 'object');
  await writeFile(
    path.join(agentDir, 'config.json'),
    JSON.stringify({ agent_id: AGENT_ID, base_url: `http://127.0.0.1:${address.port}` }),
  );
  const scriptPath = path.join(homeDir, 'guard.cjs');
  await writeFile(scriptPath, generateGuardScript('claudecode', AGENT_ID));
  return {
    homeDir,
    scriptPath,
    async close() {
      await new Promise((resolve, reject) => {
        server.close((error) => (error ? reject(error) : resolve()));
      });
      await rm(homeDir, { recursive: true, force: true });
    },
  };
}

function runGuard(scriptPath, homeDir) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [scriptPath], {
      env: { ...process.env, HOME: homeDir, USERPROFILE: homeDir },
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

test('frozen agents use the blocking exit code required by hook CLIs', async () => {
  const fixture = await createFixture('frozen');
  try {
    const result = await runGuard(fixture.scriptPath, fixture.homeDir);
    assert.equal(result.code, 2);
    assert.match(result.stderr, /Tool execution blocked/);
    assert.equal(result.stdout, '');
  } finally {
    await fixture.close();
  }
});

test('active agents allow tool execution', async () => {
  const fixture = await createFixture('active');
  try {
    const result = await runGuard(fixture.scriptPath, fixture.homeDir);
    assert.equal(result.code, 0);
    assert.equal(result.stderr, '');
    assert.equal(result.stdout, '');
  } finally {
    await fixture.close();
  }
});

test('cached frozen status keeps the blocking exit code', async () => {
  const fixture = await createFixture('active');
  try {
    await writeFile(
      path.join(fixture.homeDir, '.elydora', AGENT_ID, 'status-cache.json'),
      JSON.stringify({ status: 'frozen', cached_at: Date.now() }),
    );
    const result = await runGuard(fixture.scriptPath, fixture.homeDir);
    assert.equal(result.code, 2);
    assert.match(result.stderr, /Tool execution blocked/);
    assert.equal(result.stdout, '');
  } finally {
    await fixture.close();
  }
});
