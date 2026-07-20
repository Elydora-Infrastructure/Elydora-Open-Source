import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createFixture,
  runProcess,
} from '../test-support/claudecode-test-helpers.mjs';

const claudeBinary = process.env.ELYDORA_CLAUDE_BINARY;

test('official Claude Code accepts the installed user hook contract', {
  skip: claudeBinary ? false : 'set ELYDORA_CLAUDE_BINARY to the official Claude Code executable',
}, async () => {
  const fixture = await createFixture({
    settings: {
      hooks: {
        Stop: [{
          hooks: [{
            type: 'command',
            command: process.execPath,
            args: ['--version'],
            asyncRewake: true,
            rewakeMessage: 'Background validation failed',
            rewakeSummary: 'Validation feedback',
          }],
        }],
      },
    },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    const environment = {
      HOME: fixture.homeDir,
      USERPROFILE: fixture.homeDir,
      CLAUDE_CONFIG_DIR: fixture.claudeConfigDir,
      DISABLE_AUTOUPDATER: '1',
      DISABLE_TELEMETRY: '1',
    };
    const version = await runProcess(
      claudeBinary,
      ['--version'],
      environment,
      fixture.projectDir,
    );
    assert.equal(version.code, 0, version.stderr);
    assert.match(version.stdout, /Claude Code/i);

    const doctor = await runProcess(
      claudeBinary,
      ['doctor'],
      environment,
      fixture.projectDir,
    );
    assert.equal(doctor.code, 0, doctor.stderr);
    const output = `${doctor.stdout}\n${doctor.stderr}`;
    assert.doesNotMatch(output, /invalid settings|settings validation|failed to parse/i);
  } finally {
    await fixture.close();
  }
});
