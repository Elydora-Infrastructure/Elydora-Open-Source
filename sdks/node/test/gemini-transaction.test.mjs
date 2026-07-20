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
  writeSettings,
} from '../test-support/gemini-test-helpers.mjs';

const managedFileNames = ['guard.js', 'config.json', 'private.key', 'hook.js'];

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
    GEMINI_CLI_HOME: fixture.geminiCliHome,
    ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, overrides)),
  };
}

test('Gemini rolls back all five files when a transaction commit fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const source = `
      import { rename } from 'node:fs/promises';
      import { readGeminiDocument } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitGeminiInstallation,
        prepareGeminiInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
      const document = await readGeminiDocument();
      const prepared = await prepareGeminiInstallation(config, { document, changed: false });
      let calls = 0;
      await commitGeminiInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 3) throw new Error('injected rename failure');
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

test('Gemini detects concurrent settings changes before runtime commit', async () => {
  const fixture = await createFixture({ settings: { theme: 'GitHub' } });
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"theme":"Atom One","hooks":{"Notification":[]}}\n';
    const source = `
      import { writeFile } from 'node:fs/promises';
      import { readGeminiDocument } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitGeminiInstallation,
        prepareGeminiInstallation,
      } from ${JSON.stringify(installationModuleUrl)};
      const document = await readGeminiDocument();
      const prepared = await prepareGeminiInstallation(
        JSON.parse(process.env.ELYDORA_INSTALL_CONFIG),
        { document, changed: false },
      );
      await writeFile(document.filePath, process.env.ELYDORA_CONCURRENT, 'utf-8');
      await commitGeminiInstallation(prepared);
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
    assert.match(result.stderr, /changed during Gemini CLI installation/i);
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

test('Gemini rejects orphaned and mismatched runtime identities before writes', async (t) => {
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
      await writeSettings(path.join(fixture.agentDir, 'config.json'), {
        agent_name: 'gemini',
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

test('Gemini rejects linked configuration, settings, and runtime paths', async (t) => {
  for (const kind of ['configuration', 'settings', 'runtime']) {
    await t.test(kind, async () => {
      const fixture = await createFixture();
      try {
        const target = path.join(fixture.rootDir, `${kind}-target`);
        await mkdir(target, { recursive: true });
        await mkdir(fixture.homeDir, { recursive: true });
        let link;
        let linkType = 'junction';
        if (kind === 'configuration') {
          link = path.dirname(fixture.settingsPath);
        } else if (kind === 'runtime') {
          link = path.join(fixture.homeDir, '.elydora');
        } else {
          await mkdir(path.dirname(fixture.settingsPath), { recursive: true });
          const targetFile = path.join(target, 'settings.json');
          await writeFile(targetFile, '{}\n');
          link = fixture.settingsPath;
          linkType = 'file';
          await symlink(targetFile, link, linkType);
        }
        if (kind !== 'settings') await symlink(target, link, linkType);
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

test('Gemini installation leaves no transaction artifacts', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});
