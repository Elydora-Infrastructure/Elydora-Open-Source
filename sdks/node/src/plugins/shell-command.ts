import path from 'node:path';
import { samePath } from './common.js';

export interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

export type CommandParts = readonly [executable: string, script: string];

const POSIX_APOSTROPHE = `'"'"'`;
const POWERSHELL_EXIT_SUFFIX = '; exit $LASTEXITCODE';
const ENCODED_COMMAND_FLAGS = '-NoLogo -NoProfile -NonInteractive -EncodedCommand';

export function quotePosix(value: string): string {
  return `'${value.replaceAll("'", POSIX_APOSTROPHE)}'`;
}

export function quotePowerShell(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

export function windowsPowerShellPath(): string {
  const configuredRoot = process.platform === 'win32' ? process.env.SystemRoot : undefined;
  const systemRoot = configuredRoot
    && path.win32.isAbsolute(configuredRoot)
    && !/["%\r\n]/.test(configuredRoot)
    ? configuredRoot
    : 'C:\\Windows';
  return path.win32.join(systemRoot, 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe');
}

export function posixSource(scriptPath: string): string {
  return `${quotePosix(process.execPath)} ${quotePosix(scriptPath)}`;
}

export function powerShellSource(scriptPath: string): string {
  return `& ${quotePowerShell(process.execPath)} ${quotePowerShell(scriptPath)}${POWERSHELL_EXIT_SUFFIX}`;
}

export function encodePowerShellSource(scriptPath: string): string {
  return Buffer.from(powerShellSource(scriptPath), 'utf16le').toString('base64');
}

export function encodedWindowsCommand(scriptPath: string): string {
  return `"${windowsPowerShellPath()}" ${ENCODED_COMMAND_FLAGS} ${encodePowerShellSource(scriptPath)}`;
}

// The running executable, or a binary named node (node.exe case-insensitive on Windows).
export function isNodeExecutable(filePath: string): boolean {
  if (samePath(filePath, process.execPath)) return true;
  const basename = path.basename(filePath);
  return basename === 'node' || basename.toLowerCase() === 'node.exe';
}

export function isPowerShellExecutable(filePath: string): boolean {
  return path.win32.isAbsolute(filePath)
    && path.win32.basename(filePath).toLowerCase() === 'powershell.exe';
}

export function readPosixArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== "'") return undefined;
  let value = '';
  for (let index = start + 1; index < command.length;) {
    if (command.startsWith(POSIX_APOSTROPHE, index)) {
      value += "'";
      index += POSIX_APOSTROPHE.length;
      continue;
    }
    if (command[index] === "'") return { value, next: index + 1 };
    value += command[index];
    index += 1;
  }
  return undefined;
}

export function parsePosixCommand(command: string): CommandParts | undefined {
  const executable = readPosixArgument(command, 0);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readPosixArgument(command, executable.next + 1);
  if (!script || script.next !== command.length) return undefined;
  return [executable.value, script.value];
}

function readPowerShellArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== "'") return undefined;
  let value = '';
  for (let index = start + 1; index < command.length; index += 1) {
    if (command[index] !== "'") {
      value += command[index];
      continue;
    }
    if (command[index + 1] === "'") {
      value += "'";
      index += 1;
      continue;
    }
    return { value, next: index + 1 };
  }
  return undefined;
}

export function parsePowerShellSource(source: string): CommandParts | undefined {
  if (!source.startsWith('& ')) return undefined;
  const executable = readPowerShellArgument(source, 2);
  if (!executable || source[executable.next] !== ' ') return undefined;
  const script = readPowerShellArgument(source, executable.next + 1);
  if (!script || source.slice(script.next) !== POWERSHELL_EXIT_SUFFIX) return undefined;
  return [executable.value, script.value];
}

export function decodePowerShellSource(encoded: string): CommandParts | undefined {
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(encoded)) return undefined;
  const buffer = Buffer.from(encoded, 'base64');
  if (buffer.toString('base64') !== encoded || buffer.length % 2 !== 0) return undefined;
  return parsePowerShellSource(buffer.toString('utf16le'));
}

export function parseEncodedWindowsCommand(command: string): CommandParts | undefined {
  const match = /^"([^"\r\n]+)" -NoLogo -NoProfile -NonInteractive -EncodedCommand ([A-Za-z0-9+/]+={0,2})$/.exec(command);
  if (!match || !isPowerShellExecutable(match[1])) return undefined;
  return decodePowerShellSource(match[2]);
}

export function parseLegacyWindowsCommand(command: string): CommandParts | undefined {
  const match = /^"([^"\r\n]+)" "([^"\r\n]+)"$/.exec(command);
  return match ? [match[1], match[2]] : undefined;
}
