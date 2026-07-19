import assert from 'node:assert/strict';
import { chmod, mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import test from 'node:test';

import { resolveInstallSecrets } from '../dist/cli-secrets.js';

const PRIVATE_KEY = Buffer.alloc(32, 7).toString('base64url');
const API_TOKEN = 'ely_test_token';

async function runCli(args, homeDir) {
  return await new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [path.resolve('dist/cli.js'), ...args], {
      env: {
        ...process.env,
        HOME: homeDir,
        USERPROFILE: homeDir,
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    const stdout = [];
    const stderr = [];
    child.stdout.on('data', (chunk) => stdout.push(chunk));
    child.stderr.on('data', (chunk) => stderr.push(chunk));
    child.once('error', reject);
    child.once('close', (code) => resolve({
      code,
      stdout: Buffer.concat(stdout).toString('utf-8'),
      stderr: Buffer.concat(stderr).toString('utf-8'),
    }));
  });
}

test('install secrets resolve from hidden prompts without process arguments', async () => {
  const answers = [PRIVATE_KEY, API_TOKEN];
  const prompts = [];
  const secrets = await resolveInstallSecrets({}, {
    interactive: true,
    async readHidden(prompt) {
      prompts.push(prompt);
      return answers.shift();
    },
  });

  assert.deepEqual(secrets, { privateKey: PRIVATE_KEY, token: API_TOKEN });
  assert.deepEqual(prompts, ['Private key: ', 'API token (optional): ']);
});

test('non-interactive install requires a credential file for the private key', async () => {
  await assert.rejects(
    resolveInstallSecrets({}, {
      interactive: false,
      async readHidden() {
        throw new Error('unexpected prompt');
      },
    }),
    /--private_key_file <path>/,
  );
});

test('credential files contain exactly one secret line', async () => {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-node-cli-'));
  const privateKeyFile = path.join(homeDir, 'private-key');
  try {
    await writeFile(privateKeyFile, `${PRIVATE_KEY}\nsecond-line\n`, { mode: 0o600 });
    if (process.platform !== 'win32') await chmod(privateKeyFile, 0o600);

    await assert.rejects(
      resolveInstallSecrets({ privateKeyFile }, {
        interactive: false,
        async readHidden() {
          throw new Error('unexpected prompt');
        },
      }),
      /private key must contain exactly one line/,
    );
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('CLI rejects legacy secret arguments before writing files', async () => {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-node-cli-'));
  try {
    const result = await runCli([
      'install',
      '--agent', 'opencode',
      '--org_id', 'org-1',
      '--agent_id', 'agent-1',
      '--kid', 'key-1',
      '--private_key', PRIVATE_KEY,
      '--token', API_TOKEN,
    ], homeDir);

    assert.equal(result.code, 1);
    assert.match(result.stderr, /Unknown option '--private_key'/);
    await assert.rejects(stat(path.join(homeDir, '.elydora')), { code: 'ENOENT' });
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});

test('CLI installs from owner-only files and protects persisted credentials', async () => {
  const homeDir = await mkdtemp(path.join(os.tmpdir(), 'elydora-node-cli-'));
  const privateKeyFile = path.join(homeDir, 'private-key');
  const tokenFile = path.join(homeDir, 'api-token');
  try {
    await writeFile(privateKeyFile, `${PRIVATE_KEY}\n`, { mode: 0o600 });
    await writeFile(tokenFile, `${API_TOKEN}\n`, { mode: 0o600 });
    if (process.platform !== 'win32') {
      await chmod(privateKeyFile, 0o600);
      await chmod(tokenFile, 0o600);
    }

    const result = await runCli([
      'install',
      '--agent', 'opencode',
      '--org_id', 'org-1',
      '--agent_id', 'agent-1',
      '--kid', 'key-1',
      '--private_key_file', privateKeyFile,
      '--token_file', tokenFile,
      '--base_url', 'https://api.example.test',
    ], homeDir);

    assert.equal(result.code, 0, result.stderr);
    assert.doesNotMatch(result.stdout + result.stderr, new RegExp(PRIVATE_KEY));
    assert.doesNotMatch(result.stdout + result.stderr, new RegExp(API_TOKEN));

    const agentDir = path.join(homeDir, '.elydora', 'agent-1');
    assert.equal(await readFile(path.join(agentDir, 'private.key'), 'utf-8'), PRIVATE_KEY);
    const config = JSON.parse(await readFile(path.join(agentDir, 'config.json'), 'utf-8'));
    assert.equal(config.token, API_TOKEN);

    const rotatedToken = `${API_TOKEN}_rotated`;
    await writeFile(tokenFile, `${rotatedToken}\n`);
    const reinstall = await runCli([
      'install',
      '--agent', 'opencode',
      '--org_id', 'org-1',
      '--agent_id', 'agent-1',
      '--kid', 'key-1',
      '--private_key_file', privateKeyFile,
      '--token_file', tokenFile,
      '--base_url', 'https://api.example.test',
    ], homeDir);
    assert.equal(reinstall.code, 0, reinstall.stderr);
    assert.doesNotMatch(reinstall.stdout + reinstall.stderr, new RegExp(rotatedToken));
    const rotatedConfig = JSON.parse(
      await readFile(path.join(agentDir, 'config.json'), 'utf-8'),
    );
    assert.equal(rotatedConfig.token, rotatedToken);

    if (process.platform !== 'win32') {
      assert.equal((await stat(agentDir)).mode & 0o777, 0o700);
      assert.equal((await stat(path.join(agentDir, 'private.key'))).mode & 0o777, 0o600);
      assert.equal((await stat(path.join(agentDir, 'config.json'))).mode & 0o777, 0o600);
    }
  } finally {
    await rm(homeDir, { recursive: true, force: true });
  }
});
