import crypto from 'node:crypto';
import fsp from 'node:fs/promises';
import path from 'node:path';
import { ensurePrivateDirectory, isMissingPath } from './runtime-paths.js';

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
        if (!isMissingPath(error)) throw error;
      }
    }
  }
}
