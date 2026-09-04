import os from 'node:os';
import path from 'node:path';
import { samePath } from './common.js';
import {
  decodePowerShellSource,
  encodePowerShellSource,
  isNodeExecutable,
  isPowerShellExecutable,
  parsePosixCommand,
  parsePowerShellSource,
  posixSource,
  quotePowerShell,
  windowsPowerShellPath,
} from './shell-command.js';

export interface GeminiRuntimeReference {
  readonly agentId: string;
  readonly scriptPath: string;
}

function windowsCommand(scriptPath: string): string {
  return `& ${quotePowerShell(windowsPowerShellPath())} -NoLogo -NoProfile -NonInteractive -EncodedCommand ${encodePowerShellSource(scriptPath)}`;
}

export function buildGeminiCommand(scriptPath: string): string {
  if (!path.isAbsolute(process.execPath) || !path.isAbsolute(scriptPath)) {
    throw new Error('Gemini CLI hook commands require absolute executable and script paths');
  }
  return process.platform === 'win32'
    ? windowsCommand(scriptPath)
    : posixSource(scriptPath);
}

function parseWindowsCommand(command: string): readonly [string, string] | undefined {
  const match = /^& ('(?:[^']|'')+') -NoLogo -NoProfile -NonInteractive -EncodedCommand ([A-Za-z0-9+/]+={0,2})$/.exec(command);
  if (!match) return undefined;
  const powershell = parsePowerShellSource(`& ${match[1]} ''; exit $LASTEXITCODE`);
  if (!powershell || !isPowerShellExecutable(powershell[0])) return undefined;
  return decodePowerShellSource(match[2]);
}

function parseLegacyCommand(command: string): readonly [string, string] | undefined {
  const match = /^node "([^"\r\n]+)"$/.exec(command);
  return match ? ['node', match[1]] : undefined;
}

export function geminiRuntimeReference(
  command: string,
  scriptName: string,
  includeLegacy = false,
): GeminiRuntimeReference | undefined {
  const parsed = parsePosixCommand(command)
    ?? parseWindowsCommand(command)
    ?? (includeLegacy ? parseLegacyCommand(command) : undefined);
  if (!parsed
    || !isNodeExecutable(parsed[0])
    || !path.isAbsolute(parsed[1])
    || path.basename(parsed[1]) !== scriptName) return undefined;
  if (parsed[0] !== 'node' && !path.isAbsolute(parsed[0])) return undefined;
  const agentDirectory = path.dirname(parsed[1]);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) {
    return undefined;
  }
  const agentId = path.basename(agentDirectory);
  if (!agentId || agentId === '.' || agentId === '..') return undefined;
  return { agentId, scriptPath: parsed[1] };
}
