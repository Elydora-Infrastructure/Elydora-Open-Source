import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

const pluginModuleUrl = pathToFileURL(path.resolve('dist/plugins/cline.js')).href;
const registryModuleUrl = pathToFileURL(path.resolve('dist/plugins/registry.js')).href;
const hookTemplateModuleUrl = pathToFileURL(path.resolve('dist/plugins/hook-template.js')).href;
const cliPath = path.resolve('dist/cli.js');

function runNode(args, env, input = '', cwd) {
  return new Promise((resolve, reject) => {
    const childEnvironment = { ...process.env, ...env };
    for (const [name, value] of Object.entries(env)) {
      if (value === undefined) delete childEnvironment[name];
    }
    const child = spawn(process.execPath, args, {
      cwd,
      env: childEnvironment,
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.once('error', reject);
    child.once('close', (code) => resolve({ code, stdout, stderr }));
    child.stdin.end(input);
  });
}

async function runPlugin(fixture, method, argument, clineDir = fixture.clineDir) {
  const source = `
    import { clinePlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await clinePlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      CLINE_DIR: clineDir === null ? undefined : clineDir,
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    },
    '',
    fixture.workspaceDir,
  );
}

async function writeOptional(filePath, contents) {
  if (contents === undefined) return;
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, contents);
}

async function createFixture({
  agentId = 'agent-1',
  autoInstall = true,
  createRuntimes = true,
  existingAudit,
  existingGuard,
  guardSource,
  hookSource,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-cline-'));
  const homeDir = path.join(rootDir, "home with spaces and 'quote");
  const workspaceDir = path.join(rootDir, 'workspace');
  const clineDir = path.join(rootDir, 'custom-cline-home');
  const hooksDir = path.join(clineDir, 'hooks');
  const guardWrapperPath = path.join(hooksDir, 'PreToolUse.mjs');
  const auditWrapperPath = path.join(hooksDir, 'PostToolUse.mjs');
  const agentDir = path.join(homeDir, '.elydora', agentId);
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  await mkdir(workspaceDir, { recursive: true });
  if (createRuntimes) {
    await mkdir(agentDir, { recursive: true });
    await writeFile(
      guardScriptPath,
      guardSource ?? "process.stdin.destroy(); process.stderr.write('Agent is frozen by Elydora.\\n'); process.exit(2);\n",
    );
    await writeFile(hookScriptPath, hookSource ?? 'process.stdin.resume();\n');
    await writeFile(path.join(agentDir, 'config.json'), JSON.stringify({
      agent_id: agentId,
      agent_name: 'cline',
    }));
  }
  await writeOptional(guardWrapperPath, existingGuard);
  await writeOptional(auditWrapperPath, existingAudit);

  const fixture = {
    agentDir,
    agentId,
    auditWrapperPath,
    clineDir,
    guardScriptPath,
    guardWrapperPath,
    homeDir,
    hookScriptPath,
    hooksDir,
    rootDir,
    workspaceDir,
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
  fixture.config = {
    agentName: 'cline',
    agentId,
    baseUrl: 'https://api.elydora.com',
    guardScriptPath,
    hookScriptPath,
  };
  if (autoInstall) fixture.installResult = await runPlugin(fixture, 'install', fixture.config);
  return fixture;
}

function parseControl(stdout) {
  const line = stdout.trim().split(/\r?\n/).at(-1);
  assert.match(line, /^HOOK_CONTROL\t/);
  return JSON.parse(line.slice('HOOK_CONTROL\t'.length));
}

test('Cline is registered in the SDK and CLI', async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get('cline'), {
    name: 'Cline',
    configDir: '~/.cline/hooks',
    configFile: 'PreToolUse.mjs',
  });
  const fixture = await createFixture({ autoInstall: false });
  try {
    const result = await runNode(['--no-warnings', cliPath, 'status'], {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      CLINE_DIR: fixture.clineDir,
    }, '', fixture.workspaceDir);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Cline \(cline\)/);
  } finally {
    await fixture.close();
  }
});

test('Cline install writes only the native global hooks and is idempotent', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const originalGuard = await readFile(fixture.guardWrapperPath, 'utf-8');
    const originalAudit = await readFile(fixture.auditWrapperPath, 'utf-8');
    assert.match(originalGuard, /^#!\/usr\/bin\/env node\n\/\/ @elydora-cline-hook /);
    assert.match(originalAudit, /^#!\/usr\/bin\/env node\n\/\/ @elydora-cline-hook /);

    const second = await runPlugin(fixture, 'install', fixture.config);
    assert.equal(second.code, 0, second.stderr);
    assert.equal(await readFile(fixture.guardWrapperPath, 'utf-8'), originalGuard);
    assert.equal(await readFile(fixture.auditWrapperPath, 'utf-8'), originalAudit);
    assert.equal((await readdir(fixture.hooksDir)).some((name) => name.endsWith('.tmp')), false);

    await assert.rejects(
      readFile(path.join(fixture.homeDir, 'Documents', 'Cline', 'Hooks', 'PreToolUse.mjs')),
      { code: 'ENOENT' },
    );
    await assert.rejects(
      readFile(path.join(fixture.workspaceDir, '.cline', 'hooks', 'PreToolUse.mjs')),
      { code: 'ENOENT' },
    );
    await assert.rejects(
      readFile(path.join(fixture.workspaceDir, '.clinerules', 'hooks', 'PreToolUse.mjs')),
      { code: 'ENOENT' },
    );
  } finally {
    await fixture.close();
  }
});

test('Cline install uses the official default when CLINE_DIR is absent', async () => {
  const fixture = await createFixture({ autoInstall: false });
  const defaultHooks = path.join(fixture.homeDir, '.cline', 'hooks');
  try {
    const result = await runPlugin(fixture, 'install', fixture.config, null);
    assert.equal(result.code, 0, result.stderr);
    await readFile(path.join(defaultHooks, 'PreToolUse.mjs'));
    await readFile(path.join(defaultHooks, 'PostToolUse.mjs'));
    await assert.rejects(readFile(fixture.guardWrapperPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Cline wrappers translate freezes and forward official payloads byte-for-byte', async () => {
  const fixtureRoot = await mkdtemp(path.join(os.tmpdir(), 'elydora-cline-capture-'));
  const capturePath = path.join(fixtureRoot, 'event.json');
  const hookSource = `
    const fs = require('node:fs');
    const chunks = [];
    process.stdin.on('data', (chunk) => chunks.push(chunk));
    process.stdin.on('end', () => fs.writeFileSync(${JSON.stringify(capturePath)}, JSON.stringify({
      cwd: process.cwd(),
      input: Buffer.concat(chunks).toString('utf-8'),
    })));
  `;
  const fixture = await createFixture({ hookSource });
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    const prePayload = JSON.stringify({
      clineVersion: '3.0.46',
      hookName: 'tool_call',
      taskId: 'task-1',
      tool_call: { id: 'call-1', name: 'read_file', input: { path: 'README.md' } },
    });
    const guard = await runNode(
      [fixture.guardWrapperPath],
      { HOME: fixture.homeDir, USERPROFILE: fixture.homeDir },
      prePayload,
      fixture.workspaceDir,
    );
    assert.equal(guard.code, 0, guard.stderr);
    assert.match(guard.stderr, /Agent is frozen by Elydora/);
    assert.deepEqual(parseControl(guard.stdout), {
      cancel: true,
      errorMessage: 'Agent is frozen by Elydora.',
    });

    const postPayload = JSON.stringify({
      clineVersion: '3.0.46',
      hookName: 'tool_result',
      taskId: 'task-1',
      tool_result: {
        id: 'call-1',
        name: 'read_file',
        input: { path: 'README.md' },
        output: 'ok',
        durationMs: 5,
      },
    });
    const audit = await runNode(
      [fixture.auditWrapperPath],
      { HOME: fixture.homeDir, USERPROFILE: fixture.homeDir },
      postPayload,
      fixture.workspaceDir,
    );
    assert.equal(audit.code, 0, audit.stderr);
    assert.equal(audit.stdout, '');
    assert.deepEqual(JSON.parse(await readFile(capturePath, 'utf-8')), {
      cwd: fixture.workspaceDir,
      input: postPayload,
    });
  } finally {
    await fixture.close();
    await rm(fixtureRoot, { recursive: true, force: true });
  }
});

test('Cline audit runtime maps official nested fields into the submitted operation', async () => {
  let resolveOperation;
  const operationReceived = new Promise((resolve) => { resolveOperation = resolve; });
  const server = createServer((request, response) => {
    const chunks = [];
    request.on('data', (chunk) => chunks.push(chunk));
    request.on('end', () => {
      resolveOperation(JSON.parse(Buffer.concat(chunks).toString('utf-8')));
      response.writeHead(201, { 'Content-Type': 'application/json' });
      response.end('{}');
    });
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  assert.equal(typeof address, 'object');
  const fixture = await createFixture({ autoInstall: false });
  try {
    const { generateHookScript } = await import(hookTemplateModuleUrl);
    await writeFile(fixture.hookScriptPath, generateHookScript('cline', fixture.agentId));
    await writeFile(path.join(fixture.agentDir, 'config.json'), JSON.stringify({
      org_id: 'org-1',
      agent_id: fixture.agentId,
      kid: 'key-1',
      base_url: `http://127.0.0.1:${address.port}`,
      agent_name: 'cline',
    }));
    await writeFile(path.join(fixture.agentDir, 'private.key'), Buffer.alloc(32, 1).toString('base64url'));
    const install = await runPlugin(fixture, 'install', fixture.config);
    assert.equal(install.code, 0, install.stderr);
    const payload = JSON.stringify({
      hookName: 'tool_result',
      taskId: 'task-1',
      tool_result: {
        name: 'read_file',
        input: { path: 'README.md' },
        output: 'ok',
      },
    });
    const result = await runNode([fixture.auditWrapperPath], {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
    }, payload, fixture.workspaceDir);
    assert.equal(result.code, 0, result.stderr);
    let timeout;
    const operation = await Promise.race([
      operationReceived,
      new Promise((_, reject) => {
        timeout = setTimeout(() => reject(new Error('Timed out waiting for audit operation')), 3_000);
      }),
    ]).finally(() => clearTimeout(timeout));
    assert.deepEqual(operation.payload, {
      tool_name: 'read_file',
      tool_input: { path: 'README.md' },
      session_id: 'task-1',
    });
    assert.deepEqual(operation.action, { tool: 'read_file' });
    assert.deepEqual(operation.subject, { session_id: 'task-1' });
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
    await fixture.close();
  }
});

test('Cline wrappers keep pass decisions quiet and surface runtime failures', async () => {
  const passing = await createFixture({ guardSource: 'process.stdin.resume();\n' });
  try {
    assert.equal(passing.installResult.code, 0, passing.installResult.stderr);
    const result = await runNode([passing.guardWrapperPath], {}, '{}', passing.workspaceDir);
    assert.equal(result.code, 0, result.stderr);
    assert.equal(result.stdout, '');
  } finally {
    await passing.close();
  }

  const failing = await createFixture({ hookSource: "process.stderr.write('audit failed\\n'); process.exit(7);\n" });
  try {
    assert.equal(failing.installResult.code, 0, failing.installResult.stderr);
    const result = await runNode([failing.auditWrapperPath], {}, '{}', failing.workspaceDir);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /audit failed/);
    assert.match(result.stderr, /exited with code 7/);
  } finally {
    await failing.close();
  }
});

test('Cline status requires an intact pair and both Elydora runtimes', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    let result = await runPlugin(fixture, 'status', null);
    assert.equal(result.code, 0, result.stderr);
    assert.deepEqual(JSON.parse(result.stdout), {
      installed: true,
      agentName: 'cline',
      displayName: 'Cline',
      hookConfigured: true,
      hookScriptExists: true,
      configPath: fixture.hooksDir,
    });

    await rm(fixture.guardScriptPath);
    result = await runPlugin(fixture, 'status', null);
    assert.equal(JSON.parse(result.stdout).installed, false);
    await writeFile(fixture.guardScriptPath, 'process.exit(0);\n');
    await rm(fixture.auditWrapperPath);
    result = await runPlugin(fixture, 'status', null);
    const partial = JSON.parse(result.stdout);
    assert.equal(partial.installed, false);
    assert.equal(partial.hookConfigured, false);
  } finally {
    await fixture.close();
  }
});

test('Cline status surfaces corrupt owned hooks and runtime metadata', async () => {
  const fixture = await createFixture();
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    await writeFile(fixture.guardWrapperPath, `${await readFile(fixture.guardWrapperPath, 'utf-8')}\n// tampered\n`);
    let result = await runPlugin(fixture, 'status', null);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /managed template/i);

    const reinstall = await runPlugin(fixture, 'install', fixture.config);
    assert.equal(reinstall.code, 0, reinstall.stderr);
    await writeFile(path.join(fixture.agentDir, 'config.json'), '{ malformed');
    result = await runPlugin(fixture, 'status', null);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /parse Elydora runtime config/i);
  } finally {
    await fixture.close();
  }
});

test('Cline install rejects user filename collisions before every write', async () => {
  for (const collision of ['guard', 'audit']) {
    const existingGuard = collision === 'guard' ? '// user PreToolUse hook\n' : undefined;
    const existingAudit = collision === 'audit' ? '// user PostToolUse hook\n' : undefined;
    const fixture = await createFixture({
      autoInstall: false,
      existingAudit,
      existingGuard,
    });
    try {
      const result = await runPlugin(fixture, 'install', fixture.config);
      assert.equal(result.code, 1);
      assert.match(result.stderr, /already exists and is owned by another integration/i);
      if (existingGuard) assert.equal(await readFile(fixture.guardWrapperPath, 'utf-8'), existingGuard);
      else await assert.rejects(readFile(fixture.guardWrapperPath), { code: 'ENOENT' });
      if (existingAudit) assert.equal(await readFile(fixture.auditWrapperPath, 'utf-8'), existingAudit);
      else await assert.rejects(readFile(fixture.auditWrapperPath), { code: 'ENOENT' });
    } finally {
      await fixture.close();
    }
  }
});

test('Cline install preserves corrupt owned metadata for recovery', async () => {
  const corrupt = '#!/usr/bin/env node\n// @elydora-cline-hook invalid\n';
  const fixture = await createFixture({ autoInstall: false, existingGuard: corrupt });
  try {
    const result = await runPlugin(fixture, 'install', fixture.config);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /parse Elydora Cline hook metadata/i);
    assert.equal(await readFile(fixture.guardWrapperPath, 'utf-8'), corrupt);
    await assert.rejects(readFile(fixture.auditWrapperPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Cline install rejects missing runtimes before creating hooks', async () => {
  const fixture = await createFixture({ autoInstall: false, createRuntimes: false });
  try {
    const result = await runPlugin(fixture, 'install', fixture.config);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /runtime is missing/i);
    await assert.rejects(readFile(fixture.guardWrapperPath), { code: 'ENOENT' });
    await assert.rejects(readFile(fixture.auditWrapperPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Cline uninstall removes exact ownership and preserves other hooks', async () => {
  const fixture = await createFixture();
  const userHook = path.join(fixture.hooksDir, 'PreToolUse.py');
  try {
    assert.equal(fixture.installResult.code, 0, fixture.installResult.stderr);
    await writeFile(userHook, '# user hook\n');
    let result = await runPlugin(fixture, 'uninstall', 'agent-10');
    assert.equal(result.code, 0, result.stderr);
    await readFile(fixture.guardWrapperPath);
    await readFile(fixture.auditWrapperPath);

    result = await runPlugin(fixture, 'uninstall', 'agent-1');
    assert.equal(result.code, 0, result.stderr);
    await assert.rejects(readFile(fixture.guardWrapperPath), { code: 'ENOENT' });
    await assert.rejects(readFile(fixture.auditWrapperPath), { code: 'ENOENT' });
    assert.equal(await readFile(userHook, 'utf-8'), '# user hook\n');
  } finally {
    await fixture.close();
  }
});
