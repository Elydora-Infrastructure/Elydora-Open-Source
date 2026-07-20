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
  contractModuleUrl,
  createFixture,
  environment,
  installConfig,
  installationModuleUrl,
  ioModuleUrl,
  runNode,
} from '../test-support/cline-test-helpers.mjs';

const managedFiles = [
  'guard.js',
  'config.json',
  'private.key',
  'hook.js',
];

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

async function snapshotInstallation(fixture) {
  const paths = [
    ...managedFiles.map((name) => path.join(fixture.agentDir, name)),
    fixture.guardWrapperPath,
    fixture.auditWrapperPath,
  ];
  return new Map(await Promise.all(paths.map(async (filePath) => [
    filePath,
    await readFile(filePath, 'utf-8'),
  ])));
}

async function assertSnapshot(snapshot) {
  for (const [filePath, source] of snapshot) {
    assert.equal(await readFile(filePath, 'utf-8'), source, filePath);
  }
}

async function assertNoTransactionFiles(fixture) {
  const names = [];
  for (const directory of [fixture.agentDir, fixture.hooksDir]) {
    try {
      names.push(...await readdir(directory));
    } catch (error) {
      if (error?.code !== 'ENOENT') throw error;
    }
  }
  assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false, names.join(', '));
}

function transactionEnvironment(fixture, overrides = {}) {
  return {
    ...environment(fixture),
    ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, overrides)),
  };
}

const prepareSource = `
  import { resolveHookFiles } from ${JSON.stringify(contractModuleUrl)};
  import { readHookFile } from ${JSON.stringify(ioModuleUrl)};
  import { prepareClineInstallation } from ${JSON.stringify(installationModuleUrl)};
  const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
  const paths = resolveHookFiles();
  const guard = await readHookFile(paths.guardPath);
  const audit = await readHookFile(paths.auditPath);
  const prepared = await prepareClineInstallation(config, guard, audit);
`;

test('Cline rolls back all six files when a late transaction commit fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const source = `${prepareSource}
      import { commitClineInstallation } from ${JSON.stringify(installationModuleUrl)};
      import { rename } from 'node:fs/promises';
      let calls = 0;
      await commitClineInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 5) throw new Error('injected rename failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      transactionEnvironment(fixture, { orgId: 'org-updated', token: 'token-updated' }),
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

test('Cline detects concurrent hook replacement before runtime commit', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '// concurrently replaced\n';
    const source = `${prepareSource}
      import { writeFile } from 'node:fs/promises';
      import { commitClineInstallation } from ${JSON.stringify(installationModuleUrl)};
      await writeFile(process.env.ELYDORA_CONCURRENT_PATH, process.env.ELYDORA_CONCURRENT);
      await commitClineInstallation(prepared);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...transactionEnvironment(fixture, { orgId: 'org-updated' }),
        ELYDORA_CONCURRENT_PATH: fixture.auditWrapperPath,
        ELYDORA_CONCURRENT: concurrent,
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during Cline installation/i);
    for (const [filePath, sourceBefore] of before) {
      if (filePath !== fixture.auditWrapperPath) {
        assert.equal(await readFile(filePath, 'utf-8'), sourceBefore, filePath);
      }
    }
    assert.equal(await readFile(fixture.auditWrapperPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Cline rejects stale hook snapshots before staging files', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '// stale snapshot replacement\n';
    const source = `
      import { resolveHookFiles } from ${JSON.stringify(contractModuleUrl)};
      import { readHookFile } from ${JSON.stringify(ioModuleUrl)};
      import { prepareClineInstallation } from ${JSON.stringify(installationModuleUrl)};
      import { writeFile } from 'node:fs/promises';
      const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
      const paths = resolveHookFiles();
      const guard = await readHookFile(paths.guardPath);
      const audit = await readHookFile(paths.auditPath);
      await writeFile(paths.auditPath, process.env.ELYDORA_CONCURRENT);
      await prepareClineInstallation(config, guard, audit);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      { ...transactionEnvironment(fixture), ELYDORA_CONCURRENT: concurrent },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed before installation/i);
    for (const [filePath, sourceBefore] of before) {
      if (filePath !== fixture.auditWrapperPath) {
        assert.equal(await readFile(filePath, 'utf-8'), sourceBefore, filePath);
      }
    }
    assert.equal(await readFile(fixture.auditWrapperPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Cline preserves orphaned runtime artifacts without a verifiable identity', async () => {
  const fixture = await createFixture();
  try {
    await mkdir(fixture.agentDir, { recursive: true });
    await writeFile(fixture.guardScriptPath, 'orphaned guard\n');
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /identity cannot be verified/i);
    assert.equal(await readFile(fixture.guardScriptPath, 'utf-8'), 'orphaned guard\n');
    await assertMissing(fixture.guardWrapperPath);
  } finally {
    await fixture.close();
  }
});

test('Cline rejects linked hook directories, runtime roots, and hook files', async (t) => {
  for (const kind of ['hooks', 'runtime', 'hook']) {
    await t.test(kind, async () => {
      const fixture = await createFixture();
      try {
        const target = path.join(fixture.rootDir, `${kind}-target`);
        await mkdir(target, { recursive: true });
        if (kind === 'hooks') {
          await mkdir(path.dirname(fixture.hooksDir), { recursive: true });
          await symlink(target, fixture.hooksDir, 'junction');
        } else if (kind === 'runtime') {
          await mkdir(fixture.homeDir, { recursive: true });
          await symlink(target, path.join(fixture.homeDir, '.elydora'), 'junction');
        } else {
          assert.equal((await fixture.install()).code, 0);
          const targetFile = path.join(target, 'PreToolUse.mjs');
          await writeFile(targetFile, 'external hook\n');
          await rm(fixture.guardWrapperPath);
          await symlink(targetFile, fixture.guardWrapperPath, 'file');
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

test('Cline uninstall restores both hooks when the second removal fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const source = `
      import { resolveHookFiles } from ${JSON.stringify(contractModuleUrl)};
      import { readHookFile } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitClineUninstall,
        prepareClineUninstall,
      } from ${JSON.stringify(installationModuleUrl)};
      import { rename } from 'node:fs/promises';
      const paths = resolveHookFiles();
      const files = await Promise.all([
        readHookFile(paths.guardPath),
        readHookFile(paths.auditPath),
      ]);
      const prepared = await prepareClineUninstall(files, 'agent-1');
      let calls = 0;
      await commitClineUninstall(prepared, async (from, to) => {
        calls += 1;
        if (calls === 2) throw new Error('injected uninstall failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      environment(fixture),
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected uninstall failure/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Cline installation leaves no transaction artifacts', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});
