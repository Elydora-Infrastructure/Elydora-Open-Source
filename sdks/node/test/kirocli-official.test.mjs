import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createFixture,
  runProcess,
} from '../test-support/kirocli-test-helpers.mjs';

test('official Kiro CLI 2.13 validates the installed v2 agent contract', {
  skip: process.env.ELYDORA_KIRO_CLI_BINARY
    ? false
    : 'set ELYDORA_KIRO_CLI_BINARY to the official Kiro CLI executable',
}, async () => {
  const fixture = await createFixture();
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const binary = process.env.ELYDORA_KIRO_CLI_BINARY;
    const version = await runProcess(binary, ['--version'], {}, fixture.projectDir);
    assert.equal(version.code, 0, version.stderr);
    assert.match(version.stdout, /\b2\.13\.\d+\b/);
    const validation = await runProcess(
      binary,
      ['agent', 'validate', '--path', fixture.v2Path],
      {},
      fixture.projectDir,
    );
    assert.equal(validation.code, 0, validation.stderr);
  } finally {
    await fixture.close();
  }
});
