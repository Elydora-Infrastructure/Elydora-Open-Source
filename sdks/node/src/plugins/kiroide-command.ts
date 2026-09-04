import path from 'node:path';
import { managedScriptReference, type ManagedScriptReference } from './common.js';
import {
  encodedWindowsCommand,
  isNodeExecutable,
  parseEncodedWindowsCommand,
  parsePosixCommand,
  posixSource,
} from './shell-command.js';

export type KiroIdeRuntimeReference = ManagedScriptReference;

export function buildKiroIdeCommand(scriptPath: string): string {
  if (!path.isAbsolute(process.execPath) || !path.isAbsolute(scriptPath)) {
    throw new Error('Kiro IDE hook commands require absolute executable and script paths');
  }
  return process.platform === 'win32' ? encodedWindowsCommand(scriptPath) : posixSource(scriptPath);
}

export function kiroIdeRuntimeReference(
  command: string,
  scriptName: string,
): KiroIdeRuntimeReference | undefined {
  const parsed = process.platform === 'win32'
    ? parseEncodedWindowsCommand(command)
    : parsePosixCommand(command);
  if (!parsed || !path.isAbsolute(parsed[0]) || !isNodeExecutable(parsed[0])) return undefined;
  return managedScriptReference(parsed[1], scriptName);
}
