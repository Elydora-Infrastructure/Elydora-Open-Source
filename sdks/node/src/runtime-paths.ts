import fsp from 'node:fs/promises';
import path from 'node:path';

export function isMissingPath(error: unknown): boolean {
  return error instanceof Error && 'code' in error && error.code === 'ENOENT';
}

export async function ensurePrivateDirectory(directory: string): Promise<void> {
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  const metadata = await fsp.lstat(directory);
  if (!metadata.isDirectory() || metadata.isSymbolicLink()) {
    throw new Error(`Private directory path is not a physical directory: ${directory}`);
  }
  if (process.platform !== 'win32') {
    await fsp.chmod(directory, 0o700);
  }
}

export async function requirePhysicalDirectory(directory: string): Promise<boolean> {
  let metadata;
  try {
    metadata = await fsp.lstat(directory);
  } catch (error) {
    if (isMissingPath(error)) return false;
    throw new Error(`Could not inspect agent runtime directory: ${directory}`, { cause: error });
  }
  if (!metadata.isDirectory() || metadata.isSymbolicLink()) {
    throw new Error(`Agent runtime path is not a physical directory: ${directory}`);
  }
  return true;
}

export async function requirePhysicalFile(filePath: string): Promise<boolean> {
  let metadata;
  try {
    metadata = await fsp.lstat(filePath);
  } catch (error) {
    if (isMissingPath(error)) return false;
    throw new Error(`Could not inspect agent runtime file: ${filePath}`, { cause: error });
  }
  if (!metadata.isFile() || metadata.isSymbolicLink()) {
    throw new Error(`Agent runtime config is not a physical file: ${filePath}`);
  }
  return true;
}

export function resolvePrivateChildDirectory(root: string, childName: string): string {
  const isWindowsDeviceName = /^(?:con|prn|aux|nul|com[1-9¹²³]|lpt[1-9¹²³])(?:\.|$)/i.test(childName);
  if (
    childName.length === 0
    || /[<>:"|?*\u0000-\u001f]/.test(childName)
    || childName.includes('/')
    || childName.includes('\\')
    || childName === '.'
    || childName === '..'
    || childName.startsWith(' ')
    || childName.endsWith('.')
    || childName.endsWith(' ')
    || isWindowsDeviceName
  ) {
    throw new Error(`Invalid agent ID for local storage: ${JSON.stringify(childName)}`);
  }

  const resolvedRoot = path.resolve(root);
  const candidate = path.resolve(resolvedRoot, childName);
  const relative = path.relative(resolvedRoot, candidate);
  if (
    relative.length === 0
    || path.isAbsolute(relative)
    || relative === '..'
    || relative.startsWith(`..${path.sep}`)
    || relative.includes(path.sep)
  ) {
    throw new Error(`Agent ID escapes the local storage directory: ${JSON.stringify(childName)}`);
  }
  return candidate;
}
