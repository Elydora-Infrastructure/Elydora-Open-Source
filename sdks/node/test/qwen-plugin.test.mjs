import assert from "node:assert/strict";
import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
  childEnv,
  cliPath,
  createFixture,
  expectedShell,
  generatedCommand,
  managedHandler,
  parseSettings,
  registryModuleUrl,
  runHook,
  runNode,
  runPlugin,
} from "../test-support/qwen.mjs";

test("Qwen Code is registered in the SDK and CLI", async () => {
  const { SUPPORTED_AGENTS } = await import(registryModuleUrl);
  assert.deepEqual(SUPPORTED_AGENTS.get("qwen"), {
    name: "Qwen Code",
    configDir: "~/.qwen",
    configFile: "settings.json",
  });
  const fixture = await createFixture();
  try {
    const result = await runNode(
      ["--no-warnings", cliPath, "status"],
      childEnv(fixture),
      "",
      fixture.workspaceDir,
    );
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /Qwen Code \(qwen\)/);
  } finally {
    await fixture.close();
  }
});

test("Qwen install preserves JSONC settings and is idempotent", async () => {
  const existing = [
    "{",
    "  // Keep this user preference.",
    '  "theme": "GitHub",',
    '  "hooks": {',
    '    "SessionStart": [{ "hooks": [{ "type": "command", "command": "session-hook" }] }],',
    '    "PreToolUse": [{ "matcher": "read_file", "hooks": [{ "type": "command", "command": "user-hook" }] }]',
    "  }",
    "}",
    "",
  ].join("\r\n");
  const fixture = await createFixture({ existingSettings: existing });
  const workspaceSettings = path.join(
    fixture.workspaceDir,
    ".qwen",
    "settings.json",
  );
  await mkdir(path.dirname(workspaceSettings), { recursive: true });
  await writeFile(workspaceSettings, '{ "owner": "workspace" }\n');
  try {
    const first = await fixture.install();
    assert.equal(first.code, 0, first.stderr);
    assert.match(first.stdout, /run \/hooks/i);
    const second = await fixture.install();
    assert.equal(second.code, 0, second.stderr);
    const raw = await readFile(fixture.configPath, "utf-8");
    const settings = parseSettings(raw);
    assert.match(raw, /Keep this user preference/);
    assert.match(raw, /\r\n/);
    assert.equal(settings.theme, "GitHub");
    assert.equal(
      settings.hooks.SessionStart[0].hooks[0].command,
      "session-hook",
    );
    assert.equal(settings.hooks.PreToolUse.length, 2);
    assert.equal(settings.hooks.PostToolUse.length, 1);
    for (const [event, scriptPath] of [
      ["PreToolUse", fixture.guardScriptPath],
      ["PostToolUse", fixture.hookScriptPath],
    ]) {
      const handler = managedHandler(settings, event, scriptPath);
      assert.deepEqual(Object.keys(handler).sort(), [
        "command",
        "shell",
        "timeout",
        "type",
      ]);
      assert.equal(handler.type, "command");
      assert.equal(handler.shell, expectedShell);
      assert.equal(handler.timeout, 10_000);
    }
    assert.equal(
      await readFile(workspaceSettings, "utf-8"),
      '{ "owner": "workspace" }\n',
    );
  } finally {
    await fixture.close();
  }
});

test("Qwen resolves QWEN_HOME with official user env precedence", async () => {
  const fixture = await createFixture();
  const firstHome = path.join(fixture.rootDir, "first qwen home");
  const secondHome = path.join(fixture.rootDir, "second qwen home");
  await mkdir(path.join(fixture.homeDir, ".qwen"), { recursive: true });
  await writeFile(
    path.join(fixture.homeDir, ".qwen", ".env"),
    `QWEN_HOME=${firstHome}\n`,
  );
  await writeFile(
    path.join(fixture.homeDir, ".env"),
    `QWEN_HOME=${secondHome}\n`,
  );
  try {
    const result = await fixture.install();
    assert.equal(result.code, 0, result.stderr);
    const selected = path.join(firstHome, "settings.json");
    assert.ok(
      managedHandler(
        parseSettings(await readFile(selected, "utf-8")),
        "PreToolUse",
        fixture.guardScriptPath,
      ),
    );
    await assert.rejects(readFile(path.join(secondHome, "settings.json")), {
      code: "ENOENT",
    });
    await assert.rejects(readFile(fixture.configPath), { code: "ENOENT" });
    const status = await runPlugin(fixture, "status", null);
    assert.equal(JSON.parse(status.stdout).configPath, selected);
  } finally {
    await fixture.close();
  }
});

test("Qwen resolves explicit relative, tilde, and empty QWEN_HOME values", async () => {
  for (const [value, expectedPath] of [
    [
      "relative-qwen",
      (fixture) =>
        path.join(fixture.workspaceDir, "relative-qwen", "settings.json"),
    ],
    [
      "~/custom-qwen",
      (fixture) => path.join(fixture.homeDir, "custom-qwen", "settings.json"),
    ],
    ["", (fixture) => fixture.configPath],
  ]) {
    const fixture = await createFixture();
    await mkdir(path.join(fixture.homeDir, ".qwen"), { recursive: true });
    await writeFile(
      path.join(fixture.homeDir, ".qwen", ".env"),
      `QWEN_HOME=${path.join(fixture.rootDir, "ignored-qwen-home")}\n`,
    );
    try {
      const result = await fixture.install({ QWEN_HOME: value });
      assert.equal(result.code, 0, result.stderr);
      assert.ok(
        parseSettings(await readFile(expectedPath(fixture), "utf-8")).hooks,
      );
    } finally {
      await fixture.close();
    }
  }
});

test("Qwen hooks block freezes and forward official input", async () => {
  const capturePath = path.join(
    os.tmpdir(),
    `elydora-qwen-event-${process.pid}-${Date.now()}.json`,
  );
  const hookSource = `
    const fs = require('node:fs');
    const chunks = [];
    process.stdin.on('data', (chunk) => chunks.push(chunk));
    process.stdin.on('end', () => fs.writeFileSync(${JSON.stringify(capturePath)}, Buffer.concat(chunks)));
  `;
  const fixture = await createFixture({ hookSource });
  try {
    const installed = await fixture.install();
    assert.equal(installed.code, 0, installed.stderr);
    const settings = parseSettings(await readFile(fixture.configPath, "utf-8"));
    const guard = managedHandler(
      settings,
      "PreToolUse",
      fixture.guardScriptPath,
    );
    const audit = managedHandler(
      settings,
      "PostToolUse",
      fixture.hookScriptPath,
    );
    const preInput = JSON.stringify({
      session_id: "session-1",
      transcript_path: "transcript.jsonl",
      cwd: fixture.workspaceDir,
      hook_event_name: "PreToolUse",
      timestamp: "2026-07-19T00:00:00.000Z",
      tool_name: "run_shell_command",
      tool_input: { command: "echo test" },
    });
    const guardResult = await runHook(guard, preInput);
    assert.equal(guardResult.code, 2, guardResult.stderr);
    assert.match(guardResult.stderr, /Agent is frozen by Elydora/);
    const postInput = preInput.replace("PreToolUse", "PostToolUse");
    const auditResult = await runHook(audit, postInput);
    assert.equal(auditResult.code, 0, auditResult.stderr);
    assert.equal(await readFile(capturePath, "utf-8"), postInput);
  } finally {
    await fixture.close();
    await rm(capturePath, { force: true });
  }
});

test("Qwen status requires enabled hooks and complete runtimes", async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    let status = JSON.parse((await runPlugin(fixture, "status", null)).stdout);
    assert.equal(status.installed, true);
    const settings = parseSettings(await readFile(fixture.configPath, "utf-8"));
    await writeFile(
      fixture.configPath,
      `${JSON.stringify({ ...settings, disableAllHooks: true }, null, 2)}\n`,
    );
    status = JSON.parse((await runPlugin(fixture, "status", null)).stdout);
    assert.equal(status.installed, false);
    assert.equal(status.hookConfigured, false);
    await writeFile(
      fixture.configPath,
      `${JSON.stringify({ ...settings, disableAllHooks: false }, null, 2)}\n`,
    );
    await rm(fixture.guardScriptPath);
    status = JSON.parse((await runPlugin(fixture, "status", null)).stdout);
    assert.equal(status.installed, false);
    assert.equal(status.hookConfigured, true);
    assert.equal(status.hookScriptExists, false);
  } finally {
    await fixture.close();
  }
});

test("Qwen uninstall removes exact ownership and preserves user hooks", async () => {
  const fixture = await createFixture({
    existingSettings: { $version: 4, owner: "user" },
  });
  try {
    assert.equal((await fixture.install()).code, 0);
    const settings = parseSettings(await readFile(fixture.configPath, "utf-8"));
    const managed = settings.hooks.PreToolUse.at(-1);
    managed.hooks.push({ type: "command", command: "user-command" });
    settings.hooks.PreToolUse.push({
      matcher: "*",
      hooks: [
        {
          type: "command",
          command: generatedCommand(
            path.join(fixture.homeDir, ".elydora", "agent-10", "guard.js"),
          ),
          shell: expectedShell,
          timeout: 10_000,
        },
      ],
    });
    await writeFile(
      fixture.configPath,
      `${JSON.stringify(settings, null, 2)}\n`,
    );
    const before = await readFile(fixture.configPath, "utf-8");
    assert.equal(
      (await runPlugin(fixture, "uninstall", "other-agent")).code,
      0,
    );
    assert.equal(await readFile(fixture.configPath, "utf-8"), before);
    const uninstallId = process.platform === "win32" ? "AGENT-1" : "agent-1";
    const result = await runPlugin(fixture, "uninstall", uninstallId);
    assert.equal(result.code, 0, result.stderr);
    const remaining = parseSettings(
      await readFile(fixture.configPath, "utf-8"),
    );
    assert.equal(remaining.$version, 4);
    assert.equal(remaining.owner, "user");
    assert.equal(remaining.hooks.PreToolUse.length, 2);
    assert.equal(
      remaining.hooks.PreToolUse[0].hooks[0].command,
      "user-command",
    );
    assert.match(JSON.stringify(remaining), /agent-10/);
    assert.equal(remaining.hooks.PostToolUse, undefined);
  } finally {
    await fixture.close();
  }
});

test("Qwen removes an Elydora-owned settings file on uninstall", async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    assert.match(
      await readFile(fixture.configPath, "utf-8"),
      /^\/\/ Managed by Elydora/,
    );
    const result = await runPlugin(fixture, "uninstall", "agent-1");
    assert.equal(result.code, 0, result.stderr);
    await assert.rejects(readFile(fixture.configPath), { code: "ENOENT" });
  } finally {
    await fixture.close();
  }
});

test("Qwen rejects malformed settings before writes", async () => {
  const cases = [
    "{ malformed",
    "[]",
    '{ "owner": true, }',
    '{ "hooks": {}, "hooks": {} }',
    '{ "disableAllHooks": "yes" }',
    '{ "hooks": [] }',
    '{ "hooks": { "UnknownEvent": [] } }',
    '{ "hooks": { "PreToolUse": null } }',
    '{ "hooks": { "PreToolUse": [null] } }',
    '{ "hooks": { "PreToolUse": [{ "matcher": "[", "hooks": [] }] } }',
    '{ "hooks": { "PreToolUse": [{ "sequential": "yes", "hooks": [] }] } }',
    '{ "hooks": { "PreToolUse": [{ "hooks": null }] } }',
    '{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "command" }] }] } }',
    '{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "http" }] }] } }',
    '{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "function", "command": "x" }] }] } }',
    '{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "command", "command": "x", "timeout": "ten" }] }] } }',
  ];
  for (const existingSettings of cases) {
    const fixture = await createFixture({ existingSettings });
    try {
      const result = await fixture.install();
      assert.equal(result.code, 1, `${existingSettings}\n${result.stderr}`);
      assert.match(result.stderr, /Qwen (Code settings|hooks)/i);
      assert.equal(
        await readFile(fixture.configPath, "utf-8"),
        existingSettings,
      );
      const names = await readdir(path.dirname(fixture.configPath));
      assert.equal(
        names.some((name) => name.endsWith(".tmp")),
        false,
      );
    } finally {
      await fixture.close();
    }
  }
});

test("Qwen fails on unreadable home env routing and missing runtimes", async () => {
  const envFixture = await createFixture();
  await mkdir(path.join(envFixture.homeDir, ".qwen", ".env"), {
    recursive: true,
  });
  try {
    const result = await envFixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /Qwen home environment/i);
    await assert.rejects(readFile(envFixture.configPath), { code: "ENOENT" });
  } finally {
    await envFixture.close();
  }

  const runtimeFixture = await createFixture();
  await rm(runtimeFixture.guardScriptPath);
  try {
    const result = await runtimeFixture.install();
    assert.equal(result.code, 1);
    assert.match(result.stderr, /runtime is missing/i);
    await assert.rejects(readFile(runtimeFixture.configPath), {
      code: "ENOENT",
    });
  } finally {
    await runtimeFixture.close();
  }
});

test("Qwen status surfaces malformed runtime metadata and leaves no staging files", async () => {
  const fixture = await createFixture();
  try {
    assert.equal((await fixture.install()).code, 0);
    assert.equal(
      (await readdir(path.dirname(fixture.configPath))).some((name) =>
        name.endsWith(".tmp"),
      ),
      false,
    );
    await writeFile(path.join(fixture.agentDir, "config.json"), "{ malformed");
    const status = await runPlugin(fixture, "status", null);
    assert.equal(status.code, 1);
    assert.match(status.stderr, /parse Elydora runtime config/i);
  } finally {
    await fixture.close();
  }
});
