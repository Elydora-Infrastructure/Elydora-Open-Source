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
} from '../test-support/opencode-test-helpers.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

async function runPreparedInstallation(fixture, { failLegacy = false, racePlugin } = {}) {
  const source = `
    import fsp from 'node:fs/promises';
    import path from 'node:path';
    import {
      commitOpenCodeInstallation,
      prepareOpenCodeInstallation,
    } from ${JSON.stringify(installationModuleUrl)};
    import { readOpenCodeSources } from ${JSON.stringify(ioModuleUrl)};

    const config = JSON.parse(process.env.ELYDORA_TEST_CONFIG);
    const sources = await readOpenCodeSources();
    const prepared = await prepareOpenCodeInstallation(config, sources);
    if (process.env.ELYDORA_TEST_RACE_PLUGIN) {
      await fsp.mkdir(path.dirname(sources.paths.pluginPath), { recursive: true });
      await fsp.writeFile(sources.paths.pluginPath, process.env.ELYDORA_TEST_RACE_PLUGIN);
    }
    let failed = false;
    await commitOpenCodeInstallation(prepared, async (from, to) => {
      if (!failed
        && process.env.ELYDORA_TEST_FAIL_LEGACY === 'true'
        && from === sources.paths.legacyPluginPath
        && to.endsWith('.rollback')) {
        failed = true;
        throw new Error('injected OpenCode legacy migration failure');
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
      ELYDORA_TEST_RACE_PLUGIN: racePlugin,
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

test('OpenCode rolls back plugin, legacy migration, and runtime after a late failure', async () => {
  const fixture = await createFixture();
  const { buildLegacyOpenCodePlugin } = await import(contractModuleUrl);
  const legacy = buildLegacyOpenCodePlugin(fixture.hookScriptPath, fixture.guardScriptPath);
  try {
    await mkdir(path.dirname(fixture.legacyPluginPath), { recursive: true });
    await writeFile(fixture.legacyPluginPath, legacy);
    const result = await runPreparedInstallation(fixture, { failLegacy: true });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected OpenCode legacy migration failure/);
    assert.equal(await readFile(fixture.legacyPluginPath, 'utf-8'), legacy);
    await assertMissing(fixture.pluginPath);
    for (const filePath of [
      fixture.guardScriptPath,
      fixture.hookScriptPath,
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
    ]) await assertMissing(filePath);
    await assertNoTransactionFiles(fixture.pluginsDir, fixture.agentDir);
  } finally {
    await fixture.close();
  }
});

test('OpenCode detects concurrent plugin creation before committing runtime files', async () => {
  const fixture = await createFixture();
  const racePlugin = 'export const RacePlugin = async () => ({});\n';
  try {
    const result = await runPreparedInstallation(fixture, { racePlugin });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during OpenCode installation/i);
    assert.equal(await readFile(fixture.pluginPath, 'utf-8'), racePlugin);
    await assertMissing(fixture.guardScriptPath);
    await assertMissing(fixture.hookScriptPath);
    await assertNoTransactionFiles(fixture.pluginsDir, fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
