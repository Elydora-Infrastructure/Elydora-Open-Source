import { isObject, type JsonObject } from './strict-json.js';

export type CopilotHooks = Record<string, JsonObject[]>;

const SUPPORTED_EVENTS = new Set([
  'agentStop',
  'Stop',
  'errorOccurred',
  'ErrorOccurred',
  'notification',
  'Notification',
  'permissionRequest',
  'PermissionRequest',
  'postToolUse',
  'PostToolUse',
  'postToolUseFailure',
  'PostToolUseFailure',
  'preCompact',
  'PreCompact',
  'preToolUse',
  'PreToolUse',
  'sessionEnd',
  'SessionEnd',
  'sessionStart',
  'SessionStart',
  'subagentStart',
  'SubagentStart',
  'subagentStop',
  'SubagentStop',
  'userPromptSubmitted',
  'UserPromptSubmit',
  'userPromptTransformed',
]);

function fieldLabel(label: string, field: string): string {
  return `${label} field "${field}"`;
}

function requireString(value: unknown, label: string): string {
  if (typeof value !== 'string' || value.length === 0) {
    throw new Error(`${label} must be a non-empty string`);
  }
  return value;
}

function validateOptionalString(value: unknown, label: string): void {
  if (value !== undefined && typeof value !== 'string') {
    throw new Error(`${label} must be a string`);
  }
}

function validateTimeout(handler: JsonObject, label: string): void {
  for (const field of ['timeout', 'timeoutSec']) {
    const value = handler[field];
    if (value !== undefined && (typeof value !== 'number' || !Number.isFinite(value) || value <= 0)) {
      throw new Error(`${fieldLabel(label, field)} must be a positive number`);
    }
  }
}

function validateMatcher(handler: JsonObject, event: string, label: string): void {
  if (handler.matcher === undefined) return;
  const matcher = requireString(handler.matcher, fieldLabel(label, 'matcher'));
  if ((event === 'PreToolUse' || event === 'PermissionRequest')
    && (matcher === '*' || matcher === '**')) return;
  try {
    new RegExp(`^(?:${matcher})$`);
  } catch (error) {
    throw new Error(`${fieldLabel(label, 'matcher')} is not a valid regular expression`, {
      cause: error instanceof Error ? error : new Error(String(error)),
    });
  }
}

function validateStringMap(value: unknown, label: string): void {
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  for (const [key, item] of Object.entries(value)) {
    requireString(item, `${label}.${key}`);
  }
}

function validateCommand(handler: JsonObject, event: string, label: string): void {
  const commands = ['bash', 'powershell', 'command']
    .map((field) => handler[field])
    .filter((value) => value !== undefined);
  if (commands.length === 0) {
    throw new Error(`${label} must define bash, powershell, or command`);
  }
  for (const field of ['bash', 'powershell', 'command', 'cwd']) {
    validateOptionalString(handler[field], fieldLabel(label, field));
  }
  if (handler.env !== undefined) validateStringMap(handler.env, fieldLabel(label, 'env'));
  validateTimeout(handler, label);
  validateMatcher(handler, event, label);
}

function isLoopback(hostname: string): boolean {
  return hostname === 'localhost'
    || hostname === '::1'
    || hostname.startsWith('127.');
}

function validateHttp(handler: JsonObject, event: string, label: string): void {
  const rawUrl = requireString(handler.url, fieldLabel(label, 'url'));
  let url: URL;
  try {
    url = new URL(rawUrl);
  } catch (error) {
    throw new Error(`${fieldLabel(label, 'url')} is invalid`, {
      cause: error instanceof Error ? error : new Error(String(error)),
    });
  }
  const localhostAllowed = process.env.COPILOT_HOOK_ALLOW_LOCALHOST === '1'
    && url.protocol === 'http:'
    && isLoopback(url.hostname);
  if (url.protocol !== 'https:' && !localhostAllowed) {
    throw new Error(`${fieldLabel(label, 'url')} must use HTTPS`);
  }
  if (handler.headers !== undefined) {
    validateStringMap(handler.headers, fieldLabel(label, 'headers'));
  }
  if (handler.allowedEnvVars !== undefined) {
    if (!Array.isArray(handler.allowedEnvVars)
      || handler.allowedEnvVars.some((value) => typeof value !== 'string' || !value)) {
      throw new Error(`${fieldLabel(label, 'allowedEnvVars')} must be an array of strings`);
    }
  }
  validateTimeout(handler, label);
  validateMatcher(handler, event, label);
}

function validatePrompt(handler: JsonObject, event: string, label: string): void {
  if (event !== 'sessionStart' && event !== 'SessionStart') {
    throw new Error(`${label} prompt hooks are supported only for sessionStart`);
  }
  requireString(handler.prompt, fieldLabel(label, 'prompt'));
}

function validateHandler(handler: JsonObject, event: string, label: string): void {
  const type = handler.type ?? 'command';
  if (type === 'command') validateCommand(handler, event, label);
  else if (type === 'http') validateHttp(handler, event, label);
  else if (type === 'prompt') validatePrompt(handler, event, label);
  else throw new Error(`${fieldLabel(label, 'type')} is unsupported`);
}

export function validateHooks(value: unknown, label: string): CopilotHooks {
  if (value === undefined) return {};
  if (!isObject(value)) throw new Error(`${label} field "hooks" must be an object`);
  const hooks: CopilotHooks = {};
  for (const [event, handlers] of Object.entries(value)) {
    if (!SUPPORTED_EVENTS.has(event)) throw new Error(`${label} hook event "${event}" is unsupported`);
    if (!Array.isArray(handlers)) throw new Error(`${label} field "hooks.${event}" must be an array`);
    hooks[event] = handlers.map((handler, index) => {
      const itemLabel = `${label} handler hooks.${event}[${index}]`;
      if (!isObject(handler)) throw new Error(`${itemLabel} must be an object`);
      validateHandler(handler, event, itemLabel);
      return handler;
    });
  }
  return hooks;
}
