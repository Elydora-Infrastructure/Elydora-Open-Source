import crypto from 'node:crypto';
import fsp from 'node:fs/promises';
import path from 'node:path';

function isMissingFile(error: unknown): boolean {
  return error instanceof Error && 'code' in error && error.code === 'ENOENT';
}

export async function ensurePrivateDirectory(directory: string): Promise<void> {
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  if (process.platform !== 'win32') {
    await fsp.chmod(directory, 0o700);
  }
}

export async function writePrivateFile(filePath: string, contents: string): Promise<void> {
  const directory = path.dirname(filePath);
  await ensurePrivateDirectory(directory);

  const temporaryPath = path.join(
    directory,
    `.${path.basename(filePath)}.${crypto.randomUUID()}.tmp`,
  );
  let committed = false;
  try {
    const handle = await fsp.open(temporaryPath, 'wx', 0o600);
    try {
      await handle.writeFile(contents, 'utf-8');
      await handle.sync();
    } finally {
      await handle.close();
    }

    await fsp.rename(temporaryPath, filePath);
    committed = true;
    if (process.platform !== 'win32') {
      await fsp.chmod(filePath, 0o600);
    }
  } finally {
    if (!committed) {
      try {
        await fsp.unlink(temporaryPath);
      } catch (error) {
        if (!isMissingFile(error)) throw error;
      }
    }
  }
}
