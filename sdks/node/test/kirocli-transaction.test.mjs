import assert from 'node:assert/strict';
import { lstat, readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  contractModuleUrl,
  createFixture,
  environment,
  installConfig,
  installationModuleUrl,
  ioModuleUrl,
  kiroV1ModuleUrl,
  managedV2Document,
  runNode,
} from '../test-support/kirocli-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

async function runPreparedInstallation(fixture, { failV3 = false, raceV2 } = {}) {
  const source = `
    import fsp from 'node:fs/promises';
    import {
      buildKiroCliV2Hooks,
      buildKiroCliV3Hooks,
      renderKiroCliV2Installation,
    } from ${JSON.stringify(contractModuleUrl)};
    import {
      commitKiroCliInstallation,
      preflightKiroCliInstallation,
      prepareKiroCliInstallation,
    } from ${JSON.stringify(installationModuleUrl)};
    import { readKiroCliSources } from ${JSON.stringify(ioModuleUrl)};
    import { renderKiroIdeDocument } from ${JSON.stringify(kiroV1ModuleUrl)};

    const config = JSON.parse(process.env.ELYDORA_TEST_CONFIG);
    const sources = await readKiroCliSources();
    const paths = await preflightKiroCliInstallation(config, sources);
    const v2 = renderKiroCliV2Installation(
      sources.v2,
      buildKiroCliV2Hooks(sources.v2, paths.guardPath, paths.auditPath),
    );
    const v3 = renderKiroIdeDocument(
      sources.v3,
      buildKiroCliV3Hooks(sources.v3.hooks, paths.guardPath, paths.auditPath),
    );
    const prepared = await prepareKiroCliInstallation(config, sources, v2, v3);
    if (process.env.ELYDORA_TEST_RACE_V2) {
      await fsp.writeFile(sources.paths.v2Path, process.env.ELYDORA_TEST_RACE_V2);
    }
    let failed = false;
    await commitKiroCliInstallation(prepared, async (from, to) => {
      if (!failed
        && process.env.ELYDORA_TEST_FAIL_V3 === 'true'
        && to === sources.paths.v3Path
        && from.endsWith('.tmp')) {
        failed = true;
        throw new Error('injected Kiro CLI v3 rename failure');
      }
      await fsp.rename(from, to);
    });
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      ...environment(fixture),
      ELYDORA_TEST_CONFIG: JSON.stringify(installConfig(fixture)),
      ELYDORA_TEST_FAIL_V3: String(failV3),
      ELYDORA_TEST_RACE_V2: raceV2,
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

test('Kiro CLI rolls back v2, v3, and runtime files after a late commit failure', async () => {
  const originalV2 = managedV2Document({ owner: 'v2', hooks: {} });
  const originalV3 = { version: 'v1', owner: 'v3', hooks: [] };
  const fixture = await createFixture({ existingV2: originalV2, existingV3: originalV3 });
  try {
    const result = await runPreparedInstallation(fixture, { failV3: true });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected Kiro CLI v3 rename failure/);
    assert.deepEqual(JSON.parse(await readFile(fixture.v2Path, 'utf-8')), originalV2);
    assert.deepEqual(JSON.parse(await readFile(fixture.v3Path, 'utf-8')), originalV3);
    for (const filePath of [
      fixture.guardScriptPath,
      fixture.hookScriptPath,
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
    ]) await assertMissing(filePath);
    await assertNoTransactionFiles(
      path.dirname(fixture.v2Path),
      path.dirname(fixture.v3Path),
      fixture.agentDir,
    );
  } finally {
    await fixture.close();
  }
});

test('Kiro CLI detects concurrent source changes before the first commit', async () => {
  const originalV2 = managedV2Document({ hooks: {} });
  const originalV3 = { version: 'v1', hooks: [] };
  const raceV2 = '{"name":"elydora-audit","race":true,"hooks":{}}\n';
  const fixture = await createFixture({ existingV2: originalV2, existingV3: originalV3 });
  try {
    const result = await runPreparedInstallation(fixture, { raceV2 });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during Kiro CLI installation/i);
    assert.equal(await readFile(fixture.v2Path, 'utf-8'), raceV2);
    assert.deepEqual(JSON.parse(await readFile(fixture.v3Path, 'utf-8')), originalV3);
    await assertMissing(fixture.guardScriptPath);
    await assertMissing(fixture.hookScriptPath);
    await assertNoTransactionFiles(path.dirname(fixture.v2Path), fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
