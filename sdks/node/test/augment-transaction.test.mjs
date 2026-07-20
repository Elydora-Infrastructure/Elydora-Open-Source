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
  createFixture,
  installConfig,
  installationModuleUrl,
  ioModuleUrl,
  managedInstallationModuleUrl,
  runNode,
  writeJson,
} from '../test-support/augment-test-helpers.mjs';

const managedFileNames = [
  'guard.js',
  'config.json',
  'private.key',
  'hook.js',
  `augment-guard${process.platform === 'win32' ? '.cmd' : '.sh'}`,
  `augment-hook${process.platform === 'win32' ? '.cmd' : '.sh'}`,
];

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

async function snapshotInstallation(fixture) {
  const entries = await Promise.all(managedFileNames.map(async (name) => [
    path.join(fixture.agentDir, name),
    await readFile(path.join(fixture.agentDir, name), 'utf-8'),
  ]));
  entries.push([fixture.settingsPath, await readFile(fixture.settingsPath, 'utf-8')]);
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
    ...await readdir(path.dirname(fixture.settingsPath)),
  ];
  assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false, names.join(', '));
}

function testEnvironment(fixture, overrides = {}) {
  return {
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, overrides)),
  };
}

test('Auggie rolls back all seven files when a transaction commit fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const source = `
      import { rename } from 'node:fs/promises';
      import { readConfig } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitAugmentInstallation,
        prepareAugmentInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const document = await readConfig();
      const prepared = await prepareAugmentInstallation(
        JSON.parse(process.env.ELYDORA_INSTALL_CONFIG),
        { document, changed: false },
      );
      let calls = 0;
      await commitAugmentInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 5) throw new Error('injected rename failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      testEnvironment(fixture, { orgId: 'org-updated', token: 'token-updated' }),
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

test('Auggie detects concurrent settings changes before runtime commit', async () => {
  const fixture = await createFixture({ settings: { telemetryEnabled: true } });
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"telemetryEnabled":false,"hooks":{"Notification":[]}}\n';
    const source = `
      import { writeFile } from 'node:fs/promises';
      import { readConfig } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitAugmentInstallation,
        prepareAugmentInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const document = await readConfig();
      const prepared = await prepareAugmentInstallation(
        JSON.parse(process.env.ELYDORA_INSTALL_CONFIG),
        { document, changed: false },
      );
      await writeFile(document.configPath, process.env.ELYDORA_CONCURRENT, 'utf-8');
      await commitAugmentInstallation(prepared);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...testEnvironment(fixture, { orgId: 'updated' }),
        ELYDORA_CONCURRENT: concurrent,
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during Augment Code CLI installation/i);
    for (const [filePath, expected] of before) {
      if (filePath !== fixture.settingsPath) {
        assert.equal(await readFile(filePath, 'utf-8'), expected);
      }
    }
    assert.equal(await readFile(fixture.settingsPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Auggie rejects a stale settings document before staging files', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"hooks":{"Notification":[]}}\n';
    const source = `
      import { writeFile } from 'node:fs/promises';
      import { readConfig } from ${JSON.stringify(ioModuleUrl)};
      import { prepareAugmentInstallation } from ${JSON.stringify(installationModuleUrl)};
      const document = await readConfig();
      await writeFile(document.configPath, process.env.ELYDORA_CONCURRENT, 'utf-8');
      await prepareAugmentInstallation(
        JSON.parse(process.env.ELYDORA_INSTALL_CONFIG),
        { document, changed: false },
      );
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      { ...testEnvironment(fixture), ELYDORA_CONCURRENT: concurrent },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed before installation/i);
    for (const [filePath, expected] of before) {
      if (filePath !== fixture.settingsPath) {
        assert.equal(await readFile(filePath, 'utf-8'), expected);
      }
    }
    assert.equal(await readFile(fixture.settingsPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Auggie rejects orphaned and mismatched runtime identities before writes', async (t) => {
  await t.test('orphaned runtime', async () => {
    const fixture = await createFixture();
    try {
      await mkdir(fixture.agentDir, { recursive: true });
      await writeFile(fixture.guardScriptPath, 'orphaned guard\n');
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /identity cannot be verified/i);
      assert.equal(await readFile(fixture.guardScriptPath, 'utf-8'), 'orphaned guard\n');
      await assertMissing(fixture.settingsPath);
    } finally {
      await fixture.close();
    }
  });

  await t.test('mismatched identity', async () => {
    const fixture = await createFixture();
    try {
      await writeJson(path.join(fixture.agentDir, 'config.json'), {
        agent_name: 'augment',
        agent_id: 'another-agent',
      });
      const configPath = path.join(fixture.agentDir, 'config.json');
      const before = await readFile(configPath, 'utf-8');
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /identity does not match/i);
      assert.equal(await readFile(configPath, 'utf-8'), before);
      await assertMissing(fixture.settingsPath);
    } finally {
      await fixture.close();
    }
  });
});

test('Auggie rejects linked settings, runtime directories, and wrappers', async (t) => {
  for (const kind of ['settings', 'runtime', 'wrapper']) {
    await t.test(kind, async () => {
      const fixture = await createFixture();
      try {
        const target = path.join(fixture.rootDir, `${kind}-target`);
        await mkdir(target, { recursive: true });
        if (kind === 'settings') {
          await mkdir(path.dirname(fixture.settingsPath), { recursive: true });
          const targetFile = path.join(target, 'settings.json');
          await writeFile(targetFile, '{}\n');
          await symlink(targetFile, fixture.settingsPath, 'file');
        } else if (kind === 'runtime') {
          await mkdir(fixture.homeDir, { recursive: true });
          await symlink(target, path.join(fixture.homeDir, '.elydora'), 'junction');
        } else {
          assert.equal((await fixture.install()).code, 0);
          const targetFile = path.join(target, 'wrapper');
          await writeFile(targetFile, 'external wrapper\n');
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

test('Managed runtime extensions reject paths outside the agent directory', async () => {
  const fixture = await createFixture();
  try {
    const source = `
      import { prepareManagedInstallation } from ${JSON.stringify(managedInstallationModuleUrl)};
      const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
      await prepareManagedInstallation({
        agentKey: 'augment',
        displayName: 'Augment Code CLI',
        hookSources: [{
          directoryLabel: 'Auggie configuration directory',
          label: 'Auggie user settings',
          filePath: process.env.ELYDORA_SETTINGS_PATH,
          source: '{}\\n',
        }],
        runtimeFiles: [{
          fileName: '../outside.js',
          label: 'Unsafe runtime',
          source: 'unsafe',
          mode: 448,
        }],
        config,
      }, 'guard.js', 'hook.js');
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...testEnvironment(fixture),
        ELYDORA_SETTINGS_PATH: fixture.settingsPath,
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /fileName must be a basename/i);
    await assertMissing(path.join(fixture.agentDir, '..', 'outside.js'));
  } finally {
    await fixture.close();
  }
});

test('Auggie installation leaves no transaction artifacts', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});
