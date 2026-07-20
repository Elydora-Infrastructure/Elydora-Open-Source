import assert from 'node:assert/strict';
import {
  lstat,
  mkdir,
  readFile,
  readdir,
  symlink,
  writeFile,
} from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  VALID_PRIVATE_KEY,
  createFixture,
  installationModuleUrl,
  installConfig,
  ioModuleUrl,
  runNode,
  writeJson,
} from '../test-support/cursor-test-helpers.mjs';

const managedFileNames = ['guard.js', 'config.json', 'private.key', 'hook.js'];

async function snapshotInstallation(fixture) {
  const entries = await Promise.all(managedFileNames.map(async (name) => [
    path.join(fixture.agentDir, name),
    await readFile(path.join(fixture.agentDir, name), 'utf-8'),
  ]));
  entries.push([fixture.configPath, await readFile(fixture.configPath, 'utf-8')]);
  return new Map(entries);
}

async function assertSnapshot(snapshot) {
  for (const [filePath, expected] of snapshot) {
    assert.equal(await readFile(filePath, 'utf-8'), expected, filePath);
  }
}

async function assertNoTransactionFiles(fixture) {
  const names = [
    ...await readdir(fixture.agentDir),
    ...await readdir(path.dirname(fixture.configPath)),
  ];
  assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false);
}

test('Cursor rolls back all five files when a transaction commit fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const source = `
      import { rename } from 'node:fs/promises';
      import { readDocument } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitCursorInstallation,
        prepareCursorInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
      const document = await readDocument();
      const prepared = await prepareCursorInstallation(config, { document, changed: false });
      let calls = 0;
      await commitCursorInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 3) throw new Error('injected rename failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        HOME: fixture.homeDir,
        USERPROFILE: fixture.homeDir,
        ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, {
          orgId: 'org-updated',
          token: 'token-updated',
        })),
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected rename failure/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Cursor detects concurrent hook config changes before committing runtime updates', async () => {
  const fixture = await createFixture({
    existingConfig: { version: 1, hooks: { sessionStart: [{ command: 'original' }] } },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"version":1,"hooks":{"sessionStart":[{"command":"concurrent"}]}}\n';
    const source = `
      import { writeFile } from 'node:fs/promises';
      import { readDocument } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitCursorInstallation,
        prepareCursorInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
      const document = await readDocument();
      const prepared = await prepareCursorInstallation(config, { document, changed: false });
      await writeFile(document.filePath, process.env.ELYDORA_CONCURRENT, 'utf-8');
      await commitCursorInstallation(prepared);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        HOME: fixture.homeDir,
        USERPROFILE: fixture.homeDir,
        ELYDORA_CONCURRENT: concurrent,
        ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, { orgId: 'org-updated' })),
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during Cursor installation/i);
    for (const [filePath, expected] of before) {
      if (filePath !== fixture.configPath) assert.equal(await readFile(filePath, 'utf-8'), expected);
    }
    assert.equal(await readFile(fixture.configPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Cursor rejects a stale hook document before staging an installation', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"version":1,"hooks":{"sessionStart":[{"command":"new"}]}}\n';
    const source = `
      import { writeFile } from 'node:fs/promises';
      import { readDocument } from ${JSON.stringify(ioModuleUrl)};
      import { prepareCursorInstallation } from ${JSON.stringify(installationModuleUrl)};
      const document = await readDocument();
      await writeFile(document.filePath, process.env.ELYDORA_CONCURRENT, 'utf-8');
      await prepareCursorInstallation(
        JSON.parse(process.env.ELYDORA_INSTALL_CONFIG),
        { document, changed: false },
      );
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        HOME: fixture.homeDir,
        USERPROFILE: fixture.homeDir,
        ELYDORA_CONCURRENT: concurrent,
        ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture)),
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed before installation/i);
    for (const [filePath, expected] of before) {
      if (filePath !== fixture.configPath) assert.equal(await readFile(filePath, 'utf-8'), expected);
    }
    assert.equal(await readFile(fixture.configPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Cursor rejects orphaned runtime artifacts before writing hook config', async () => {
  const fixture = await createFixture();
  try {
    await mkdir(fixture.agentDir, { recursive: true });
    await writeFile(fixture.guardScriptPath, 'orphaned guard\n');
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /identity cannot be verified/i);
    assert.equal(await readFile(fixture.guardScriptPath, 'utf-8'), 'orphaned guard\n');
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Cursor rejects a mismatched runtime identity before changing files', async () => {
  const fixture = await createFixture();
  try {
    await writeJson(path.join(fixture.agentDir, 'config.json'), {
      agent_name: 'cursor',
      agent_id: 'another-agent',
    });
    const before = await readFile(path.join(fixture.agentDir, 'config.json'), 'utf-8');
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /identity does not match/i);
    assert.equal(await readFile(path.join(fixture.agentDir, 'config.json'), 'utf-8'), before);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Cursor rejects symbolic-link config and runtime directories before file writes', async (t) => {
  for (const kind of ['cursor', 'runtime']) {
    const fixture = await createFixture();
    try {
      const target = path.join(fixture.rootDir, `${kind}-target`);
      await mkdir(target, { recursive: true });
      const link = kind === 'cursor'
        ? path.join(fixture.homeDir, '.cursor')
        : path.join(fixture.homeDir, '.elydora');
      await mkdir(fixture.homeDir, { recursive: true });
      try {
        await symlink(target, link, 'junction');
      } catch (error) {
        if (error?.code === 'EPERM') {
          t.skip(`symbolic links unavailable: ${error.message}`);
          return;
        }
        throw error;
      }
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /physical directory/i);
      assert.deepEqual(await readdir(target), []);
      const untouched = kind === 'cursor'
        ? path.join(fixture.homeDir, '.elydora')
        : path.join(fixture.homeDir, '.cursor', 'hooks.json');
      await assert.rejects(lstat(untouched), { code: 'ENOENT' });
    } finally {
      await fixture.close();
    }
  }
});

test('Cursor CLI preflight rejects malformed hooks before creating runtime files', async () => {
  const fixture = await createFixture({ existingConfig: '{ malformed' });
  try {
    const privateKeyPath = path.join(fixture.rootDir, 'private.key');
    await writeFile(privateKeyPath, VALID_PRIVATE_KEY, { mode: 0o600 });
    const result = await runNode([
      path.resolve('dist/cli.js'),
      'install',
      '--agent', 'cursor',
      '--org_id', 'org-1',
      '--agent_id', 'agent-1',
      '--private_key_file', privateKeyPath,
      '--kid', 'kid-1',
      '--base_url', fixture.baseUrl,
    ], {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
    }, fixture.projectDir);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /Cursor user hooks/i);
    await assert.rejects(lstat(path.join(fixture.homeDir, '.elydora')), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});
