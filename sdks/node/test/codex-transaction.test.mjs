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
  createFixture,
  installationModuleUrl,
  installConfig,
  ioModuleUrl,
  runNode,
  writeJson,
} from '../test-support/codex-test-helpers.mjs';

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

async function preservedRollback(fixture, basename) {
  const entries = await readdir(fixture.agentDir);
  const matches = entries.filter(
    (name) => name.startsWith(`.${basename}.`) && name.endsWith('.rollback'),
  );
  assert.equal(matches.length, 1, entries.join(', '));
  return path.join(fixture.agentDir, matches[0]);
}

test('Codex rolls back all five files when a transaction commit fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const source = `
      import { rename } from 'node:fs/promises';
      import { readDocument } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitCodexInstallation,
        prepareCodexInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
      const document = await readDocument();
      const prepared = await prepareCodexInstallation(config, { document, changed: false });
      let calls = 0;
      await commitCodexInstallation(prepared, async (from, to) => {
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

test('Codex detects concurrent hook changes before committing runtime updates', async () => {
  const fixture = await createFixture({
    existingConfig: {
      hooks: { SessionStart: [{ matcher: 'startup', hooks: [{ command: 'original' }] }] },
    },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"hooks":{"SessionStart":[{"matcher":"resume","hooks":[{"command":"concurrent"}]}]}}\n';
    const source = `
      import { writeFile } from 'node:fs/promises';
      import { readDocument } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitCodexInstallation,
        prepareCodexInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
      const document = await readDocument();
      const prepared = await prepareCodexInstallation(config, { document, changed: false });
      await writeFile(document.filePath, process.env.ELYDORA_CONCURRENT, 'utf-8');
      await commitCodexInstallation(prepared);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        HOME: fixture.homeDir,
        USERPROFILE: fixture.homeDir,
        ELYDORA_CONCURRENT: concurrent,
        ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, { orgId: 'updated' })),
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during Codex installation/i);
    for (const [filePath, expected] of before) {
      if (filePath !== fixture.configPath) assert.equal(await readFile(filePath, 'utf-8'), expected);
    }
    assert.equal(await readFile(fixture.configPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('managed installation preserves rollback data after a committed file changes', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const original = await readFile(fixture.guardScriptPath, 'utf-8');
    const source = `
      import { rename, rm, writeFile } from 'node:fs/promises';
      import { readDocument } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitCodexInstallation,
        prepareCodexInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const document = await readDocument();
      const prepared = await prepareCodexInstallation(
        JSON.parse(process.env.ELYDORA_INSTALL_CONFIG),
        { document, changed: false },
      );
      let calls = 0;
      await commitCodexInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 1) {
          await rename(from, to);
          await rm(to);
          await writeFile(to, 'external change\\n');
          return;
        }
        if (calls === 2) throw new Error('injected later commit failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        HOME: fixture.homeDir,
        USERPROFILE: fixture.homeDir,
        ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, { orgId: 'updated' })),
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /original content preserved at/i);
    assert.equal(await readFile(fixture.guardScriptPath, 'utf-8'), 'external change\n');
    assert.equal(await readFile(
      await preservedRollback(fixture, 'guard.js'),
      'utf-8',
    ), original);
  } finally {
    await fixture.close();
  }
});

test('managed installation preserves rollback data after restore rename failure', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const original = await readFile(fixture.guardScriptPath, 'utf-8');
    const source = `
      import { rename } from 'node:fs/promises';
      import { readDocument } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitCodexInstallation,
        prepareCodexInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const document = await readDocument();
      const prepared = await prepareCodexInstallation(
        JSON.parse(process.env.ELYDORA_INSTALL_CONFIG),
        { document, changed: false },
      );
      let calls = 0;
      await commitCodexInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 2) throw new Error('injected later commit failure');
        if (from.endsWith('.rollback')) throw new Error('injected rollback failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        HOME: fixture.homeDir,
        USERPROFILE: fixture.homeDir,
        ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, { orgId: 'updated' })),
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /original content preserved at/i);
    assert.equal(await readFile(
      await preservedRollback(fixture, 'guard.js'),
      'utf-8',
    ), original);
  } finally {
    await fixture.close();
  }
});

test('Codex rejects a stale hook document before staging files', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"hooks":{"SessionStart":[]}}\n';
    const source = `
      import { writeFile } from 'node:fs/promises';
      import { readDocument } from ${JSON.stringify(ioModuleUrl)};
      import { prepareCodexInstallation } from ${JSON.stringify(installationModuleUrl)};
      const document = await readDocument();
      await writeFile(document.filePath, process.env.ELYDORA_CONCURRENT, 'utf-8');
      await prepareCodexInstallation(
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

test('Codex rejects orphaned runtime artifacts before writing hooks', async () => {
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

test('Codex rejects mismatched runtime identity before changing files', async () => {
  const fixture = await createFixture();
  try {
    await writeJson(path.join(fixture.agentDir, 'config.json'), {
      agent_name: 'codex',
      agent_id: 'another-agent',
    });
    const configPath = path.join(fixture.agentDir, 'config.json');
    const before = await readFile(configPath, 'utf-8');
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /identity does not match/i);
    assert.equal(await readFile(configPath, 'utf-8'), before);
    await assert.rejects(readFile(fixture.configPath), { code: 'ENOENT' });
  } finally {
    await fixture.close();
  }
});

test('Codex rejects linked hooks and runtime directories before writes', async (t) => {
  for (const kind of ['codex', 'runtime']) {
    const fixture = await createFixture();
    try {
      const target = path.join(fixture.rootDir, `${kind}-target`);
      await mkdir(target, { recursive: true });
      const link = kind === 'codex'
        ? path.join(fixture.homeDir, '.codex')
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
      const untouched = kind === 'codex'
        ? path.join(fixture.homeDir, '.elydora')
        : fixture.configPath;
      await assert.rejects(lstat(untouched), { code: 'ENOENT' });
    } finally {
      await fixture.close();
    }
  }
});
