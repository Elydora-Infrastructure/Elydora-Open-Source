export const PROTECTED_RUNTIME_READER: string = `const MAX_PROTECTED_SECRET_BYTES = 64 * 1024;
const MAX_PROTECTED_CONFIG_BYTES = 512 * 1024;

function readProtectedFile(filePath, label, maxBytes = MAX_PROTECTED_SECRET_BYTES) {
  const before = fs.lstatSync(filePath);
  if (!before.isFile() || before.isSymbolicLink()) {
    throw new Error(label + ' path is not a physical file: ' + filePath);
  }
  if (before.size > maxBytes) {
    throw new Error(label + ' exceeds the size limit: ' + filePath);
  }
  if (process.platform !== 'win32' && (before.mode & 0o077) !== 0) {
    throw new Error(label + ' permissions are too broad: ' + filePath);
  }

  let flags = fs.constants.O_RDONLY;
  if (process.platform !== 'win32' && typeof fs.constants.O_NOFOLLOW === 'number') {
    flags |= fs.constants.O_NOFOLLOW;
  }
  const descriptor = fs.openSync(filePath, flags);
  try {
    const after = fs.fstatSync(descriptor);
    if (!after.isFile()) {
      throw new Error(label + ' path is not a physical file: ' + filePath);
    }
    if (after.size > maxBytes) {
      throw new Error(label + ' exceeds the size limit: ' + filePath);
    }
    if (process.platform !== 'win32' && (after.mode & 0o077) !== 0) {
      throw new Error(label + ' permissions are too broad: ' + filePath);
    }
    if (before.dev !== after.dev || before.ino !== after.ino) {
      throw new Error(label + ' changed while opening: ' + filePath);
    }
    const raw = fs.readFileSync(descriptor);
    if (raw.length > maxBytes) {
      throw new Error(label + ' exceeds the size limit: ' + filePath);
    }
    return raw;
  } finally {
    fs.closeSync(descriptor);
  }
}
`;
