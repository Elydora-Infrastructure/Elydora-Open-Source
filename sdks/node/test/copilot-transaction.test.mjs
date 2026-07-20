import assert from 'node:assert/strict';
import {
  lstat,
  mkdir,
  readFile,
  readdir,
  rm,
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
  legacyManagedConfig,
  runNode,
  writeJson,
} from '../test-support/copilot-test-helpers.mjs';

const managedFiles = ['guard.js', 'config.json', 'private.key', 'hook.js'];

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

async function snapshotInstallation(fixture, includeLegacy = false) {
  const paths = [
    ...managedFiles.map((name) => path.join(fixture.agentDir, name)),
    fixture.configPath,
    ...(includeLegacy ? [fixture.legacyPath] : []),
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
  for (const directory of [fixture.agentDir, fixture.hooksDir, path.dirname(fixture.legacyPath)]) {
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

const readAndRenderSource = `
  import {
    buildHandler,
    removeManagedHooks,
    renderDocument,
  } from ${JSON.stringify(contractModuleUrl)};
  import { readSources } from ${JSON.stringify(ioModuleUrl)};
  import {
    preflightCopilotInstallation,
    prepareCopilotInstallation,
  } from ${JSON.stringify(installationModuleUrl)};
  const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
  const sources = await readSources();
`;

const prepareSource = `${readAndRenderSource}
  const paths = await preflightCopilotInstallation(config, sources);
  const hooks = removeManagedHooks(sources.user.hooks);
  hooks.preToolUse = [...(hooks.preToolUse ?? []), buildHandler(paths.guardPath)];
  hooks.postToolUse = [...(hooks.postToolUse ?? []), buildHandler(paths.auditPath)];
  hooks.postToolUseFailure = [
    ...(hooks.postToolUseFailure ?? []),
    buildHandler(paths.auditPath),
  ];
  const rendered = [renderDocument(sources.user, hooks)];
  if (sources.legacy) {
    rendered.push(renderDocument(sources.legacy, removeManagedHooks(sources.legacy.hooks)));
  }
  const prepared = await prepareCopilotInstallation(config, sources, rendered);
`;

test('Copilot rolls back runtime, user hooks, and legacy migration after a late failure', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const installed = await readFile(fixture.configPath, 'utf-8');
    await writeJson(fixture.legacyPath, installed);
    const before = await snapshotInstallation(fixture, true);
    const source = `${prepareSource}
      import { commitCopilotInstallation } from ${JSON.stringify(installationModuleUrl)};
      import { rename } from 'node:fs/promises';
      let calls = 0;
      await commitCopilotInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 6) throw new Error('injected Copilot rename failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      transactionEnvironment(fixture, { orgId: 'org-updated', token: 'token-updated' }),
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected Copilot rename failure/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Copilot detects concurrent hook replacement before committing runtime files', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"version":1,"hooks":{"preToolUse":[{"command":"concurrent"}]}}\n';
    const source = `${prepareSource}
      import { writeFile } from 'node:fs/promises';
      import { commitCopilotInstallation } from ${JSON.stringify(installationModuleUrl)};
      await writeFile(process.env.ELYDORA_CONCURRENT_PATH, process.env.ELYDORA_CONCURRENT);
      await commitCopilotInstallation(prepared);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...transactionEnvironment(fixture, { orgId: 'org-updated' }),
        ELYDORA_CONCURRENT_PATH: fixture.configPath,
        ELYDORA_CONCURRENT: concurrent,
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during GitHub Copilot CLI installation/i);
    for (const [filePath, sourceBefore] of before) {
      if (filePath !== fixture.configPath) {
        assert.equal(await readFile(filePath, 'utf-8'), sourceBefore, filePath);
      }
    }
    assert.equal(await readFile(fixture.configPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Copilot rolls back when effective settings change during installation', async () => {
  const fixture = await createFixture({ repositorySettings: { disableAllHooks: false } });
  const settingsPath = path.join(
    fixture.projectDir,
    '.github',
    'copilot',
    'settings.json',
  );
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const concurrent = '{"disableAllHooks":true}\n';
    const source = `${prepareSource}
      import { rename, writeFile } from 'node:fs/promises';
      import { commitCopilotInstallation } from ${JSON.stringify(installationModuleUrl)};
      let calls = 0;
      await commitCopilotInstallation(prepared, async (from, to) => {
        calls += 1;
        await rename(from, to);
        if (calls === 3) {
          await writeFile(process.env.ELYDORA_SETTINGS_PATH, process.env.ELYDORA_CONCURRENT);
        }
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...transactionEnvironment(fixture, { orgId: 'org-updated' }),
        ELYDORA_SETTINGS_PATH: settingsPath,
        ELYDORA_CONCURRENT: concurrent,
      },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /settings changed during GitHub Copilot CLI installation/i);
    await assertSnapshot(before);
    assert.equal(await readFile(settingsPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Copilot rejects same-content stale snapshots by physical file identity', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotInstallation(fixture);
    const source = `${readAndRenderSource}
      import { readFile, rm, writeFile } from 'node:fs/promises';
      const original = await readFile(sources.user.filePath, 'utf-8');
      await rm(sources.user.filePath);
      await writeFile(sources.user.filePath, original);
      const paths = await preflightCopilotInstallation(config, sources);
      const hooks = removeManagedHooks(sources.user.hooks);
      hooks.preToolUse = [buildHandler(paths.guardPath)];
      hooks.postToolUse = [buildHandler(paths.auditPath)];
      hooks.postToolUseFailure = [buildHandler(paths.auditPath)];
      const rendered = [renderDocument(sources.user, hooks)];
      await prepareCopilotInstallation(config, sources, rendered);
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      transactionEnvironment(fixture),
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed before installation/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Copilot preserves orphaned runtime artifacts without a verifiable identity', async () => {
  const fixture = await createFixture();
  try {
    await mkdir(fixture.agentDir, { recursive: true });
    await writeFile(fixture.guardScriptPath, 'orphaned guard\n');
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /identity cannot be verified/i);
    assert.equal(await readFile(fixture.guardScriptPath, 'utf-8'), 'orphaned guard\n');
    await assertMissing(fixture.configPath);
  } finally {
    await fixture.close();
  }
});

test('Copilot uninstall restores both hook documents when the second removal fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const installed = await readFile(fixture.configPath, 'utf-8');
    await writeJson(fixture.legacyPath, installed);
    const before = await snapshotInstallation(fixture, true);
    const source = `
      import { removeManagedHooks, renderDocument } from ${JSON.stringify(contractModuleUrl)};
      import { readSources } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitCopilotUninstall,
        prepareCopilotUninstall,
      } from ${JSON.stringify(installationModuleUrl)};
      import { rename } from 'node:fs/promises';
      const sources = await readSources();
      const rendered = [
        renderDocument(sources.user, removeManagedHooks(sources.user.hooks, 'agent-1')),
        renderDocument(sources.legacy, removeManagedHooks(sources.legacy.hooks, 'agent-1')),
      ];
      const prepared = await prepareCopilotUninstall(rendered);
      let calls = 0;
      await commitCopilotUninstall(prepared, async (from, to) => {
        calls += 1;
        if (calls === 2) throw new Error('injected Copilot uninstall failure');
        await rename(from, to);
      });
    `;
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      environment(fixture),
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected Copilot uninstall failure/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Copilot detects legacy migration snapshots that change before preparation', async () => {
  const fixture = await createFixture();
  try {
    await writeJson(fixture.legacyPath, legacyManagedConfig(fixture));
    const original = await readFile(fixture.legacyPath, 'utf-8');
    const source = `${readAndRenderSource}
      import { writeFile } from 'node:fs/promises';
      await writeFile(sources.legacy.filePath, process.env.ELYDORA_CHANGED);
      const paths = await preflightCopilotInstallation(config, sources);
      const hooks = removeManagedHooks(sources.user.hooks);
      hooks.preToolUse = [buildHandler(paths.guardPath)];
      hooks.postToolUse = [buildHandler(paths.auditPath)];
      hooks.postToolUseFailure = [buildHandler(paths.auditPath)];
      const rendered = [
        renderDocument(sources.user, hooks),
        renderDocument(sources.legacy, removeManagedHooks(sources.legacy.hooks)),
      ];
      await prepareCopilotInstallation(config, sources, rendered);
    `;
    const changed = original.replace('"version": 1', '"version": 1,\n  "owner": "concurrent"');
    const result = await runNode(
      ['--input-type=module', '--eval', source],
      { ...transactionEnvironment(fixture), ELYDORA_CHANGED: changed },
      fixture.projectDir,
    );
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed before installation/i);
    assert.equal(await readFile(fixture.legacyPath, 'utf-8'), changed);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
