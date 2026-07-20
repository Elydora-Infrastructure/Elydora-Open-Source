import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {
  assertMissing,
  configModuleUrl,
  createFixture,
} from '../test-support/droid-test-helpers.mjs';

test('Droid resolves hooksDisabled through official settings precedence', async (t) => {
  await t.test('user settings disable installation before runtime creation', async () => {
    const fixture = await createFixture({ settings: { hooksDisabled: true } });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /hooksDisabled/);
      await assertMissing(path.join(fixture.agentDir, 'config.json'));
      await assertMissing(fixture.rootPath);
    } finally {
      await fixture.close();
    }
  });

  await t.test('user local settings override their base file', async () => {
    const fixture = await createFixture({
      settings: { hooksDisabled: true },
      localSettings: { hooksDisabled: false },
    });
    try {
      assert.equal((await fixture.install()).code, 0);
    } finally {
      await fixture.close();
    }
  });

  await t.test('legacy direct flags remain safety-compatible', async () => {
    const fixture = await createFixture({
      legacyConfig: { hooksDisabled: true, PreToolUse: [] },
      settings: { hooksDisabled: false },
    });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, new RegExp(fixture.legacyPath.replaceAll('\\', '\\\\'), 'i'));
    } finally {
      await fixture.close();
    }
  });

  await t.test('project policy takes precedence over user settings', async () => {
    const fixture = await createFixture({
      settings: { hooksDisabled: false },
      projectSettings: { hooksDisabled: true },
    });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /project settings/);
      await assertMissing(path.join(fixture.agentDir, 'config.json'));
    } finally {
      await fixture.close();
    }
  });

  await t.test('project false locks out a lower user disable flag', async () => {
    const fixture = await createFixture({
      settings: { hooksDisabled: true },
      projectSettings: { hooksDisabled: false },
    });
    try {
      assert.equal((await fixture.install()).code, 0);
    } finally {
      await fixture.close();
    }
  });

  await t.test('project local settings override their base file', async () => {
    const fixture = await createFixture({
      projectSettings: { hooksDisabled: false },
      projectLocalSettings: { hooksDisabled: true },
    });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1);
      assert.match(result.stderr, /project local settings/);
    } finally {
      await fixture.close();
    }
  });
});

test('Droid treats allowManagedHooksOnly as an authoritative user-hook block', async () => {
  const { hookBlock } = await import(configModuleUrl);
  assert.deepEqual(hookBlock({
    policy: {
      allowManagedHooksOnlyBy: {
        filePath: '/managed/settings.json',
        label: 'Factory Droid system-managed settings',
      },
      preconditions: [],
    },
  }), {
    field: 'allowManagedHooksOnly',
    filePath: '/managed/settings.json',
    label: 'Factory Droid system-managed settings',
  });
});

test('Droid surfaces malformed read-only project policy before any write', async () => {
  const fixture = await createFixture({ projectSettings: '{ malformed' });
  try {
    const before = await readFile(fixture.projectSettingsPath, 'utf-8');
    const result = await fixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /project settings/);
    assert.equal(await readFile(fixture.projectSettingsPath, 'utf-8'), before);
    await assertMissing(path.join(fixture.agentDir, 'config.json'));
    await assertMissing(fixture.rootPath);
  } finally {
    await fixture.close();
  }
});
