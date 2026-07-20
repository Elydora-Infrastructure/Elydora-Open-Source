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
  runPlugin,
} from '../test-support/kimi-test-helpers.mjs';

async function createLink(t, target, linkPath, type) {
  try {
    await symlink(target, linkPath, type);
    return true;
  } catch (error) {
    if (error?.code === 'EPERM') {
      t.skip(`symbolic links unavailable: ${error.message}`);
      return false;
    }
    throw error;
  }
}

async function assertMissing(filePath) {
  await assert.rejects(lstat(filePath), { code: 'ENOENT' });
}

test('Kimi rejects linked config files before creating runtimes', async (t) => {
  const fixture = await createFixture({ legacyDetected: false });
  try {
    const target = path.join(fixture.rootDir, 'config-target.toml');
    const source = '# protected target\ntelemetry = false\n';
    await mkdir(path.dirname(fixture.stablePath), { recursive: true });
    await writeFile(target, source, 'utf-8');
    if (!await createLink(t, target, fixture.stablePath, 'file')) return;
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /not a physical file/i);
    assert.equal(await readFile(target, 'utf-8'), source);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});

test('Kimi rejects linked config and runtime directories before writes', async (t) => {
  for (const kind of ['config', 'runtime']) {
    await t.test(kind, async (subtest) => {
      const fixture = await createFixture({ legacyDetected: false });
      try {
        const target = path.join(fixture.rootDir, `${kind}-target`);
        await mkdir(target, { recursive: true });
        const linkPath = kind === 'config'
          ? fixture.kimiHome
          : path.join(fixture.homeDir, '.elydora');
        await mkdir(path.dirname(linkPath), { recursive: true });
        if (!await createLink(subtest, target, linkPath, 'junction')) return;
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, /physical directory/i);
        assert.deepEqual(await readdir(target), []);
        const untouched = kind === 'config' ? fixture.agentDir : fixture.stablePath;
        await assertMissing(untouched);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Kimi status rejects a linked private key', async (t) => {
  const fixture = await createFixture({ legacyDetected: false });
  try {
    assert.equal((await fixture.install()).code, 0);
    const keyPath = path.join(fixture.agentDir, 'private.key');
    const target = path.join(fixture.rootDir, 'private-key-target');
    const source = await readFile(keyPath, 'utf-8');
    await writeFile(target, source, 'utf-8');
    await rm(keyPath);
    if (!await createLink(t, target, keyPath, 'file')) return;
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /not a physical file/i);
    assert.equal(await readFile(target, 'utf-8'), source);
  } finally {
    await fixture.close();
  }
});

test('Kimi rejects orphaned and mismatched runtime identity before config writes', async () => {
  for (const kind of ['orphaned', 'mismatched']) {
    const fixture = await createFixture({ legacyDetected: false });
    try {
      await mkdir(fixture.agentDir, { recursive: true });
      if (kind === 'orphaned') {
        await writeFile(fixture.guardScriptPath, 'orphaned guard\n');
      } else {
        await writeFile(path.join(fixture.agentDir, 'config.json'), JSON.stringify({
          agent_name: 'kimi',
          agent_id: 'another-agent',
        }));
      }
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(
        result.stderr,
        kind === 'orphaned' ? /identity cannot be verified/i : /identity does not match/i,
      );
      await assertMissing(fixture.stablePath);
    } finally {
      await fixture.close();
    }
  }
});

test('Kimi validates managed runtime inputs before creating config files', async (t) => {
  const fixture = await createFixture({ legacyDetected: false });
  try {
    for (const [name, overrides, pattern] of [
      ['agent name', { agentName: 'codex' }, /requires agentName kimi/i],
      ['private key', { privateKey: 'invalid' }, /canonical 32-byte/i],
      ['API origin', { baseUrl: 'https://api.elydora.com/path?token=secret' }, /query parameters/i],
      [
        'runtime path',
        { guardScriptPath: path.join(fixture.homeDir, 'outside', 'guard.js') },
        /managed agent directory/i,
      ],
    ]) {
      await t.test(name, async () => {
        const result = await runPlugin(fixture, 'install', installConfig(fixture, overrides));
        assert.equal(result.code, 1);
        assert.match(result.stderr, pattern);
        await assertMissing(fixture.stablePath);
        await assertMissing(fixture.agentDir);
      });
    }
  } finally {
    await fixture.close();
  }
});

test('Kimi status surfaces malformed referenced runtime metadata', async () => {
  const fixture = await createFixture({ legacyDetected: false });
  try {
    assert.equal((await fixture.install()).code, 0);
    await writeFile(path.join(fixture.agentDir, 'config.json'), '{ malformed');
    const status = await runPlugin(fixture, 'status', null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /parse Elydora runtime config/i);
  } finally {
    await fixture.close();
  }
});
