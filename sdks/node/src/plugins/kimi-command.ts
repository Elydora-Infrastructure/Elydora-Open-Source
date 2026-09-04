import path from 'node:path';
import { managedScriptReference, type ManagedScriptReference } from './common.js';
import {
  encodedWindowsCommand,
  isNodeExecutable,
  parseEncodedWindowsCommand,
  parseLegacyWindowsCommand,
  parsePosixCommand,
  posixSource,
} from './shell-command.js';

export type KimiRuntimeReference = ManagedScriptReference;

export function buildKimiCommand(scriptPath: string): string {
  if (!path.isAbsolute(process.execPath) || !path.isAbsolute(scriptPath)) {
    throw new Error('Kimi hook commands require absolute executable and script paths');
  }
  return process.platform === 'win32' ? encodedWindowsCommand(scriptPath) : posixSource(scriptPath);
}

export function kimiRuntimeReference(
  command: string,
  scriptName: string,
): KimiRuntimeReference | undefined {
  const parsed = parsePosixCommand(command)
    ?? parseEncodedWindowsCommand(command)
    ?? parseLegacyWindowsCommand(command);
  if (!parsed || !path.isAbsolute(parsed[0]) || !isNodeExecutable(parsed[0])) return undefined;
  return managedScriptReference(parsed[1], scriptName);
}
