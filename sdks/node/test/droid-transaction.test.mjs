import assert from 'node:assert/strict';
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  assertMissing,
  assertNoTransactionFiles,
  configModuleUrl,
  contractModuleUrl,
  createFixture,
  environment,
  installConfig,
  installationModuleUrl,
  ioModuleUrl,
  runNode,
  writeConfig,
} from '../test-support/droid-test-helpers.mjs';

const managedFiles = ['guard.js', 'config.json', 'private.key', 'hook.js'];

async function snapshotInstallation(fixture, extraPaths = []) {
  const paths = [
    ...managedFiles.map((name) => path.join(fixture.agentDir, name)),
    fixture.rootPath,
    ...extraPaths,
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

function transactionEnvironment(fixture, overrides = {}) {
  return {
    ...environment(fixture),
    ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, overrides)),
  };
}

const readAndRenderSource = `
  import { buildGroup } from ${JSON.stringify(contractModuleUrl)};
  import {
    activeDocument,
    additionsForTarget,
    installationDocuments,
    renderDocument,
  } from ${JSON.stringify(configModuleUrl)};
  import { readSources } from ${JSON.stringify(ioModuleUrl)};
  import {
    preflightDroidInstallation,
    prepareDroidInstallation,
  } from ${JSON.stringify(installationModuleUrl)};
  const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
  const sources = await readSources();
`;

const prepareSource = `${readAndRenderSource}
  const paths = await preflightDroidInstallation(config, sources);
  const target = activeDocument(sources);
  const groups = new Map([
    ['PreToolUse', buildGroup(paths.guardPath)],
    ['PostToolUse', buildGroup(paths.auditPath)],
  ]);
  const rendered = installationDocuments(sources).map((document) => renderDocument(
    document,
    undefined,
    additionsForTarget(document, target, groups),
  ));
  const prepared = await prepareDroidInstallation(config, sources, rendered);
`;

test('Droid rolls back runtime and hooks after a late commit failure', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const source = `${prepareSource}
      import { rename } from 'node:fs/promises';
      import { commitDroidInstallation } from ${JSON.stringify(installationModuleUrl)};
      let calls = 0;
      await commitDroidInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 5) throw new Error('injected Droid rename failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      transactionEnvironment(fixture, { orgId: 'org-updated', token: 'token-updated' }),
      fixture.workspaceDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected Droid rename failure/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Droid detects concurrent active hook replacement before runtime commit', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"hooks":{"PreToolUse":[]},"owner":"concurrent"}\n';
    const source = `${prepareSource}
      import { writeFile } from 'node:fs/promises';
      import { commitDroidInstallation } from ${JSON.stringify(installationModuleUrl)};
      await writeFile(process.env.ELYDORA_CONCURRENT_PATH, process.env.ELYDORA_CONCURRENT);
      await commitDroidInstallation(prepared);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...transactionEnvironment(fixture, { orgId: 'org-updated' }),
        ELYDORA_CONCURRENT_PATH: fixture.rootPath,
        ELYDORA_CONCURRENT: concurrent,
      },
      fixture.workspaceDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /hooks changed during Factory Droid installation/i);
    for (const [filePath, original] of before) {
      if (filePath !== fixture.rootPath) assert.equal(await readFile(filePath, 'utf-8'), original);
    }
    assert.equal(await readFile(fixture.rootPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Droid tracks concurrent creation of an inactive local settings source', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"hooksDisabled":true}\n';
    const source = `${prepareSource}
      import { writeFile } from 'node:fs/promises';
      import { commitDroidInstallation } from ${JSON.stringify(installationModuleUrl)};
      await writeFile(process.env.ELYDORA_LOCAL_PATH, process.env.ELYDORA_CONCURRENT);
      await commitDroidInstallation(prepared);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...transactionEnvironment(fixture, { orgId: 'org-updated' }),
        ELYDORA_LOCAL_PATH: fixture.localSettingsPath,
        ELYDORA_CONCURRENT: concurrent,
      },
      fixture.workspaceDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /local settings changed during Factory Droid installation/i);
    await assertSnapshot(before);
    assert.equal(await readFile(fixture.localSettingsPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Droid tracks concurrent changes to read-only project policy', async () => {
  const fixture = await createFixture({ projectSettings: { hooksDisabled: false } });
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"hooksDisabled":true}\n';
    const source = `${prepareSource}
      import { writeFile } from 'node:fs/promises';
      import { commitDroidInstallation } from ${JSON.stringify(installationModuleUrl)};
      await writeFile(process.env.ELYDORA_POLICY_PATH, process.env.ELYDORA_CONCURRENT);
      await commitDroidInstallation(prepared);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...transactionEnvironment(fixture, { orgId: 'org-updated' }),
        ELYDORA_POLICY_PATH: fixture.projectSettingsPath,
        ELYDORA_CONCURRENT: concurrent,
      },
      fixture.workspaceDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /project settings changed during Factory Droid installation/i);
    await assertSnapshot(before);
    assert.equal(await readFile(fixture.projectSettingsPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Droid rejects same-content stale hook snapshots by physical identity', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const source = `${readAndRenderSource}
      import { readFile, rm, writeFile } from 'node:fs/promises';
      const original = await readFile(sources.root.filePath, 'utf-8');
      await rm(sources.root.filePath);
      await writeFile(sources.root.filePath, original);
      const paths = await preflightDroidInstallation(config, sources);
      const target = activeDocument(sources);
      const groups = new Map([
        ['PreToolUse', buildGroup(paths.guardPath)],
        ['PostToolUse', buildGroup(paths.auditPath)],
      ]);
      const rendered = installationDocuments(sources).map((document) => renderDocument(
        document,
        undefined,
        additionsForTarget(document, target, groups),
      ));
      await prepareDroidInstallation(config, sources, rendered);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      transactionEnvironment(fixture),
      fixture.workspaceDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /hooks changed before installation/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Droid preserves orphaned runtime artifacts without a verifiable identity', async () => {
  const fixture = await createFixture();
  try {
    await mkdir(fixture.agentDir, { recursive: true });
    await writeFile(fixture.guardScriptPath, 'orphaned guard\n');
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /identity cannot be verified/i);
    assert.equal(await readFile(fixture.guardScriptPath, 'utf-8'), 'orphaned guard\n');
    await assertMissing(fixture.rootPath);
  } finally {
    await fixture.close();
  }
});

test('Droid uninstall restores all hook documents after a later removal fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const installed = JSON.parse(
      (await readFile(fixture.rootPath, 'utf-8')).replace(/^\/\/[^\n]*\n/, ''),
    );
    await writeConfig(fixture.settingsPath, { hooks: installed.hooks, owner: 'user' });
    const before = await snapshotInstallation(fixture, [fixture.settingsPath]);
    const source = `
      import { renderDocument, sourceDocuments } from ${JSON.stringify(configModuleUrl)};
      import { readSources } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitDroidUninstall,
        prepareDroidUninstall,
      } from ${JSON.stringify(installationModuleUrl)};
      import { rename } from 'node:fs/promises';
      const sources = await readSources();
      const rendered = sourceDocuments(sources).map((document) => renderDocument(
        document,
        'agent-1',
        new Map(),
      ));
      const prepared = await prepareDroidUninstall(rendered);
      let calls = 0;
      await commitDroidUninstall(prepared, async (from, to) => {
        calls += 1;
        if (calls === 2) throw new Error('injected Droid uninstall failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      environment(fixture),
      fixture.workspaceDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected Droid uninstall failure/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});
