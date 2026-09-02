import assert from 'node:assert/strict';
import path from 'node:path';
import test from 'node:test';
import {
  createFixture,
  environment,
  runProcess,
} from '../test-support/opencode-test-helpers.mjs';

const binary = process.env.ELYDORA_OPENCODE_BINARY;

test(
  'official OpenCode 1.18 discovers the generated global JavaScript plugin',
  { skip: binary ? false : 'set ELYDORA_OPENCODE_BINARY to the official OpenCode executable' },
  async () => {
    const fixture = await createFixture();
    try {
      assert.equal((await fixture.install()).code, 0);
      const env = {
        ...environment(fixture),
        OPENCODE_DISABLE_AUTOUPDATE: 'true',
        OPENCODE_DISABLE_MODELS_FETCH: 'true',
      };
      const version = await runProcess(binary, ['--version'], env, fixture.projectDir);
      assert.equal(version.code, 0, version.stderr);
      assert.match(version.stdout, /^1\.18\./);
      const paths = await runProcess(binary, ['debug', 'paths'], env, fixture.projectDir);
      assert.equal(paths.code, 0, paths.stderr);
      assert.equal(paths.stdout.includes(path.join(fixture.configRoot, 'opencode')), true);
      const info = await runProcess(binary, ['debug', 'info'], env, fixture.projectDir);
      assert.equal(info.code, 0, info.stderr);
      assert.match(info.stdout, /elydora-audit\.js/);
      assert.doesNotMatch(info.stdout, /elydora-audit\.mjs/);
    } finally {
      await fixture.close();
    }
  },
);
