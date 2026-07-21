import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createFixture,
  runProcess,
} from '../test-support/grok-test-helpers.mjs';

const grokBinary = process.env.ELYDORA_GROK_BINARY;

test('official Grok Build 0.2.106 discovers the installed matchless hook contract', {
  skip: grokBinary ? false : 'set ELYDORA_GROK_BINARY to the official Grok Build executable',
}, async () => {
  const fixture = await createFixture();
  try {
    const install = await fixture.install();
    assert.equal(install.code, 0, install.stderr);
    const environment = {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      GROK_HOME: fixture.grokHome,
    };
    const version = await runProcess(
      grokBinary,
      ['--version'],
      environment,
      fixture.projectDir,
    );
    assert.equal(version.code, 0, version.stderr);
    assert.match(version.stdout, /grok 0\.2\.106\b/);

    const inspect = await runProcess(
      grokBinary,
      ['inspect', '--json'],
      environment,
      fixture.projectDir,
    );
    assert.equal(inspect.code, 0, inspect.stderr);
    const report = JSON.parse(inspect.stdout);
    assert.equal(report.grokVersion, '0.2.106');
    assert.deepEqual(
      report.hooks
        .map(({ event, hookType, matcher }) => ({ event, hookType, matcher }))
        .sort((left, right) => left.event.localeCompare(right.event)),
      [
        { event: 'PostToolUse', hookType: 'command', matcher: null },
        { event: 'PostToolUseFailure', hookType: 'command', matcher: null },
        { event: 'PreToolUse', hookType: 'command', matcher: null },
      ],
    );
  } finally {
    await fixture.close();
  }
});
