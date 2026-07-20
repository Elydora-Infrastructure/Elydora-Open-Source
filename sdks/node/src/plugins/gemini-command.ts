import os from 'node:os';
import path from 'node:path';

interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

export interface GeminiRuntimeReference {
  readonly agentId: string;
  readonly scriptPath: string;
}

export function sameGeminiPath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

export function sameGeminiAgentId(left: string, right: string): boolean {
  return process.platform === 'win32'
    ? left.toLowerCase() === right.toLowerCase()
    : left === right;
}

function quotePosix(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quotePowerShell(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

function windowsPowerShellPath(): string {
  const configuredRoot = process.env.SystemRoot;
  const systemRoot = configuredRoot
    && path.win32.isAbsolute(configuredRoot)
    && !/["%\r\n]/.test(configuredRoot)
    ? configuredRoot
    : 'C:\\Windows';
  return path.win32.join(
    systemRoot,
    'System32',
    'WindowsPowerShell',
    'v1.0',
    'powershell.exe',
  );
}

function windowsCommand(scriptPath: string): string {
  const source = `& ${quotePowerShell(process.execPath)} ${quotePowerShell(scriptPath)}; exit $LASTEXITCODE`;
  const encoded = Buffer.from(source, 'utf16le').toString('base64');
  return `& ${quotePowerShell(windowsPowerShellPath())} -NoLogo -NoProfile -NonInteractive -EncodedCommand ${encoded}`;
}

export function buildGeminiCommand(scriptPath: string): string {
  if (!path.isAbsolute(process.execPath) || !path.isAbsolute(scriptPath)) {
    throw new Error('Gemini CLI hook commands require absolute executable and script paths');
  }
  return process.platform === 'win32'
    ? windowsCommand(scriptPath)
    : `${quotePosix(process.execPath)} ${quotePosix(scriptPath)}`;
}

function readPosixArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== "'") return undefined;
  const apostrophe = `'"'"'`;
  let value = '';
  for (let index = start + 1; index < command.length;) {
    if (command.startsWith(apostrophe, index)) {
      value += "'";
      index += apostrophe.length;
      continue;
    }
    if (command[index] === "'") return { value, next: index + 1 };
    value += command[index];
    index += 1;
  }
  return undefined;
}

function parsePosixCommand(command: string): readonly [string, string] | undefined {
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

function parsePowerShellSource(source: string): readonly [string, string] | undefined {
  if (!source.startsWith('& ')) return undefined;
  const executable = readPowerShellArgument(source, 2);
  if (!executable || source[executable.next] !== ' ') return undefined;
  const script = readPowerShellArgument(source, executable.next + 1);
  if (!script || source.slice(script.next) !== '; exit $LASTEXITCODE') return undefined;
  return [executable.value, script.value];
}

function parseWindowsCommand(command: string): readonly [string, string] | undefined {
  if (!command.startsWith('& ')) return undefined;
  const powershell = readPowerShellArgument(command, 2);
  if (!powershell
    || !path.win32.isAbsolute(powershell.value)
    || path.win32.basename(powershell.value).toLowerCase() !== 'powershell.exe') return undefined;
  const prefix = ' -NoLogo -NoProfile -NonInteractive -EncodedCommand ';
  if (!command.startsWith(prefix, powershell.next)) return undefined;
  const encoded = command.slice(powershell.next + prefix.length);
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(encoded)) return undefined;
  const buffer = Buffer.from(encoded, 'base64');
  if (buffer.length % 2 !== 0 || buffer.toString('base64') !== encoded) return undefined;
  return parsePowerShellSource(buffer.toString('utf16le'));
}

function parseLegacyCommand(command: string): readonly [string, string] | undefined {
  const match = /^node "([^"\r\n]+)"$/.exec(command);
  return match ? ['node', match[1]] : undefined;
}

function isNodeExecutable(filePath: string): boolean {
  const basename = path.basename(filePath).toLowerCase();
  return basename === 'node' || basename === 'node.exe';
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
  if (!sameGeminiPath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) {
    return undefined;
  }
  const agentId = path.basename(agentDirectory);
  if (!agentId || agentId === '.' || agentId === '..') return undefined;
  return { agentId, scriptPath: parsed[1] };
}
