import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { parse } from "jsonc-parser";

const pluginModuleUrl = pathToFileURL(
  path.resolve("dist/plugins/qwen.js"),
).href;

export const registryModuleUrl = pathToFileURL(
  path.resolve("dist/plugins/registry.js"),
).href;
export const cliPath = path.resolve("dist/cli.js");
export const expectedShell =
  process.platform === "win32" ? "powershell" : "bash";

export function runNode(args, env, input = "", cwd) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, args, {
      cwd,
      env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.once("error", reject);
    child.once("close", (code) => resolve({ code, stdout, stderr }));
    child.stdin.end(input);
  });
}

export function childEnv(fixture, overrides = {}) {
  const env = {
    ...process.env,
    HOME: fixture.homeDir,
    USERPROFILE: fixture.homeDir,
    ...overrides,
  };
  if (!Object.hasOwn(overrides, "QWEN_HOME")) delete env.QWEN_HOME;
  for (const [key, value] of Object.entries(env)) {
    if (value === undefined) delete env[key];
  }
  return env;
}

export function runPlugin(fixture, method, argument, overrides = {}) {
  const script = `
    import { qwenPlugin } from ${JSON.stringify(pluginModuleUrl)};
    const argument = JSON.parse(process.env.ELYDORA_TEST_ARGUMENT);
    const result = await qwenPlugin[process.env.ELYDORA_TEST_METHOD](argument);
    if (result !== undefined) console.log(JSON.stringify(result));
  `;
  return runNode(
    ["--input-type=module", "--eval", script],
    childEnv(fixture, {
      ...overrides,
      ELYDORA_TEST_ARGUMENT: JSON.stringify(argument),
      ELYDORA_TEST_METHOD: method,
    }),
    "",
    fixture.workspaceDir,
  );
}

function quoteShell(value) {
  return process.platform === "win32"
    ? `'${value.replaceAll("'", "''")}'`
    : `'${value.replaceAll("'", `'"'"'`)}'`;
}

export function generatedCommand(scriptPath) {
  const invocation = `${quoteShell(process.execPath)} ${quoteShell(scriptPath)}`;
  return process.platform === "win32"
    ? `& ${invocation}; exit $LASTEXITCODE`
    : invocation;
}

export function runHook(handler, input) {
  const command = process.platform === "win32" ? "powershell" : "bash";
  const args =
    process.platform === "win32"
      ? ["-NoProfile", "-NonInteractive", "-Command", handler.command]
      : ["-c", handler.command];
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.once("error", reject);
    child.once("close", (code) => resolve({ code, stdout, stderr }));
    child.stdin.end(input);
  });
}

export function parseSettings(raw) {
  const errors = [];
  const value = parse(raw, errors, {
    allowTrailingComma: false,
    disallowComments: false,
  });
  assert.deepEqual(errors, []);
  return value;
}

export function managedHandler(settings, event, scriptPath) {
  const command = generatedCommand(scriptPath);
  for (const group of settings.hooks?.[event] ?? []) {
    const handler = group.hooks.find(
      (candidate) => candidate.command === command,
    );
    if (handler) return handler;
  }
  return undefined;
}

export async function createFixture({
  existingSettings,
  guardSource,
  hookSource,
} = {}) {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), "elydora-qwen-"));
  const homeDir = path.join(rootDir, "home with spaces and 'quote");
  const workspaceDir = path.join(homeDir, "workspace");
  const agentDir = path.join(homeDir, ".elydora", "agent-1");
  const configPath = path.join(homeDir, ".qwen", "settings.json");
  const guardScriptPath = path.join(agentDir, "guard.js");
  const hookScriptPath = path.join(agentDir, "hook.js");
  await mkdir(workspaceDir, { recursive: true });
  await mkdir(agentDir, { recursive: true });
  await writeFile(
    guardScriptPath,
    guardSource ??
      "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n",
  );
  await writeFile(hookScriptPath, hookSource ?? "process.exit(0);\n");
  await writeFile(
    path.join(agentDir, "config.json"),
    JSON.stringify({ agent_id: "agent-1", agent_name: "qwen" }),
  );
  if (existingSettings !== undefined) {
    await mkdir(path.dirname(configPath), { recursive: true });
    await writeFile(
      configPath,
      typeof existingSettings === "string"
        ? existingSettings
        : `${JSON.stringify(existingSettings, null, 2)}\n`,
    );
  }
  return {
    agentDir,
    configPath,
    guardScriptPath,
    homeDir,
    hookScriptPath,
    rootDir,
    workspaceDir,
    async install(overrides = {}) {
      return runPlugin(
        this,
        "install",
        {
          agentName: "qwen",
          agentId: "agent-1",
          guardScriptPath,
          hookScriptPath,
        },
        overrides,
      );
    },
    async close() {
      await rm(rootDir, { recursive: true, force: true });
    },
  };
}
