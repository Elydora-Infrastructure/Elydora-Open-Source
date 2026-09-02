import assert from 'node:assert/strict';
import { lstat, mkdir, readFile, readdir, symlink, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  configModuleUrl,
  contractModuleUrl,
  createFixture,
  environment,
  installConfig,
  installationModuleUrl,
  runNode,
  sourcesModuleUrl,
} from '../test-support/letta.mjs';

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
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

async function runPreparedInstallation(fixture, {
  failSettings = false,
  racePath,
  raceSource,
} = {}) {
  const source = `
    import fsp from 'node:fs/promises';
    import { buildLettaGroup } from ${JSON.stringify(contractModuleUrl)};
    import { renderLettaDocument } from ${JSON.stringify(configModuleUrl)};
    import { readLettaSources } from ${JSON.stringify(sourcesModuleUrl)};
    import {
      commitLettaInstallation,
      preflightLettaInstallation,
      prepareLettaInstallation,
    } from ${JSON.stringify(installationModuleUrl)};

    const config = JSON.parse(process.env.ELYDORA_TEST_CONFIG);
    const sources = await readLettaSources();
    const paths = await preflightLettaInstallation(config, sources);
    const rendered = renderLettaDocument(sources.global, undefined, new Map([
      ['PreToolUse', buildLettaGroup(paths.guardPath)],
      ['PostToolUse', buildLettaGroup(paths.auditPath)],
      ['PostToolUseFailure', buildLettaGroup(paths.auditPath)],
    ]));
    const prepared = await prepareLettaInstallation(config, sources, rendered);
    if (process.env.ELYDORA_TEST_RACE_PATH) {
      await fsp.writeFile(
        process.env.ELYDORA_TEST_RACE_PATH,
        process.env.ELYDORA_TEST_RACE_SOURCE,
      );
    }
    await commitLettaInstallation(prepared, async (from, to) => {
      if (process.env.ELYDORA_TEST_FAIL_SETTINGS === 'true'
        && to === sources.global.filePath) {
        throw new Error('injected Letta settings rename failure');
      }
      await fsp.rename(from, to);
    });
  `;
  return runNode(
    ['--input-type=module', '--eval', source],
    {
      ...environment(fixture),
      ELYDORA_TEST_CONFIG: JSON.stringify(installConfig(fixture)),
      ELYDORA_TEST_FAIL_SETTINGS: String(failSettings),
      ELYDORA_TEST_RACE_PATH: racePath,
      ELYDORA_TEST_RACE_SOURCE: raceSource,
    },
    fixture.projectDir,
  );
}

test('Letta rolls back every runtime file when the settings commit fails', async () => {
  const original = '{"theme":"dark"}\n';
  const fixture = await createFixture({ globalSettings: original });
  try {
    const result = await runPreparedInstallation(fixture, { failSettings: true });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected Letta settings rename failure/i);
    assert.equal(await readFile(fixture.globalPath, 'utf-8'), original);
    for (const filePath of [
      fixture.guardScriptPath,
      fixture.hookScriptPath,
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
    ]) await assertMissing(filePath);
    await assertNoTransactionFiles(path.dirname(fixture.globalPath), fixture.agentDir);
  } finally {
    await fixture.close();
  }
});

test('Letta aborts when a read-only project source changes after prepare', async () => {
  const original = '{"windowTitle":"before"}\n';
  const raceSource = '{"windowTitle":"after"}\n';
  const fixture = await createFixture({ projectSettings: original });
  try {
    const result = await runPreparedInstallation(fixture, {
      racePath: fixture.projectPath,
      raceSource,
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /project settings changed during Letta Code installation/i);
    assert.equal(await readFile(fixture.projectPath, 'utf-8'), raceSource);
    await assertMissing(fixture.globalPath);
    for (const filePath of [
      fixture.guardScriptPath,
      fixture.hookScriptPath,
      path.join(fixture.agentDir, 'config.json'),
      path.join(fixture.agentDir, 'private.key'),
    ]) await assertMissing(filePath);
    await assertNoTransactionFiles(path.dirname(fixture.globalPath), fixture.agentDir);
  } finally {
    await fixture.close();
  }
});

test('Letta rejects linked configuration directories before writes', {
  skip: process.platform === 'win32' ? 'directory symlinks require platform privileges on Windows' : false,
}, async () => {
  const fixture = await createFixture();
  const target = path.join(fixture.rootDir, 'redirected-letta');
  await mkdir(target);
  await symlink(target, path.dirname(fixture.globalPath), 'dir');
  try {
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /global configuration directory is not a physical directory/i);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});

test('Letta rejects a settings directory at the settings file path', async () => {
  const fixture = await createFixture();
  await mkdir(fixture.globalPath, { recursive: true });
  try {
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /global settings path is not a physical file/i);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});
