import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createFixture,
  environment,
  runNode,
} from '../test-support/cline-test-helpers.mjs';

const clineCoreEntry = process.env.ELYDORA_CLINE_CORE_ENTRY;
const clineEntry = process.env.ELYDORA_CLINE_ENTRY;

test('official Cline 3 loader discovers both managed file hooks', {
  skip: clineCoreEntry && clineEntry
    ? false
    : 'set ELYDORA_CLINE_CORE_ENTRY and ELYDORA_CLINE_ENTRY to official Cline files',
}, async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    const version = await runNode(
      [clineEntry, '--version'],
      environment(fixture),
      fixture.projectDir,
    );
    assert.equal(version.code, 0, version.stderr);
    assert.match(version.stdout, /^3\.0\.46\s*$/);

    const source = `
      import { pathToFileURL } from 'node:url';
      const { listHookConfigFiles } = await import(
        pathToFileURL(process.env.ELYDORA_CLINE_CORE_ENTRY).href
      );
      console.log(JSON.stringify(listHookConfigFiles(process.env.ELYDORA_WORKSPACE)));
    `;
    const load = await runNode(
      ['--input-type=module', '--eval', source],
      {
        ...environment(fixture),
        ELYDORA_CLINE_CORE_ENTRY: clineCoreEntry,
        ELYDORA_WORKSPACE: fixture.projectDir,
      },
      fixture.projectDir,
    );
    assert.equal(load.code, 0, load.stderr);
    assert.deepEqual(JSON.parse(load.stdout), [{
      fileName: 'PostToolUse',
      hookEventName: 'tool_result',
      path: fixture.auditWrapperPath,
    }, {
      fileName: 'PreToolUse',
      hookEventName: 'tool_call',
      path: fixture.guardWrapperPath,
    }]);
  } finally {
    await fixture.close();
  }
});
