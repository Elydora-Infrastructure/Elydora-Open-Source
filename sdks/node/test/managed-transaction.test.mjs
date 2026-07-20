import assert from 'node:assert/strict';
import {
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rename,
  rm,
  writeFile,
} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

const transactionModuleUrl = pathToFileURL(
  path.resolve('dist/plugins/managed-transaction.js'),
).href;

async function fixture() {
  const root = await mkdtemp(path.join(os.tmpdir(), 'elydora-managed-transaction-'));
  const directory = path.join(root, 'managed');
  await mkdir(directory);
  const updatePath = path.join(directory, 'update.txt');
  const removePath = path.join(directory, 'remove.txt');
  const createPath = path.join(directory, 'create.txt');
  await writeFile(updatePath, 'old update\n');
  await writeFile(removePath, 'old remove\n');
  return {
    root,
    directory,
    updatePath,
    removePath,
    createPath,
    close: () => rm(root, { recursive: true, force: true }),
  };
}

async function prepareTransaction(state) {
  const { prepareManagedFileChange } = await import(transactionModuleUrl);
  const changes = await Promise.all([
    prepareManagedFileChange({
      filePath: state.updatePath,
      label: 'updated source',
      next: 'new update\n',
      mode: 0o600,
    }),
    prepareManagedFileChange({
      filePath: state.removePath,
      label: 'removed source',
      mode: 0o600,
    }),
    prepareManagedFileChange({
      filePath: state.createPath,
      label: 'created source',
      next: 'new create\n',
      mode: 0o600,
    }),
  ]);
  return {
    displayName: 'test adapter',
    directories: [{ path: state.directory, label: 'test directory' }],
    changes: changes.filter(Boolean),
  };
}

async function assertNoTransactionFiles(directory) {
  const names = await readdir(directory);
  assert.equal(names.some((name) => /\.(tmp|rollback)$/.test(name)), false, names.join(', '));
}

test('managed transaction commits update, removal, and creation atomically', async () => {
  const state = await fixture();
  try {
    const { commitManagedTransaction } = await import(transactionModuleUrl);
    await commitManagedTransaction(await prepareTransaction(state));
    assert.equal(await readFile(state.updatePath, 'utf-8'), 'new update\n');
    await assert.rejects(readFile(state.removePath), { code: 'ENOENT' });
    assert.equal(await readFile(state.createPath, 'utf-8'), 'new create\n');
    await assertNoTransactionFiles(state.directory);
  } finally {
    await state.close();
  }
});

test('managed transaction restores update and removal after a later commit fails', async () => {
  const state = await fixture();
  try {
    const { commitManagedTransaction } = await import(transactionModuleUrl);
    let calls = 0;
    await assert.rejects(
      commitManagedTransaction(await prepareTransaction(state), async (source, destination) => {
        calls += 1;
        if (calls === 3) throw new Error('injected create failure');
        await rename(source, destination);
      }),
      /injected create failure/,
    );
    assert.equal(await readFile(state.updatePath, 'utf-8'), 'old update\n');
    assert.equal(await readFile(state.removePath, 'utf-8'), 'old remove\n');
    await assert.rejects(readFile(state.createPath), { code: 'ENOENT' });
    await assertNoTransactionFiles(state.directory);
  } finally {
    await state.close();
  }
});
