package plugins

const protectedRuntimeFileReader = `const MAX_PROTECTED_SECRET_BYTES = 64 * 1024;
const MAX_PROTECTED_CONFIG_BYTES = 512 * 1024;
const MAX_PROTECTED_LOG_BYTES = 2 * 1024 * 1024;

function hasRuntimeErrorCode(error, code) {
  return error && typeof error === 'object' && error.code === code;
}

function validateProtectedMetadata(metadata, filePath, label, maxBytes) {
  if (!metadata.isFile() || metadata.isSymbolicLink()) {
    throw new Error(label + ' path is not a physical file: ' + filePath);
  }
  if (metadata.size > maxBytes) {
    throw new Error(label + ' exceeds the size limit: ' + filePath);
  }
  if (process.platform !== 'win32' && (metadata.mode & 0o077) !== 0) {
    throw new Error(label + ' permissions are too broad: ' + filePath);
  }
}

function inspectProtectedFile(filePath, label, maxBytes) {
  const metadata = fs.lstatSync(filePath);
  validateProtectedMetadata(metadata, filePath, label, maxBytes);
  return metadata;
}

function readProtectedFile(filePath, label, maxBytes = MAX_PROTECTED_SECRET_BYTES) {
  const before = inspectProtectedFile(filePath, label, maxBytes);
  let flags = fs.constants.O_RDONLY;
  if (process.platform !== 'win32' && typeof fs.constants.O_NOFOLLOW === 'number') {
    flags |= fs.constants.O_NOFOLLOW;
  }
  const descriptor = fs.openSync(filePath, flags);
  try {
    const after = fs.fstatSync(descriptor);
    validateProtectedMetadata(after, filePath, label, maxBytes);
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

function openProtectedAppend(filePath, label, maxBytes) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    let before;
    try {
      before = inspectProtectedFile(filePath, label, maxBytes);
    } catch (error) {
      if (!hasRuntimeErrorCode(error, 'ENOENT')) throw error;
      let createFlags = fs.constants.O_WRONLY | fs.constants.O_APPEND |
        fs.constants.O_CREAT | fs.constants.O_EXCL;
      if (process.platform !== 'win32' && typeof fs.constants.O_NOFOLLOW === 'number') {
        createFlags |= fs.constants.O_NOFOLLOW;
      }
      try {
        const descriptor = fs.openSync(filePath, createFlags, 0o600);
        validateProtectedMetadata(fs.fstatSync(descriptor), filePath, label, maxBytes);
        return descriptor;
      } catch (createError) {
        if (hasRuntimeErrorCode(createError, 'EEXIST')) continue;
        throw createError;
      }
    }

    let flags = fs.constants.O_WRONLY | fs.constants.O_APPEND;
    if (process.platform !== 'win32' && typeof fs.constants.O_NOFOLLOW === 'number') {
      flags |= fs.constants.O_NOFOLLOW;
    }
    const descriptor = fs.openSync(filePath, flags);
    try {
      const after = fs.fstatSync(descriptor);
      validateProtectedMetadata(after, filePath, label, maxBytes);
      if (before.dev !== after.dev || before.ino !== after.ino) {
        throw new Error(label + ' changed while opening: ' + filePath);
      }
      return descriptor;
    } catch (error) {
      fs.closeSync(descriptor);
      throw error;
    }
  }
  throw new Error(label + ' changed repeatedly while opening: ' + filePath);
}

function appendProtectedText(filePath, label, text, maxBytes = MAX_PROTECTED_LOG_BYTES) {
  const descriptor = openProtectedAppend(filePath, label, maxBytes);
  try {
    const metadata = fs.fstatSync(descriptor);
    const bytes = Buffer.byteLength(text, 'utf-8');
    if (metadata.size + bytes > maxBytes) {
      throw new Error(label + ' exceeds the size limit: ' + filePath);
    }
    fs.writeFileSync(descriptor, text, 'utf-8');
    fs.fsyncSync(descriptor);
    if (process.platform !== 'win32') fs.fchmodSync(descriptor, 0o600);
  } finally {
    fs.closeSync(descriptor);
  }
}

function writeProtectedJson(filePath, label, value) {
  const encoded = JSON.stringify(value);
  if (Buffer.byteLength(encoded, 'utf-8') > MAX_PROTECTED_CONFIG_BYTES) {
    throw new Error(label + ' exceeds the size limit: ' + filePath);
  }
  const temporaryPath = filePath + '.' + process.pid + '.' +
    crypto.randomBytes(8).toString('hex') + '.tmp';
  let descriptor;
  try {
    descriptor = fs.openSync(temporaryPath, 'wx', 0o600);
    fs.writeFileSync(descriptor, encoded, 'utf-8');
    fs.fsyncSync(descriptor);
    fs.closeSync(descriptor);
    descriptor = undefined;
    if (process.platform !== 'win32') fs.chmodSync(temporaryPath, 0o600);
    try {
      inspectProtectedFile(filePath, label, MAX_PROTECTED_CONFIG_BYTES);
    } catch (error) {
      if (!hasRuntimeErrorCode(error, 'ENOENT')) throw error;
    }
    try {
      fs.renameSync(temporaryPath, filePath);
    } catch (error) {
      if (process.platform !== 'win32' || !['EEXIST', 'EPERM'].includes(error.code)) throw error;
      inspectProtectedFile(filePath, label, MAX_PROTECTED_CONFIG_BYTES);
      fs.unlinkSync(filePath);
      fs.renameSync(temporaryPath, filePath);
    }
  } finally {
    if (descriptor !== undefined) fs.closeSync(descriptor);
    try { fs.unlinkSync(temporaryPath); } catch (error) {
      if (!hasRuntimeErrorCode(error, 'ENOENT')) throw error;
    }
  }
}
`
