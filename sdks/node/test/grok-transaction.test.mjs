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
} from '../test-support/grok-test-helpers.mjs';

const runtimeNames = ['guard.js', 'config.json', 'private.key', 'hook.js'];

async function snapshotFixture(fixture) {
  const paths = [
    ...runtimeNames.map((name) => path.join(fixture.agentDir, name)),
    fixture.configPath,
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
  for (const directory of [fixture.agentDir, path.dirname(fixture.configPath)]) {
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
      GROK_HOME: fixture.grokHome,
      ...environment,
    },
    fixture.projectDir,
  );
}

const prepareInstallSource = `
  import {
    buildGrokGroup,
    removeManagedGrokHooks,
    renderGrokDocument,
  } from ${JSON.stringify(contractModuleUrl)};
  import { readGrokDocument } from ${JSON.stringify(ioModuleUrl)};
  import {
    preflightGrokInstallation,
    prepareGrokInstallation,
  } from ${JSON.stringify(installationModuleUrl)};
  const config = JSON.parse(process.env.ELYDORA_INSTALL_CONFIG);
  const document = await readGrokDocument();
  const paths = await preflightGrokInstallation(config, document.filePath);
  const cleaned = removeManagedGrokHooks(document.hooks);
  const hooks = {
    ...cleaned,
    PreToolUse: [...(cleaned.PreToolUse ?? []), buildGrokGroup(paths.guardPath)],
    PostToolUse: [...(cleaned.PostToolUse ?? []), buildGrokGroup(paths.auditPath)],
    PostToolUseFailure: [
      ...(cleaned.PostToolUseFailure ?? []),
      buildGrokGroup(paths.auditPath),
    ],
  };
  const prepared = await prepareGrokInstallation(
    config,
    renderGrokDocument(document, hooks),
  );
`;

test('Grok restores its hook config and four runtime files after the final commit fails', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotFixture(fixture);
    const source = `${prepareInstallSource}
      import { rename } from 'node:fs/promises';
      import { commitGrokInstallation } from ${JSON.stringify(installationModuleUrl)};
      let calls = 0;
      await commitGrokInstallation(prepared, async (from, to) => {
        calls += 1;
        if (calls === 5) throw new Error('injected Grok config failure');
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
    assert.match(result.stderr, /injected Grok config failure/i);
    await assertSnapshot(before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Grok rejects concurrent config changes before committing runtime updates', async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await snapshotFixture(fixture);
    const concurrent = '{"owner":"concurrent"}\n';
    const source = `${prepareInstallSource}
      import { writeFile } from 'node:fs/promises';
      import { commitGrokInstallation } from ${JSON.stringify(installationModuleUrl)};
      await writeFile(document.filePath, process.env.ELYDORA_CONCURRENT, 'utf-8');
      await commitGrokInstallation(prepared);
    `;
    const result = await runFixtureSource(fixture, source, {
      ELYDORA_CONCURRENT: concurrent,
      ELYDORA_INSTALL_CONFIG: JSON.stringify(installConfig(fixture, { orgId: 'updated' })),
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /changed during Grok Build installation/i);
    for (const [filePath, contents] of before) {
      if (filePath !== fixture.configPath) {
        assert.equal(await readFile(filePath, 'utf-8'), contents, filePath);
      }
    }
    assert.equal(await readFile(fixture.configPath, 'utf-8'), concurrent);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});

test('Grok uninstall keeps the original hook config when commit fails', async () => {
  const fixture = await createFixture({ config: '{"owner":"user"}\n' });
  try {
    assert.equal((await fixture.install()).code, 0);
    const before = await readFile(fixture.configPath, 'utf-8');
    const source = `
      import {
        removeManagedGrokHooks,
        renderGrokDocument,
      } from ${JSON.stringify(contractModuleUrl)};
      import { readGrokDocument } from ${JSON.stringify(ioModuleUrl)};
      import {
        commitGrokUninstall,
        prepareGrokUninstall,
      } from ${JSON.stringify(installationModuleUrl)};
      const document = await readGrokDocument();
      const prepared = await prepareGrokUninstall(renderGrokDocument(
        document,
        removeManagedGrokHooks(document.hooks, 'agent-1'),
      ));
      await commitGrokUninstall(prepared, async () => {
        throw new Error('injected Grok uninstall failure');
      });
    `;
    const result = await runFixtureSource(fixture, source);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /injected Grok uninstall failure/i);
    assert.equal(await readFile(fixture.configPath, 'utf-8'), before);
    await assertNoTransactionFiles(fixture);
  } finally {
    await fixture.close();
  }
});
