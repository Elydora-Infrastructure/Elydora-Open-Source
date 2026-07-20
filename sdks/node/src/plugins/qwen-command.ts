import os from 'node:os';
import path from 'node:path';

interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

export interface QwenRuntimeReference {
  readonly agentId: string;
  readonly scriptPath: string;
}

export function sameQwenPath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

export function sameQwenAgentId(left: string, right: string): boolean {
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

export function buildQwenCommand(scriptPath: string): string {
  if (!path.isAbsolute(process.execPath) || !path.isAbsolute(scriptPath)) {
    throw new Error('Qwen Code hook commands require absolute executable and script paths');
  }
  if (process.platform === 'win32') {
    return `& ${quotePowerShell(process.execPath)} ${quotePowerShell(scriptPath)}; exit $LASTEXITCODE`;
  }
  return `${quotePosix(process.execPath)} ${quotePosix(scriptPath)}`;
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

function parsePowerShellCommand(command: string): readonly [string, string] | undefined {
  if (!command.startsWith('& ')) return undefined;
  const executable = readPowerShellArgument(command, 2);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readPowerShellArgument(command, executable.next + 1);
  if (!script || command.slice(script.next) !== '; exit $LASTEXITCODE') return undefined;
  return [executable.value, script.value];
}

function isNodeExecutable(filePath: string): boolean {
  const basename = path.basename(filePath).toLowerCase();
  return basename === 'node' || basename === 'node.exe';
}

export function qwenRuntimeReference(
  command: string,
  scriptName: string,
): QwenRuntimeReference | undefined {
  const parsed = process.platform === 'win32'
    ? parsePowerShellCommand(command)
    : parsePosixCommand(command);
  if (!parsed
    || !path.isAbsolute(parsed[0])
    || !isNodeExecutable(parsed[0])
    || !path.isAbsolute(parsed[1])
    || path.basename(parsed[1]) !== scriptName) return undefined;
  const agentDirectory = path.dirname(parsed[1]);
  if (!sameQwenPath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) {
    return undefined;
  }
  const agentId = path.basename(agentDirectory);
  if (!agentId || agentId === '.' || agentId === '..') return undefined;
  return { agentId, scriptPath: parsed[1] };
}
