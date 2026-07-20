import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createFixture,
  runProcess,
} from '../test-support/gemini-test-helpers.mjs';

const geminiEntry = process.env.ELYDORA_GEMINI_ENTRY;

test('official Gemini CLI accepts the installed user hook contract', {
  skip: geminiEntry ? false : 'set ELYDORA_GEMINI_ENTRY to the official Gemini CLI entry file',
}, async () => {
  const fixture = await createFixture();
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const environment = {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      GEMINI_CLI_HOME: fixture.geminiCliHome,
      GEMINI_API_KEY: 'official-loader-test-key',
      GEMINI_TELEMETRY_ENABLED: 'false',
      OTEL_SDK_DISABLED: 'true',
    };
    const version = await runProcess(
      process.execPath,
      [geminiEntry, '--version'],
      environment,
      fixture.projectDir,
    );
    assert.equal(version.code, 0, version.stderr);
    assert.match(version.stdout, /0\.51\.0/);

    const load = await runProcess(
      process.execPath,
      [geminiEntry, '--skip-trust', '--list-extensions'],
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
