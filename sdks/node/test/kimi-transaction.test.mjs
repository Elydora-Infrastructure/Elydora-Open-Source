import assert from 'node:assert/strict';
import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  contractModuleUrl,
  createFixture,
  installationModuleUrl,
  installConfig,
  ioModuleUrl,
  runNode,
} from '../test-support/kimi-test-helpers.mjs';

const runtimeNames = ['guard.js', 'config.json', 'private.key', 'hook.js'];

async function snapshotFixture(fixture) {
  const paths = [
    ...runtimeNames.map((name) => path.join(fixture.agentDir, name)),
    fixture.stablePath,
    fixture.legacyPath,
  ];
  return new Map(await Promise.all(paths.map(async (filePath) => [
    filePath,
    await readFile(filePath, 'utf-8'),
  ])));
}

async function assertSnapshot(snapshot) {
  for (const [filePath, contents] of snapshot) {
    assert.equal(await readFile(filePath, 'utf-8'), contents, filePath);
  }
}

async function assertNoTransactionFiles(fixture) {
  for (const directory of [fixture.agentDir, fixture.kimiHome, fixture.legacyHome]) {
    const names = await readdir(directory);
    assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false, names.join(', '));
  }
}

function runFixtureSource(fixture, source, environment = {}) {
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      KIMI_CODE_HOME: fixture.kimiHome,
      ...environment,
    },
    fixture.projectDir,
  );
}

const prepareInstallSource = `
  import {
    buildKimiHook,
    removeManagedKimiHooks,
    renderKimiDocument,
  } from ${JSON.stringify(contractModuleUrl)};
  import { readKimiDocuments } from ${JSON.stringify(ioModuleUrl)};
  import {
    preflightKimiInstallation,
    prepareKimiInstallation,
  } from ${JSON.stringify(installationModuleUrl)};
  const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
  const documents = await readKimiDocuments();
  const paths = await preflightKimiInstallation(config, documents);
  const rendered = await Promise.all(documents.map((document) => renderKimiDocument(document, [
    ...removeManagedKimiHooks(document.hooks),
    buildKimiHook('PreToolUse', paths.guardPath),
    buildKimiHook('PostToolUse', paths.auditPath),
    buildKimiHook('PostToolUseFailure', paths.auditPath),
  ])));
  const prepared = await prepareKimiInstallation(config, rendered);
`;

test('Kimi restores both configs and four runtime files after the final commit fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotFixture(fixture);
    const source = `${prepareInstallSource}
      import { rename } from 'node:fs/promises';
      import { commitKimiInstallation } from ${JSON.stringify(installationModuleUrl)};
      let calls = 0;
      await commitKimiInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 6) throw new Error('injected legacy config failure');
        await rename(from, to);
      });
    `;
    const result = await runFixtureSource(fixture, source, {
      ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, {
        orgId: 'org-updated',
        token: 'token-updated',
      })),
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected legacy config failure/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Kimi rejects concurrent config changes before committing runtime updates', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotFixture(fixture);
    const concurrent = '# concurrent owner change\ntelemetry = false\n';
    const source = `${prepareInstallSource}
      import { writeFile } from 'node:fs/promises';
      import { commitKimiInstallation } from ${JSON.stringify(installationModuleUrl)};
      await writeFile(documents[0].contract.configPath, process.env.ELYDORA_CONCURRENT, 'utf-8');
      await commitKimiInstallation(prepared);
    `;
    const result = await runFixtureSource(fixture, source, {
      ELYDORA_CONCURRENT: concurrent,
      ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, { orgId: 'updated' })),
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during Kimi installation/i);
    for (const [filePath, contents] of before) {
      if (filePath !== fixture.stablePath) {
        assert.equal(await readFile(filePath, 'utf-8'), contents, filePath);
      }
    }
    assert.equal(await readFile(fixture.stablePath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Kimi uninstall restores the first config when removing the second config fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotFixture(fixture);
    const source = `
      import { rename } from 'node:fs/promises';
      import {
        removeManagedKimiHooks,
        renderKimiDocument,
      } from ${JSON.stringify(contractModuleUrl)};
      import { readKimiDocuments } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitKimiUninstall,
        prepareKimiUninstall,
      } from ${JSON.stringify(installationModuleUrl)};
      const documents = await readKimiDocuments();
      const rendered = await Promise.all(documents.map((document) => renderKimiDocument(
        document,
        removeManagedKimiHooks(document.hooks, 'agent-1'),
      )));
      const prepared = await prepareKimiUninstall(rendered);
      let calls = 0;
      await commitKimiUninstall(prepared, async (from, to) => {
        calls += 1;
        if (calls === 2) throw new Error('injected uninstall failure');
        await rename(from, to);
      });
    `;
    const result = await runFixtureSource(fixture, source);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected uninstall failure/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});
