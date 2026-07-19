import os from "node:os";
import path from "node:path";

export const AGENT_KEY = "qwen";
export const GUARD_SCRIPT = "guard.js";
export const AUDIT_SCRIPT = "hook.js";
export const HOOK_TIMEOUT_MS = 10_000;
export const TOOL_EVENTS = ["PreToolUse", "PostToolUse"] as const;

const EVENT_NAMES = new Set([
  "PreToolUse",
  "PostToolUse",
  "PostToolUseFailure",
  "PostToolBatch",
  "Notification",
  "UserPromptSubmit",
  "UserPromptExpansion",
  "SessionStart",
  "Stop",
  "MessageDisplay",
  "SubagentStart",
  "SubagentStop",
  "PreCompact",
  "PostCompact",
  "SessionEnd",
  "PermissionRequest",
  "PermissionDenied",
  "StopFailure",
  "TodoCreated",
  "TodoCompleted",
  "InstructionsLoaded",
]);
const CONFIG_FIELDS = new Set(["enabled", "disabled", "notifications"]);
const HANDLER_KEYS = ["command", "shell", "timeout", "type"];
const GROUP_KEYS = ["hooks", "matcher"];

export type JsonObject = Record<string, unknown>;
export type ToolEvent = (typeof TOOL_EVENTS)[number];

export interface QwenCommandHandler extends JsonObject {
  readonly type: "command";
  readonly command: string;
  readonly timeout?: number;
  readonly shell?: "bash" | "powershell";
}

export interface QwenGroup extends JsonObject {
  readonly matcher?: string;
  readonly sequential?: boolean;
  readonly hooks: JsonObject[];
}

export interface QwenHookSettings extends JsonObject {
  readonly PreToolUse?: QwenGroup[];
  readonly PostToolUse?: QwenGroup[];
}

export interface RuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface ManagedRemoval {
  readonly event: ToolEvent;
  readonly groupIndex: number;
  readonly handlerIndexes: number[];
  readonly removeGroup: boolean;
}

interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

export function isObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function quotePosix(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quotePowerShell(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

export function buildCommand(scriptPath: string): string {
  if (process.platform === "win32") {
    return `& ${quotePowerShell(process.execPath)} ${quotePowerShell(scriptPath)}; exit $LASTEXITCODE`;
  }
  return `${quotePosix(process.execPath)} ${quotePosix(scriptPath)}`;
}

export function buildGroup(scriptPath: string): QwenGroup {
  return {
    matcher: "*",
    hooks: [
      {
        type: "command",
        command: buildCommand(scriptPath),
        shell: process.platform === "win32" ? "powershell" : "bash",
        timeout: HOOK_TIMEOUT_MS,
      },
    ],
  };
}

function validateRegex(value: string, label: string): void {
  if (value.trim() === "" || value.trim() === "*") return;
  try {
    new RegExp(value);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`${label} must be a valid regular expression: ${message}`);
  }
}

function optionalString(value: JsonObject, key: string, label: string): void {
  if (value[key] !== undefined && typeof value[key] !== "string") {
    throw new Error(`${label} field "${key}" must be a string`);
  }
}

function optionalBoolean(value: JsonObject, key: string, label: string): void {
  if (value[key] !== undefined && typeof value[key] !== "boolean") {
    throw new Error(`${label} field "${key}" must be a boolean`);
  }
}

function optionalStringMap(
  value: JsonObject,
  key: string,
  label: string,
): void {
  const item = value[key];
  if (item === undefined) return;
  if (
    !isObject(item) ||
    Object.values(item).some((entry) => typeof entry !== "string")
  ) {
    throw new Error(`${label} field "${key}" must contain string values`);
  }
}

function validateHandler(value: unknown, label: string): JsonObject {
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (
    value.type !== "command" &&
    value.type !== "http" &&
    value.type !== "prompt"
  ) {
    throw new Error(`${label} type must be "command", "http", or "prompt"`);
  }
  if (
    value.timeout !== undefined &&
    (typeof value.timeout !== "number" ||
      !Number.isFinite(value.timeout) ||
      value.timeout < 0)
  ) {
    throw new Error(`${label} timeout must be a non-negative finite number`);
  }
  for (const key of ["name", "description", "statusMessage", "source"]) {
    optionalString(value, key, label);
  }
  if (value.type === "command") {
    if (typeof value.command !== "string" || value.command.length === 0) {
      throw new Error(`${label} command must be a non-empty string`);
    }
    optionalStringMap(value, "env", label);
    optionalBoolean(value, "async", label);
    if (
      value.shell !== undefined &&
      value.shell !== "bash" &&
      value.shell !== "powershell"
    ) {
      throw new Error(`${label} shell must be "bash" or "powershell"`);
    }
  }
  if (value.type === "http") {
    if (typeof value.url !== "string" || value.url.length === 0) {
      throw new Error(`${label} url must be a non-empty string`);
    }
    optionalStringMap(value, "headers", label);
    optionalBoolean(value, "once", label);
    optionalString(value, "if", label);
    if (
      value.allowedEnvVars !== undefined &&
      (!Array.isArray(value.allowedEnvVars) ||
        value.allowedEnvVars.some((item) => typeof item !== "string"))
    ) {
      throw new Error(`${label} allowedEnvVars must be an array of strings`);
    }
  }
  if (value.type === "prompt") {
    if (typeof value.prompt !== "string" || value.prompt.length === 0) {
      throw new Error(`${label} prompt must be a non-empty string`);
    }
    optionalString(value, "model", label);
  }
  return value;
}

function validateGroup(
  value: unknown,
  label: string,
  index: number,
): QwenGroup {
  const location = `${label}[${index}]`;
  if (!isObject(value)) throw new Error(`${location} must be an object`);
  if (value.matcher !== undefined) {
    if (typeof value.matcher !== "string")
      throw new Error(`${location} matcher must be a string`);
    validateRegex(value.matcher, `${location} matcher`);
  }
  if (value.sequential !== undefined && typeof value.sequential !== "boolean") {
    throw new Error(`${location} sequential must be a boolean`);
  }
  if (!Array.isArray(value.hooks))
    throw new Error(`${location} must contain a hooks array`);
  const hooks = value.hooks.map((handler, handlerIndex) =>
    validateHandler(handler, `${location}.hooks[${handlerIndex}]`),
  );
  return { ...value, hooks } as QwenGroup;
}

export function readHooks(value: unknown, label: string): QwenHookSettings {
  if (!isObject(value)) throw new Error(`${label} must contain a JSON object`);
  const settings: QwenHookSettings = { ...value };
  for (const [event, definitions] of Object.entries(value)) {
    if (CONFIG_FIELDS.has(event)) continue;
    if (!EVENT_NAMES.has(event))
      throw new Error(`${label} contains unsupported field "${event}"`);
    if (!Array.isArray(definitions))
      throw new Error(`${label} field "${event}" must be an array`);
    settings[event] = definitions.map((definition, index) =>
      validateGroup(definition, `${label} field "${event}"`, index),
    );
  }
  return settings;
}

function readQuotedArgument(
  command: string,
  start: number,
): ParsedArgument | undefined {
  if (command[start] !== "'") return undefined;
  let value = "";
  for (let index = start + 1; index < command.length; ) {
    if (process.platform === "win32" && command.startsWith("''", index)) {
      value += "'";
      index += 2;
      continue;
    }
    if (process.platform !== "win32" && command.startsWith(`'"'"'`, index)) {
      value += "'";
      index += 5;
      continue;
    }
    if (command[index] === "'") return { value, next: index + 1 };
    value += command[index];
    index += 1;
  }
  return undefined;
}

function parseGeneratedCommand(
  command: string,
): readonly [string, string] | undefined {
  if (process.platform === "win32" && !command.startsWith("& "))
    return undefined;
  const start = process.platform === "win32" ? 2 : 0;
  const executable = readQuotedArgument(command, start);
  if (!executable || command[executable.next] !== " ") return undefined;
  const script = readQuotedArgument(command, executable.next + 1);
  const expectedSuffix =
    process.platform === "win32" ? "; exit $LASTEXITCODE" : "";
  if (
    !script ||
    command.slice(script.next) !== expectedSuffix ||
    !executable.value ||
    !script.value
  )
    return undefined;
  return [executable.value, script.value];
}

function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === "win32"
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

function sameAgentId(left: string, right: string): boolean {
  return process.platform === "win32"
    ? left.toLowerCase() === right.toLowerCase()
    : left === right;
}

export function managedAgentId(
  handler: JsonObject,
  scriptName: string,
): string | undefined {
  if (
    Object.keys(handler).sort().join("\0") !== HANDLER_KEYS.join("\0") ||
    handler.type !== "command" ||
    handler.timeout !== HOOK_TIMEOUT_MS ||
    handler.shell !== (process.platform === "win32" ? "powershell" : "bash") ||
    typeof handler.command !== "string"
  )
    return undefined;
  const parsed = parseGeneratedCommand(handler.command);
  if (!parsed || !samePath(parsed[0], process.execPath)) return undefined;
  const scriptPath = parsed[1];
  if (path.basename(scriptPath) !== scriptName) return undefined;
  const agentDirectory = path.dirname(scriptPath);
  if (
    !samePath(path.dirname(agentDirectory), path.join(os.homedir(), ".elydora"))
  )
    return undefined;
  const agentId = path.basename(agentDirectory);
  return agentId && agentId !== "." && agentId !== ".." ? agentId : undefined;
}

function exactOwnedGroup(group: QwenGroup, managedIndexes: number[]): boolean {
  return (
    Object.keys(group).sort().join("\0") === GROUP_KEYS.join("\0") &&
    group.matcher === "*" &&
    managedIndexes.length > 0 &&
    managedIndexes.length === group.hooks.length
  );
}

export function managedRemovals(
  settings: QwenHookSettings,
  agentId?: string,
): ManagedRemoval[] {
  const removals: ManagedRemoval[] = [];
  for (const [event, scriptName] of [
    ["PreToolUse", GUARD_SCRIPT],
    ["PostToolUse", AUDIT_SCRIPT],
  ] as const) {
    (settings[event] ?? []).forEach((group, groupIndex) => {
      const handlerIndexes = group.hooks.flatMap((handler, handlerIndex) => {
        const managedId = managedAgentId(handler, scriptName);
        return managedId &&
          (agentId === undefined || sameAgentId(managedId, agentId))
          ? [handlerIndex]
          : [];
      });
      if (handlerIndexes.length > 0) {
        removals.push({
          event,
          groupIndex,
          handlerIndexes,
          removeGroup: exactOwnedGroup(group, handlerIndexes),
        });
      }
    });
  }
  return removals;
}

function managedIds(groups: QwenGroup[], scriptName: string): Set<string> {
  const ids = new Set<string>();
  for (const group of groups) {
    for (const handler of group.hooks) {
      const agentId = managedAgentId(handler, scriptName);
      if (agentId) ids.add(agentId);
    }
  }
  return ids;
}

export function runtimeContracts(
  settings: QwenHookSettings,
): RuntimeContract[] {
  const guards = managedIds(settings.PreToolUse ?? [], GUARD_SCRIPT);
  const audits = managedIds(settings.PostToolUse ?? [], AUDIT_SCRIPT);
  const root = path.join(os.homedir(), ".elydora");
  return [...guards]
    .filter((agentId) =>
      [...audits].some((auditId) => sameAgentId(auditId, agentId)),
    )
    .map((agentId) => ({
      agentId,
      guardPath: path.join(root, agentId, GUARD_SCRIPT),
      auditPath: path.join(root, agentId, AUDIT_SCRIPT),
    }));
}
