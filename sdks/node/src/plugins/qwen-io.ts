import { randomUUID } from "node:crypto";
import fsp from "node:fs/promises";
import type { FileHandle } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { parse as parseDotenv } from "dotenv";
import {
  AGENT_KEY,
  type JsonObject,
  type RuntimeContract,
  isObject,
} from "./qwen-contract.js";
import {
  type QwenDocument,
  type RenderedDocument,
  createOwnedDocument,
  parseDocument,
} from "./qwen-config.js";

interface FileChange {
  readonly filePath: string;
  readonly label: string;
  readonly original?: string;
  readonly next?: string;
}

interface StagedChange {
  readonly change: FileChange;
  readonly tempPath?: string;
  readonly rollbackPath?: string;
  committed: boolean;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function hasErrorCode(error: unknown, code: string): boolean {
  return isObject(error) && error.code === code;
}

async function readOptional(
  filePath: string,
  label: string,
): Promise<string | undefined> {
  try {
    return await fsp.readFile(filePath, "utf-8");
  } catch (error) {
    if (hasErrorCode(error, "ENOENT")) return undefined;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

function defaultQwenHome(): string {
  const homeDir = os.homedir();
  return homeDir
    ? path.join(homeDir, ".qwen")
    : path.join(os.tmpdir(), ".qwen");
}

function resolveStoragePath(value: string): string {
  let resolved = value;
  if (
    resolved === "~" ||
    resolved.startsWith("~/") ||
    resolved.startsWith("~\\")
  ) {
    const segments =
      resolved === "~"
        ? []
        : resolved
            .slice(2)
            .split(/[/\\]+/)
            .filter(Boolean);
    resolved = path.join(os.homedir(), ...segments);
  }
  return path.isAbsolute(resolved) ? resolved : path.resolve(resolved);
}

async function qwenHomeFromEnvFile(
  filePath: string,
): Promise<string | undefined> {
  const raw = await readOptional(filePath, "Qwen home environment");
  if (raw === undefined) return undefined;
  const value = parseDotenv(raw).QWEN_HOME;
  return value || undefined;
}

export async function resolveQwenHome(): Promise<string> {
  const initialValue = process.env.QWEN_HOME;
  const initialHome = initialValue
    ? resolveStoragePath(initialValue)
    : defaultQwenHome();
  if (Object.hasOwn(process.env, "QWEN_HOME")) return initialHome;
  const candidates = [
    path.join(initialHome, ".env"),
    path.join(path.dirname(initialHome), ".env"),
  ];
  for (const candidate of candidates) {
    const discovered = await qwenHomeFromEnvFile(candidate);
    if (discovered) return resolveStoragePath(discovered);
  }
  return initialHome;
}

export async function readDocument(): Promise<QwenDocument> {
  const configPath = path.join(await resolveQwenHome(), "settings.json");
  const raw = await readOptional(configPath, "Qwen Code settings");
  return raw === undefined
    ? createOwnedDocument(configPath)
    : parseDocument({ exists: true, filePath: configPath, raw });
}

function fileChange(rendered: RenderedDocument): FileChange | undefined {
  if (!rendered.changed) return undefined;
  return {
    filePath: rendered.document.filePath,
    label: "Qwen Code settings",
    original: rendered.document.exists ? rendered.document.raw : undefined,
    next: rendered.next,
  };
}

async function unlinkOptional(filePath: string): Promise<void> {
  try {
    await fsp.unlink(filePath);
  } catch (error) {
    if (!hasErrorCode(error, "ENOENT")) throw error;
  }
}

async function writeExclusive(
  filePath: string,
  contents: string,
  label: string,
): Promise<void> {
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(filePath, "wx", 0o600);
    await handle.writeFile(contents, "utf-8");
    await handle.sync();
    await handle.close();
  } catch (error) {
    const failures = [asError(error)];
    if (handle) {
      try {
        await handle.close();
      } catch (closeError) {
        failures.push(asError(closeError));
      }
    }
    try {
      await unlinkOptional(filePath);
    } catch (cleanupError) {
      failures.push(asError(cleanupError));
    }
    if (failures.length > 1)
      throw new AggregateError(failures, `Stage ${label}`);
    throw new Error(`Stage ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: failures[0],
    });
  }
}

async function assertUnchanged(change: FileChange): Promise<void> {
  const current = await readOptional(change.filePath, change.label);
  if (current !== change.original) {
    throw new Error(
      `${change.label} changed during installation: ${change.filePath}`,
    );
  }
}

async function stage(change: FileChange): Promise<StagedChange> {
  await assertUnchanged(change);
  const directory = path.dirname(change.filePath);
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  const token = randomUUID();
  const tempPath =
    change.next === undefined
      ? undefined
      : path.join(directory, `.${path.basename(change.filePath)}.${token}.tmp`);
  const rollbackPath =
    change.original === undefined
      ? undefined
      : path.join(
          directory,
          `.${path.basename(change.filePath)}.${token}.rollback`,
        );
  try {
    if (tempPath) await writeExclusive(tempPath, change.next!, change.label);
    if (rollbackPath && change.next !== undefined) {
      await writeExclusive(
        rollbackPath,
        change.original!,
        `${change.label} rollback`,
      );
    }
  } catch (error) {
    const failures = [asError(error)];
    for (const stagedPath of [tempPath, rollbackPath]) {
      if (!stagedPath) continue;
      try {
        await unlinkOptional(stagedPath);
      } catch (cleanupError) {
        failures.push(asError(cleanupError));
      }
    }
    if (failures.length > 1)
      throw new AggregateError(failures, `Stage ${change.label}`);
    throw error;
  }
  return { change, tempPath, rollbackPath, committed: false };
}

async function commit(staged: StagedChange): Promise<void> {
  await assertUnchanged(staged.change);
  if (staged.change.next === undefined) {
    if (!staged.rollbackPath)
      throw new Error(`Missing rollback path for ${staged.change.label}`);
    await fsp.rename(staged.change.filePath, staged.rollbackPath);
  } else {
    if (!staged.tempPath)
      throw new Error(`Missing staged file for ${staged.change.label}`);
    await fsp.rename(staged.tempPath, staged.change.filePath);
  }
  staged.committed = true;
}

async function rollback(staged: StagedChange): Promise<void> {
  if (!staged.committed) return;
  if (staged.change.original === undefined) {
    await unlinkOptional(staged.change.filePath);
    return;
  }
  if (!staged.rollbackPath)
    throw new Error(`Missing rollback data for ${staged.change.label}`);
  await fsp.rename(staged.rollbackPath, staged.change.filePath);
}

async function cleanup(staged: StagedChange): Promise<void> {
  const stagedPaths = [staged.tempPath, staged.rollbackPath].filter(
    (filePath): filePath is string => filePath !== undefined,
  );
  await Promise.all(stagedPaths.map(unlinkOptional));
}

export async function writeDocument(rendered: RenderedDocument): Promise<void> {
  const change = fileChange(rendered);
  if (!change) return;
  const staged = await stage(change);
  try {
    await commit(staged);
  } catch (error) {
    const failures = [asError(error)];
    try {
      await rollback(staged);
    } catch (rollbackError) {
      failures.push(asError(rollbackError));
    }
    try {
      await cleanup(staged);
    } catch (cleanupError) {
      failures.push(asError(cleanupError));
    }
    if (failures.length > 1)
      throw new AggregateError(failures, "Write Qwen Code settings");
    throw new Error(`Write Qwen Code settings: ${errorMessage(error)}`, {
      cause: failures[0],
    });
  }
  await cleanup(staged);
}

export async function regularFileExists(
  filePath: string,
  label: string,
): Promise<boolean> {
  try {
    return (await fsp.stat(filePath)).isFile();
  } catch (error) {
    if (hasErrorCode(error, "ENOENT") || hasErrorCode(error, "ENOTDIR"))
      return false;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

export async function requireRuntime(
  filePath: string,
  label: string,
): Promise<void> {
  if (!filePath) throw new Error(`${label} path is required`);
  if (!(await regularFileExists(filePath, label)))
    throw new Error(`${label} is missing: ${filePath}`);
}

async function readRuntimeConfig(
  filePath: string,
): Promise<JsonObject | undefined> {
  const raw = await readOptional(filePath, "Elydora runtime config");
  if (raw === undefined) return undefined;
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (error) {
    throw new Error(
      `Failed to parse Elydora runtime config at ${filePath}: ${errorMessage(error)}`,
      {
        cause: asError(error),
      },
    );
  }
  if (!isObject(value))
    throw new Error(
      `Elydora runtime config at ${filePath} must contain a JSON object`,
    );
  return value;
}

export async function runtimeFilesExist(
  contracts: RuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    const agentDirectory = path.dirname(contract.guardPath);
    const runtimeConfig = await readRuntimeConfig(
      path.join(agentDirectory, "config.json"),
    );
    if (!runtimeConfig || runtimeConfig.agent_name !== AGENT_KEY) continue;
    const files = await Promise.all([
      regularFileExists(contract.guardPath, "Elydora guard runtime"),
      regularFileExists(contract.auditPath, "Elydora audit runtime"),
    ]);
    if (files.every(Boolean)) return true;
  }
  return false;
}
