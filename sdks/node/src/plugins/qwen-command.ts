import path from 'node:path';
import { managedScriptReference, type ManagedScriptReference } from './common.js';
import {
  isNodeExecutable,
  parsePosixCommand,
  parsePowerShellSource,
  posixSource,
  powerShellSource,
} from './shell-command.js';

export type QwenRuntimeReference = ManagedScriptReference;

export function buildQwenCommand(scriptPath: string): string {
  if (!path.isAbsolute(process.execPath) || !path.isAbsolute(scriptPath)) {
    throw new Error('Qwen Code hook commands require absolute executable and script paths');
  }
  return process.platform === 'win32' ? powerShellSource(scriptPath) : posixSource(scriptPath);
}

export function qwenRuntimeReference(
  command: string,
  scriptName: string,
): QwenRuntimeReference | undefined {
  const parsed = process.platform === 'win32'
    ? parsePowerShellSource(command)
    : parsePosixCommand(command);
  if (!parsed || !path.isAbsolute(parsed[0]) || !isNodeExecutable(parsed[0])) return undefined;
  return managedScriptReference(parsed[1], scriptName);
}
