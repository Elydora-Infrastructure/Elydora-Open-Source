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
} from '../test-support/grok-test-helpers.mjs';

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

test('Grok rejects linked hook files before creating runtimes', async (t) => {
  const fixture = await createFixture();
  try {
    const target = path.join(fixture.rootDir, 'hooks-target.json');
    const source = '{"owner":"protected"}\n';
    await mkdir(path.dirname(fixture.configPath), { recursive: true });
    await writeFile(target, source, 'utf-8');
    if (!await createLink(t, target, fixture.configPath, 'file')) return;
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /not a physical file/i);
    assert.equal(await readFile(target, 'utf-8'), source);
    await assertMissing(fixture.agentDir);
  } finally {
    await fixture.close();
  }
});

test('Grok rejects linked home, hooks, and runtime directories before writes', async (t) => {
  for (const kind of ['home', 'hooks', 'runtime']) {
    await t.test(kind, async (subtest) => {
      const fixture = await createFixture();
      try {
        const target = path.join(fixture.rootDir, `${kind}-target`);
        await mkdir(target, { recursive: true });
        let linkPath;
        if (kind === 'home') {
          linkPath = fixture.grokHome;
          await mkdir(path.join(target, 'hooks'), { recursive: true });
          await mkdir(path.dirname(linkPath), { recursive: true });
        } else if (kind === 'hooks') {
          linkPath = path.dirname(fixture.configPath);
          await mkdir(fixture.grokHome, { recursive: true });
        } else {
          linkPath = path.join(fixture.homeDir, '.elydora');
          await mkdir(path.dirname(linkPath), { recursive: true });
        }
        if (!await createLink(subtest, target, linkPath, 'junction')) return;
        const result = await fixture.install();
        assert.equal(result.code, 1);
        assert.match(result.stderr, /physical directory/i);
        const untouched = kind === 'runtime' ? fixture.configPath : fixture.agentDir;
        await assertMissing(untouched);
      } finally {
        await fixture.close();
      }
    });
  }
});

test('Grok status rejects a linked private key', async (t) => {
  const fixture = await createFixture();
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

test('Grok rejects orphaned and mismatched runtime identity before hook writes', async () => {
  for (const kind of ['orphaned', 'mismatched']) {
    const fixture = await createFixture();
    try {
      await mkdir(fixture.agentDir, { recursive: true });
      if (kind === 'orphaned') {
        await writeFile(fixture.guardScriptPath, 'orphaned guard\n');
      } else {
        await writeFile(path.join(fixture.agentDir, 'config.json'), JSON.stringify({
          agent_name: 'grok',
          agent_id: 'another-agent',
        }));
      }
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(
        result.stderr,
        kind === 'orphaned' ? /identity cannot be verified/i : /identity does not match/i,
      );
      await assertMissing(fixture.configPath);
    } finally {
      await fixture.close();
    }
  }
});

test('Grok validates managed runtime inputs before creating hook files', async (t) => {
  const fixture = await createFixture();
  try {
    for (const [name, overrides, pattern] of [
      ['agent name', { agentName: 'codex' }, /requires agentName grok/i],
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
        await assertMissing(fixture.configPath);
        await assertMissing(fixture.agentDir);
      });
    }
  } finally {
    await fixture.close();
  }
});

test('Grok status surfaces malformed referenced runtime metadata', async () => {
  const fixture = await createFixture();
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
