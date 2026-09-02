import os from 'node:os';
import path from 'node:path';
import type { HookScriptOptions } from './hook-template.js';
import { samePath } from './managed-installation.js';
import { parseStrictJsonObject } from './strict-json.js';

export const AGENT_KEY = 'opencode';
export const AUDIT_SCRIPT = 'hook.js';
export const GUARD_SCRIPT = 'guard.js';
export const PLUGIN_FILE = 'elydora-audit.js';
export const LEGACY_PLUGIN_FILE = 'elydora-audit.mjs';
export const OPENCODE_AUDIT_OPTIONS = Object.freeze({
  failClosed: true,
  nativePayload: true,
}) satisfies HookScriptOptions;

const METADATA_MARKER = '// @elydora-opencode-plugin ';
const METADATA_VERSION = 1;
const MAX_RUNTIME_STDERR_BYTES = 64 * 1024;
const GUARD_TIMEOUT_MILLISECONDS = 5_000;
const AUDIT_TIMEOUT_MILLISECONDS = 7_000;

export interface OpenCodePaths {
  readonly homeDirectory: string;
  readonly configRoot: string;
  readonly configDirectory: string;
  readonly pluginsDirectory: string;
  readonly pluginPath: string;
  readonly legacyPluginPath: string;
}

export interface OpenCodeRuntimeContract {
  readonly agentId: string;
  readonly executablePath: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface OpenCodeLegacyContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

interface OpenCodePluginMetadata {
  readonly version: 1;
  readonly agentName: 'opencode';
  readonly agentId: string;
  readonly executablePath: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export function sameOpenCodeAgentId(left: string, right: string): boolean {
  return process.platform === 'win32' ? left.toLowerCase() === right.toLowerCase() : left === right;
}

export function resolveOpenCodePaths(
  homeDirectory = os.homedir(),
  xdgConfigHome = process.env.XDG_CONFIG_HOME,
): OpenCodePaths {
  const resolvedHome = path.resolve(homeDirectory);
  const configRoot = xdgConfigHome || path.join(resolvedHome, '.config');
  if (!path.isAbsolute(configRoot)) {
    throw new Error('XDG_CONFIG_HOME must be an absolute path for OpenCode integration');
  }
  const configDirectory = path.join(configRoot, 'opencode');
  const pluginsDirectory = path.join(configDirectory, 'plugins');
  return {
    homeDirectory: resolvedHome,
    configRoot: path.normalize(configRoot),
    configDirectory,
    pluginsDirectory,
    pluginPath: path.join(pluginsDirectory, PLUGIN_FILE),
    legacyPluginPath: path.join(pluginsDirectory, LEGACY_PLUGIN_FILE),
  };
}

function validAgentSegment(agentId: string): boolean {
  return Boolean(agentId)
    && agentId !== '.'
    && agentId !== '..'
    && path.basename(agentId) === agentId;
}

function isNodeExecutable(filePath: string): boolean {
  const basename = path.basename(filePath).toLowerCase();
  return basename === 'node' || basename === 'node.exe';
}

function validateRuntimePaths(metadata: OpenCodePluginMetadata): void {
  if (!validAgentSegment(metadata.agentId)) {
    throw new Error('OpenCode plugin metadata contains an invalid agentId');
  }
  if (!path.isAbsolute(metadata.executablePath) || !isNodeExecutable(metadata.executablePath)) {
    throw new Error('OpenCode plugin metadata executablePath must reference Node.js');
  }
  const agentDirectory = path.join(os.homedir(), '.elydora', metadata.agentId);
  if (!samePath(metadata.guardPath, path.join(agentDirectory, GUARD_SCRIPT))
    || !samePath(metadata.auditPath, path.join(agentDirectory, AUDIT_SCRIPT))) {
    throw new Error('OpenCode plugin metadata references an unexpected runtime path');
  }
}

function validateMetadata(value: unknown): OpenCodePluginMetadata {
  const object = value as Record<string, unknown>;
  if (!object || typeof object !== 'object' || Array.isArray(object)) {
    throw new Error('OpenCode plugin metadata must be an object');
  }
  if (Object.keys(object).sort().join(',') !== 'agentId,agentName,auditPath,executablePath,guardPath,version') {
    throw new Error('OpenCode plugin metadata contains an unexpected field set');
  }
  if (object.version !== METADATA_VERSION) throw new Error('OpenCode plugin metadata version must be 1');
  if (object.agentName !== AGENT_KEY) throw new Error('OpenCode plugin metadata agentName must be opencode');
  for (const field of ['agentId', 'executablePath', 'guardPath', 'auditPath']) {
    if (typeof object[field] !== 'string' || object[field].length === 0) {
      throw new Error(`OpenCode plugin metadata ${field} must be a non-empty string`);
    }
  }
  const metadata = object as unknown as OpenCodePluginMetadata;
  validateRuntimePaths(metadata);
  return metadata;
}

function encodeMetadata(metadata: OpenCodePluginMetadata): string {
  return Buffer.from(JSON.stringify(metadata), 'utf-8').toString('base64url');
}

export function buildOpenCodeMetadata(
  agentId: string,
  executablePath: string,
  guardPath: string,
  auditPath: string,
): OpenCodeRuntimeContract {
  return validateMetadata({
    version: METADATA_VERSION,
    agentName: AGENT_KEY,
    agentId,
    executablePath,
    guardPath,
    auditPath,
  });
}

function normalizeGeneratedSource(source: string): string {
  return source.replaceAll('\r\n', '\n');
}

export function buildOpenCodePlugin(contract: OpenCodeRuntimeContract): string {
  const metadata = validateMetadata({
    version: METADATA_VERSION,
    agentName: AGENT_KEY,
    ...contract,
  });
  return normalizeGeneratedSource(`// Elydora Audit Plugin for OpenCode
${METADATA_MARKER}${encodeMetadata(metadata)}
// Generated by @elydora/sdk. DO NOT EDIT.

import { spawn, spawnSync } from 'node:child_process';

const RUNTIME_EXECUTABLE = ${JSON.stringify(metadata.executablePath)};
const GUARD_SCRIPT_PATH = ${JSON.stringify(metadata.guardPath)};
const AUDIT_SCRIPT_PATH = ${JSON.stringify(metadata.auditPath)};
const MAX_STDERR_BYTES = ${MAX_RUNTIME_STDERR_BYTES};
const GUARD_TIMEOUT_MS = ${GUARD_TIMEOUT_MILLISECONDS};
const AUDIT_TIMEOUT_MS = ${AUDIT_TIMEOUT_MILLISECONDS};

function eventPayload(eventName, input, output) {
  return JSON.stringify({
    hook_event_name: eventName,
    tool_name: input?.tool || 'unknown',
    tool_input: eventName === 'tool.execute.before' ? output?.args : input?.args,
    session_id: input?.sessionID || 'unknown',
    call_id: input?.callID || 'unknown',
    input,
    output,
  });
}

function runGuard(input, output) {
  let payload;
  try {
    payload = eventPayload('tool.execute.before', input, output);
  } catch (error) {
    throw new Error('Elydora guard input serialization failed: ' + error.message);
  }
  const result = spawnSync(RUNTIME_EXECUTABLE, [GUARD_SCRIPT_PATH], {
    input: payload,
    encoding: 'utf-8',
    maxBuffer: MAX_STDERR_BYTES,
    timeout: GUARD_TIMEOUT_MS,
    windowsHide: true,
  });
  if (result.error) throw new Error('Elydora guard failed: ' + result.error.message);
  const stderr = String(result.stderr || '').trim();
  if (result.status !== 0) {
    const detail = stderr || (result.signal
      ? 'Guard terminated by signal ' + result.signal
      : 'Guard exited with status ' + result.status);
    throw new Error((result.status === 2 ? '' : 'Elydora guard failed: ') + detail);
  }
  if (stderr) console.error(stderr);
}

function runAudit(input, output) {
  let payload;
  try {
    payload = eventPayload('tool.execute.after', input, output);
  } catch (error) {
    console.error('[elydora] OpenCode audit input serialization failed:', error.message);
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const child = spawn(RUNTIME_EXECUTABLE, [AUDIT_SCRIPT_PATH], {
      stdio: ['pipe', 'ignore', 'pipe'],
      windowsHide: true,
    });
    const stderr = [];
    let stderrBytes = 0;
    let truncated = false;
    let settled = false;
    let timer;

    const report = (message) => {
      console.error('[elydora] OpenCode audit runtime failed:', message);
    };
    const complete = (failure) => {
      if (settled) return;
      settled = true;
      if (timer) clearTimeout(timer);
      if (failure) report(failure);
      resolve();
    };
    const stderrText = () => {
      const detail = Buffer.concat(stderr).toString('utf-8').trim();
      return truncated ? detail + '\\n[stderr truncated]' : detail;
    };

    child.stderr.on('data', (chunk) => {
      const value = Buffer.from(chunk);
      const remaining = MAX_STDERR_BYTES - stderrBytes;
      if (remaining <= 0) {
        truncated = true;
        return;
      }
      stderr.push(value.subarray(0, remaining));
      stderrBytes += Math.min(value.length, remaining);
      if (value.length > remaining) truncated = true;
    });
    child.once('error', (error) => complete(error.message));
    child.stdin.once('error', (error) => {
      child.kill();
      complete('failed to write hook input: ' + error.message);
    });
    child.once('close', (code, signal) => {
      const detail = stderrText();
      if (code !== 0 || signal) {
        complete(detail || (signal
          ? 'runtime terminated by signal ' + signal
          : 'runtime exited with code ' + code));
        return;
      }
      if (detail) report(detail);
      complete();
    });
    timer = setTimeout(() => {
      child.kill();
      complete('runtime timed out after ' + AUDIT_TIMEOUT_MS + 'ms');
    }, AUDIT_TIMEOUT_MS);
    try {
      child.stdin.end(payload);
    } catch (error) {
      child.kill();
      complete('failed to write hook input: ' + error.message);
    }
  });
}

export const ElydoraAuditPlugin = async () => ({
  'tool.execute.before': async (input, output) => {
    runGuard(input, output);
  },
  'tool.execute.after': async (input, output) => {
    await runAudit(input, output);
  },
});
`);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function parseOpenCodePlugin(
  filePath: string,
  source: string,
): OpenCodeRuntimeContract | undefined {
  const markerLine = source.split(/\r?\n/, 3)[1];
  if (!markerLine?.startsWith(METADATA_MARKER)) return undefined;
  let metadata: OpenCodePluginMetadata;
  try {
    const encoded = markerLine.slice(METADATA_MARKER.length);
    const decoded = Buffer.from(encoded, 'base64url').toString('utf-8');
    if (Buffer.from(decoded, 'utf-8').toString('base64url') !== encoded) {
      throw new Error('metadata encoding is not canonical base64url');
    }
    metadata = validateMetadata(parseStrictJsonObject(decoded, 'OpenCode plugin metadata'));
  } catch (error) {
    throw new Error(`Failed to parse Elydora OpenCode plugin metadata at ${filePath}: ${errorMessage(error)}`, {
      cause: error instanceof Error ? error : new Error(String(error)),
    });
  }
  if (source !== buildOpenCodePlugin(metadata)) {
    throw new Error(`Elydora OpenCode plugin at ${filePath} does not match the managed template`);
  }
  return metadata;
}

export function buildLegacyOpenCodePlugin(auditPath: string, guardPath: string): string {
  return normalizeGeneratedSource(`// Elydora Audit Plugin for OpenCode
// Generated by @elydora/sdk — DO NOT EDIT

import { spawn, spawnSync } from 'node:child_process';

const HOOK_SCRIPT_PATH = ${JSON.stringify(auditPath)};
const GUARD_SCRIPT_PATH = ${JSON.stringify(guardPath)};

export const ElydoraAuditPlugin = async (ctx) => {
  return {
    "tool.execute.before": async () => {
      // Guard — blocks tool if agent is frozen
      let result;
      try {
        result = spawnSync('node', [GUARD_SCRIPT_PATH], {
          timeout: 5000,
          stdio: ['pipe', 'ignore', 'pipe'],
        });
      } catch (error) {
        throw new Error('Elydora guard failed: ' + error.message);
      }

      if (result.error) {
        throw new Error('Elydora guard failed: ' + result.error.message);
      }
      if (result.status !== 0) {
        const message = result.stderr?.toString().trim() || 'Guard exited with status ' + result.status;
        const prefix = result.status === 2 ? '' : 'Elydora guard failed: ';
        throw new Error(prefix + message);
      }
    },

    "tool.execute.after": async (input, output) => {
      const data = JSON.stringify({
        tool_name: input?.tool || 'unknown',
        tool_input: input?.args || output?.args || {},
        session_id: input?.sessionID || ctx?.project?.name || 'unknown',
      });
      const child = spawn('node', [HOOK_SCRIPT_PATH], {
        detached: true,
        stdio: ['pipe', 'ignore', 'ignore'],
      });
      child.on('error', (error) => {
        console.error('[elydora] audit hook failed:', error.message);
      });
      child.stdin.on('error', (error) => {
        console.error('[elydora] audit input failed:', error.message);
      });
      child.stdin.end(data);
      child.unref();
    },
  };
};
`);
}

function legacyRuntimeContract(
  guardPath: string,
  auditPath: string,
): OpenCodeLegacyContract | undefined {
  if (!path.isAbsolute(guardPath) || !path.isAbsolute(auditPath)) return undefined;
  const agentDirectory = path.dirname(guardPath);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))
    || !samePath(auditPath, path.join(agentDirectory, AUDIT_SCRIPT))
    || !samePath(guardPath, path.join(agentDirectory, GUARD_SCRIPT))) return undefined;
  const agentId = path.basename(agentDirectory);
  return validAgentSegment(agentId) ? { agentId, guardPath, auditPath } : undefined;
}

export function parseLegacyOpenCodePlugin(source: string): OpenCodeLegacyContract | undefined {
  const normalized = normalizeGeneratedSource(source);
  const auditMatch = normalized.match(/^const HOOK_SCRIPT_PATH = ("(?:\\.|[^"\\])*");$/m);
  const guardMatch = normalized.match(/^const GUARD_SCRIPT_PATH = ("(?:\\.|[^"\\])*");$/m);
  if (!auditMatch || !guardMatch) return undefined;
  let auditPath: unknown;
  let guardPath: unknown;
  try {
    auditPath = JSON.parse(auditMatch[1]);
    guardPath = JSON.parse(guardMatch[1]);
  } catch {
    return undefined;
  }
  if (typeof auditPath !== 'string' || typeof guardPath !== 'string'
    || normalized !== buildLegacyOpenCodePlugin(auditPath, guardPath)) return undefined;
  return legacyRuntimeContract(guardPath, auditPath);
}
