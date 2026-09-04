import path from 'node:path';
import { managedScriptReference } from './common.js';
import {
  isNodeExecutable,
  parsePosixCommand,
  parsePowerShellSource,
  posixSource,
  powerShellSource,
} from './shell-command.js';

export interface LettaRuntimeReference {
  readonly agentId: string;
  readonly executablePath?: string;
  readonly scriptPath: string;
}

export function buildLettaCommand(scriptPath: string): string {
  if (!path.isAbsolute(process.execPath) || !path.isAbsolute(scriptPath)) {
    throw new Error('Letta Code hook commands require absolute executable and script paths');
  }
  return process.platform === 'win32' ? powerShellSource(scriptPath) : posixSource(scriptPath);
}

function legacyScriptPath(command: string): string | undefined {
  if (!command.startsWith('node "') || !command.endsWith('"')) return undefined;
  const scriptPath = command.slice(6, -1);
  return scriptPath.includes('"') ? undefined : scriptPath;
}

export function lettaRuntimeReference(
  command: string,
  scriptName: string,
): LettaRuntimeReference | undefined {
  const parsed = process.platform === 'win32'
    ? parsePowerShellSource(command)
    : parsePosixCommand(command);
  if (!parsed || !path.isAbsolute(parsed[0]) || !isNodeExecutable(parsed[0])) return undefined;
  const reference = managedScriptReference(parsed[1], scriptName);
  return reference ? { ...reference, executablePath: parsed[0] } : undefined;
}

export function lettaLegacyRuntimeReference(
  command: string,
  scriptName: string,
): LettaRuntimeReference | undefined {
  const legacy = legacyScriptPath(command);
  return legacy ? managedScriptReference(legacy, scriptName) : undefined;
}
