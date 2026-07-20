import assert from 'node:assert/strict';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  environment,
  runNode,
} from '../test-support/copilot-test-helpers.mjs';

const copilotEntry = process.env.ELYDORA_COPILOT_ENTRY;
const copilotRuntimeEntry = process.env.ELYDORA_COPILOT_RUNTIME_ENTRY;

test('official GitHub Copilot CLI 1.0.71 loads all three managed hooks', {
  skip: copilotEntry && copilotRuntimeEntry
    ? false
    : 'set ELYDORA_COPILOT_ENTRY and ELYDORA_COPILOT_RUNTIME_ENTRY to official package files',
}, async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const version = await runNode(
      [copilotEntry, '--version'],
      environment(fixture),
      fixture.projectDir,
    );
    assert.equal(version.code, 0, version.stderr);
    assert.match(version.stdout, /^GitHub Copilot CLI 1\.0\.71\.$/m);

    const source = `
      import { createRequire } from 'node:module';
      const require = createRequire(import.meta.url);
      const runtime = require(process.env.ELYDORA_COPILOT_RUNTIME_ENTRY);
      const session = await runtime.hookSessionCreate({
        cwd: process.env.ELYDORA_PROJECT,
        repoRoot: process.env.ELYDORA_PROJECT,
        sessionId: 'elydora-official-test',
        settingsJson: '{}',
        userHooksDir: process.env.ELYDORA_HOOKS,
        allowLocalhost: false,
        allowHttpAuthHooks: false,
        discoverPolicies: false,
      });
      try {
        const snapshot = JSON.parse(await runtime.hookSessionSnapshot(session.handle));
        console.log(JSON.stringify({ load: session.load, snapshot }));
      } finally {
        runtime.hookSessionDispose(session.handle);
      }
    `;
    const load = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...environment(fixture),
        ELYDORA_COPILOT_RUNTIME_ENTRY: copilotRuntimeEntry,
        ELYDORA_HOOKS: fixture.hooksDir,
        ELYDORA_PROJECT: fixture.projectDir,
      },
      fixture.projectDir,
    );
    assert.equal(load.code, 0, load.stderr);
    const result = JSON.parse(load.stdout);
    assert.equal(result.load.hookCount, 3);
    assert.deepEqual(result.load.errors, []);
    assert.deepEqual(result.load.warnings, []);
    assert.deepEqual(
      result.snapshot.hooks.map((hook) => hook.eventName).sort(),
      ['postToolUse', 'postToolUseFailure', 'preToolUse'],
    );
    for (const hook of result.snapshot.hooks) {
      assert.match(hook.source, /hooks[\\/]elydora-audit\.json$/);
      const spec = JSON.parse(hook.specJson);
      assert.equal(spec.config.type, 'command');
      assert.equal(spec.config.timeoutSec, 10);
      assert.match(spec.config.bash, /node(?:\.exe)?/i);
      assert.match(spec.config.powershell, /^& /);
      assert.equal(path.basename(spec.source), 'elydora-audit.json');
    }
  } finally {
    await fixture.close();
  }
});
