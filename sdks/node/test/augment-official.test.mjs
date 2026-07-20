import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createFixture,
  runProcess,
} from '../test-support/augment-test-helpers.mjs';

const auggieEntry = process.env.ELYDORA_AUGGIE_ENTRY;

test('official Auggie accepts the installed user hook contract', {
  skip: auggieEntry ? false : 'set ELYDORA_AUGGIE_ENTRY to the official Auggie entry file',
}, async () => {
  const fixture = await createFixture();
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const environment = {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
    };
    const version = await runProcess(
      process.execPath,
      [auggieEntry, '--version'],
      environment,
      fixture.projectDir,
    );
    assert.equal(version.code, 0, version.stderr);
    assert.match(version.stdout, /0\.33\.0/);

    const load = await runProcess(
      process.execPath,
      [auggieEntry, 'tools', 'list'],
      environment,
      fixture.projectDir,
    );
    assert.equal(load.code, 0, load.stderr);
    assert.doesNotMatch(
      `${load.stdout}\n${load.stderr}`,
      /invalid settings|settings validation|failed to parse|hook configuration error/i,
    );
  } finally {
    await fixture.close();
  }
});
