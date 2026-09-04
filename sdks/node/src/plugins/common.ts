import os from 'node:os';
import path from 'node:path';

export const MAX_SECRET_BYTES = 64 * 1024;
export const MAX_CONFIG_BYTES = 512 * 1024;
export const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

export function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function hasCode(error: unknown, code: string): boolean {
  return typeof error === 'object' && error !== null && 'code' in error && error.code === code;
}

export function pathKey(filePath: string): string {
  const resolved = path.resolve(filePath);
  return process.platform === 'win32' ? resolved.toLowerCase() : resolved;
}

export function samePath(left: string, right: string): boolean {
  return pathKey(left) === pathKey(right);
}

export function sameAgentId(left: unknown, right: string): boolean {
  return typeof left === 'string' && (process.platform === 'win32'
    ? left.toLowerCase() === right.toLowerCase()
    : left === right);
}

export interface ManagedScriptReference {
  readonly agentId: string;
  readonly scriptPath: string;
}

// Accepts only ~/.elydora/<agentId>/<scriptName>.
export function managedScriptReference(
  scriptPath: string,
  scriptName: string,
): ManagedScriptReference | undefined {
  if (!path.isAbsolute(scriptPath) || path.basename(scriptPath) !== scriptName) return undefined;
  const agentDirectory = path.dirname(scriptPath);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) return undefined;
  const agentId = path.basename(agentDirectory);
  return agentId && agentId !== '.' && agentId !== '..' ? { agentId, scriptPath } : undefined;
}
