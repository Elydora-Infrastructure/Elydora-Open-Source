import fsp from 'node:fs/promises';
import readline from 'node:readline';
import { Writable } from 'node:stream';

const MAX_SECRET_FILE_BYTES = 64 * 1024;

interface SecretSources {
  readonly privateKeyFile?: string;
  readonly tokenFile?: string;
}

interface SecretTerminal {
  readonly interactive: boolean;
  readHidden(prompt: string): Promise<string>;
}

export interface InstallSecrets {
  readonly privateKey: string;
  readonly token?: string;
}

function parseSingleLineSecret(raw: string, label: string, allowEmpty: boolean): string {
  const value = raw.endsWith('\r\n')
    ? raw.slice(0, -2)
    : raw.endsWith('\n')
      ? raw.slice(0, -1)
      : raw;

  if (!allowEmpty && value.length === 0) {
    throw new Error(`${label} is empty`);
  }
  if (value.includes('\r') || value.includes('\n') || value.includes('\0')) {
    throw new Error(`${label} must contain exactly one line`);
  }
  return value;
}

async function readSecretFile(filePath: string, label: string): Promise<string> {
  const stats = await fsp.lstat(filePath);
  if (!stats.isFile()) {
    throw new Error(`${label} file is not a regular file: ${filePath}`);
  }
  if (stats.size > MAX_SECRET_FILE_BYTES) {
    throw new Error(`${label} file exceeds ${MAX_SECRET_FILE_BYTES} bytes: ${filePath}`);
  }
  if (process.platform !== 'win32' && (stats.mode & 0o077) !== 0) {
    throw new Error(`${label} file must be accessible only by its owner: ${filePath}`);
  }

  const raw = await fsp.readFile(filePath, 'utf-8');
  return parseSingleLineSecret(raw, label, false);
}

async function readHidden(prompt: string): Promise<string> {
  if (!process.stdin.isTTY || !process.stderr.isTTY) {
    throw new Error('hidden input requires an interactive terminal');
  }

  let muted = false;
  const output = new Writable({
    write(chunk, encoding, callback) {
      if (!muted) process.stderr.write(chunk, encoding);
      callback();
    },
  });
  const terminal = readline.createInterface({
    input: process.stdin,
    output,
    terminal: true,
    historySize: 0,
  });

  process.stderr.write(prompt);
  muted = true;
  try {
    return await new Promise<string>((resolve, reject) => {
      terminal.once('SIGINT', () => reject(new Error('secret input cancelled')));
      terminal.question('', resolve);
    });
  } finally {
    muted = false;
    terminal.close();
    process.stderr.write('\n');
  }
}

const defaultTerminal: SecretTerminal = {
  interactive: Boolean(process.stdin.isTTY && process.stderr.isTTY),
  readHidden,
};

export async function resolveInstallSecrets(
  sources: SecretSources,
  terminal: SecretTerminal = defaultTerminal,
): Promise<InstallSecrets> {
  let privateKey: string;
  if (sources.privateKeyFile) {
    privateKey = await readSecretFile(sources.privateKeyFile, 'private key');
  } else if (terminal.interactive) {
    privateKey = parseSingleLineSecret(
      await terminal.readHidden('Private key: '),
      'private key',
      false,
    );
  } else {
    throw new Error(
      'private key input requires an interactive terminal or --private_key_file <path>',
    );
  }

  let token: string | undefined;
  if (sources.tokenFile) {
    token = await readSecretFile(sources.tokenFile, 'API token');
  } else if (terminal.interactive) {
    token = parseSingleLineSecret(
      await terminal.readHidden('API token (optional): '),
      'API token',
      true,
    ) || undefined;
  }

  return { privateKey, token };
}
