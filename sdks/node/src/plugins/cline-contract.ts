import os from 'node:os';
import path from 'node:path';

export const AGENT_KEY = 'cline';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const GUARD_FILE_NAME = 'PreToolUse.mjs';
export const AUDIT_FILE_NAME = 'PostToolUse.mjs';

const METADATA_MARKER = '// @elydora-cline-hook ';
const METADATA_VERSION = 1;

export type ClineHookKind = 'guard' | 'audit';

export interface ClineHookMetadata {
  readonly version: 1;
  readonly kind: ClineHookKind;
  readonly agentId: string;
  readonly runtimePath: string;
}

export interface ClineHookFile {
  readonly exists: boolean;
  readonly filePath: string;
  readonly source?: string;
  readonly metadata?: ClineHookMetadata;
}

export interface ClineRuntimeContract {
  readonly agentId: string;
  readonly agentDirectory: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

type JsonObject = Record<string, unknown>;

function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

export function sameAgentId(left: string, right: string): boolean {
  return process.platform === 'win32' ? left.toLowerCase() === right.toLowerCase() : left === right;
}

export function resolveHooksDirectory(): string {
  const configuredDirectory = process.env.CLINE_DIR?.trim();
  const clineDirectory = configuredDirectory
    ? path.resolve(configuredDirectory)
    : path.join(os.homedir(), '.cline');
  return path.join(clineDirectory, 'hooks');
}

export function resolveHookFiles(): {
  readonly hooksDirectory: string;
  readonly guardPath: string;
  readonly auditPath: string;
} {
  const hooksDirectory = resolveHooksDirectory();
  return {
    hooksDirectory,
    guardPath: path.join(hooksDirectory, GUARD_FILE_NAME),
    auditPath: path.join(hooksDirectory, AUDIT_FILE_NAME),
  };
}

export function buildMetadata(
  kind: ClineHookKind,
  agentId: string,
  runtimePath: string,
): ClineHookMetadata {
  if (!agentId) throw new Error('agentId is required');
  if (!path.isAbsolute(runtimePath)) throw new Error(`Cline ${kind} runtime path must be absolute`);
  return { version: METADATA_VERSION, kind, agentId, runtimePath };
}

function encodeMetadata(metadata: ClineHookMetadata): string {
  return Buffer.from(JSON.stringify(metadata), 'utf-8').toString('base64url');
}

export function buildWrapper(metadata: ClineHookMetadata): string {
  return `#!/usr/bin/env node
${METADATA_MARKER}${encodeMetadata(metadata)}
import { spawn } from 'node:child_process';

const hookKind = ${JSON.stringify(metadata.kind)};
const runtimePath = ${JSON.stringify(metadata.runtimePath)};

function runRuntime(input) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [runtimePath], {
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    });
    const stdout = [];
    const stderr = [];
    let stdinError;
    child.stdout.on('data', (chunk) => stdout.push(chunk));
    child.stderr.on('data', (chunk) => stderr.push(chunk));
    child.stdin.on('error', (error) => {
      if (error?.code !== 'EPIPE') stdinError = error;
    });
    child.once('error', reject);
    child.once('close', (code, signal) => {
      if (stdinError) {
        reject(stdinError);
        return;
      }
      resolve({
        code,
        signal,
        stdout: Buffer.concat(stdout).toString('utf-8'),
        stderr: Buffer.concat(stderr).toString('utf-8'),
      });
    });
    child.stdin.end(input);
  });
}

async function main() {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  const result = await runRuntime(Buffer.concat(chunks));
  if (result.stdout) process.stderr.write(result.stdout);
  if (result.stderr) process.stderr.write(result.stderr);

  if (hookKind === 'guard' && result.code === 2) {
    const errorMessage = result.stderr.trim() || 'Agent is frozen by Elydora.';
    process.stdout.write(JSON.stringify({ cancel: true, errorMessage }) + '\\n');
    return;
  }
  if (result.signal) throw new Error('runtime terminated by signal ' + result.signal);
  if (result.code !== 0) throw new Error('runtime exited with code ' + result.code);
}

main().catch((error) => {
  const message = error instanceof Error ? error.stack || error.message : String(error);
  process.stderr.write('Elydora Cline ' + hookKind + ' wrapper failed: ' + message + '\\n');
  process.exitCode = 1;
});
`;
}

function validateMetadata(value: unknown): ClineHookMetadata {
  if (!isObject(value)) throw new Error('metadata must be an object');
  const keys = Object.keys(value).sort();
  if (keys.join(',') !== 'agentId,kind,runtimePath,version') {
    throw new Error('metadata contains an unexpected field set');
  }
  if (value.version !== METADATA_VERSION) throw new Error('metadata version must be 1');
  if (value.kind !== 'guard' && value.kind !== 'audit') {
    throw new Error('metadata kind must be "guard" or "audit"');
  }
  if (typeof value.agentId !== 'string' || value.agentId.length === 0) {
    throw new Error('metadata agentId must be a non-empty string');
  }
  if (typeof value.runtimePath !== 'string' || !path.isAbsolute(value.runtimePath)) {
    throw new Error('metadata runtimePath must be an absolute path');
  }
  return value as unknown as ClineHookMetadata;
}

export function parseMetadata(filePath: string, source: string): ClineHookMetadata | undefined {
  const markerLine = source.split(/\r?\n/, 3)[1];
  if (!markerLine?.startsWith(METADATA_MARKER)) return undefined;
  try {
    const encoded = markerLine.slice(METADATA_MARKER.length);
    const decoded = Buffer.from(encoded, 'base64url').toString('utf-8');
    return validateMetadata(JSON.parse(decoded));
  } catch (error) {
    throw new Error(`Failed to parse Elydora Cline hook metadata at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

export function assertWrapperIntegrity(file: ClineHookFile): void {
  if (!file.metadata || file.source === undefined) return;
  if (file.source !== buildWrapper(file.metadata)) {
    throw new Error(`Elydora Cline hook at ${file.filePath} does not match the managed template`);
  }
}

function validateAgentSegment(agentId: string): void {
  if (agentId === '.' || agentId === '..' || path.basename(agentId) !== agentId) {
    throw new Error('Elydora Cline hook metadata contains an invalid agentId');
  }
}

export function runtimeContract(
  guardFile: ClineHookFile,
  auditFile: ClineHookFile,
): ClineRuntimeContract | undefined {
  if (!guardFile.metadata || !auditFile.metadata) return undefined;
  assertWrapperIntegrity(guardFile);
  assertWrapperIntegrity(auditFile);
  const guard = guardFile.metadata;
  const audit = auditFile.metadata;
  if (guard.kind !== 'guard' || audit.kind !== 'audit') {
    throw new Error('Elydora Cline hook files contain mismatched event metadata');
  }
  if (!sameAgentId(guard.agentId, audit.agentId)) {
    throw new Error('Elydora Cline hook files reference different agents');
  }
  validateAgentSegment(guard.agentId);
  const agentDirectory = path.join(os.homedir(), '.elydora', guard.agentId);
  const expectedGuard = path.join(agentDirectory, GUARD_SCRIPT);
  const expectedAudit = path.join(agentDirectory, AUDIT_SCRIPT);
  if (!samePath(guard.runtimePath, expectedGuard) || !samePath(audit.runtimePath, expectedAudit)) {
    throw new Error('Elydora Cline hook metadata references an unexpected runtime path');
  }
  return {
    agentId: guard.agentId,
    agentDirectory,
    guardPath: expectedGuard,
    auditPath: expectedAudit,
  };
}
