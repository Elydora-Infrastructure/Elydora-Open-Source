import assert from 'node:assert/strict';
import { lstat, mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
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
} from '../test-support/kiroide-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

function legacyDocument(fixture) {
  return {
    name: 'Elydora Audit',
    description: 'Sends tool-use events to the Elydora tamper-evident audit platform',
    version: '1.0.0',
    hooks: {
      pre_tool_use: {
        command: `node "${fixture.guardScriptPath}"`,
        timeout_ms: 5000,
      },
      post_tool_use: {
        command: `node "${fixture.hookScriptPath}"`,
        timeout_ms: 5000,
      },
    },
  };
}

async function writeJson(filePath, value) {
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
}

async function runPreparedInstallation(fixture, { failLegacy = false, raceSource } = {}) {
  const source = `
    import fsp from 'node:fs/promises';
    import {
      buildKiroIdeHook,
      renderKiroIdeDocument,
      withoutManagedKiroIdeHooks,
    } from ${JSON.stringify(contractModuleUrl)};
    import { readKiroIdeSources } from ${JSON.stringify(ioModuleUrl)};
    import {
      commitKiroIdeInstallation,
      preflightKiroIdeInstallation,
      prepareKiroIdeInstallation,
    } from ${JSON.stringify(installationModuleUrl)};

    const config = JSON.parse(process.env.ELYDORA_TEST_CONFIG);
    const sources = await readKiroIdeSources();
    const paths = await preflightKiroIdeInstallation(config, sources.paths);
    const rendered = renderKiroIdeDocument(sources.document, [
      ...withoutManagedKiroIdeHooks(sources.document.hooks),
      buildKiroIdeHook('elydora-guard', paths.guardPath),
      buildKiroIdeHook('elydora-audit', paths.auditPath),
    ]);
    const prepared = await prepareKiroIdeInstallation(config, sources, rendered);
    if (process.env.ELYDORA_TEST_RACE_SOURCE) {
      await fsp.writeFile(sources.paths.configPath, process.env.ELYDORA_TEST_RACE_SOURCE);
    }
    await commitKiroIdeInstallation(prepared, async (from, to) => {
      if (process.env.ELYDORA_TEST_FAIL_LEGACY === 'true'
        && from === sources.paths.legacyConfigPath) {
        throw new Error('injected legacy rename failure');
      }
      await fsp.rename(from, to);
    });
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      ...environment(fixture),
      ELYDORA_TEST_CONFIG: JSON.stringify(installConfig(fixture)),
      ELYDORA_TEST_FAIL_LEGACY: String(failLegacy),
      ELYDORA_TEST_RACE_SOURCE: raceSource,
    },
    fixture.projectDir,
  );
}

async function assertNoTransactionFiles(...directories) {
  const names = [];
  for (const directory of directories) {
    try {
      names.push(...await readdir(directory));
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
  }
  assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false, names.join(', '));
}

test('Kiro IDE rolls back runtime and workspace hooks when legacy cleanup fails', async () => {
  const original = { version: 'v1', owner: 'workspace', hooks: [] };
  const fixture = await createFixture({ existingConfig: original });
  try {
    await writeJson(fixture.legacyConfigPath, legacyDocument(fixture));
    const result = await runPreparedInstallation(fixture, { failLegacy: true });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected legacy rename failure/);
    assert.deepEqual(JSON.parse(await readFile(fixture.configPath, 'utf-8')), original);
    assert.equal((await readFile(fixture.legacyConfigPath, 'utf-8')).length > 0, true);
    for (const filePath of [
      fixture.guardScriptPath,
      fixture.hookScriptPath,
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
    ]) await assertMissing(filePath);
    await assertNoTransactionFiles(
      fixture.hooksDir,
      fixture.agentDir,
      path.dirname(fixture.legacyConfigPath),
    );
  } finally {
    await fixture.close();
  }
});

test('Kiro IDE detects workspace hook races before the first commit', async () => {
  const original = { version: 'v1', hooks: [] };
  const raceSource = '{"version":"v1","hooks":[],"race":true}\n';
  const fixture = await createFixture({ existingConfig: original });
  try {
    const result = await runPreparedInstallation(fixture, { raceSource });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during Kiro IDE installation/i);
    assert.equal(await readFile(fixture.configPath, 'utf-8'), raceSource);
    await assertMissing(fixture.guardScriptPath);
    await assertMissing(fixture.hookScriptPath);
    await assertNoTransactionFiles(fixture.hooksDir, fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
