package plugins

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runOpenCodeModule(t *testing.T, modulePath string, scriptBody string) string {
	t.Helper()
	nodeBinary, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required to execute generated OpenCode plugins")
	}
	moduleURL := "file:///" + filepath.ToSlash(modulePath)
	moduleJSON, err := json.Marshal(moduleURL)
	if err != nil {
		t.Fatalf("marshal module URL: %v", err)
	}
	script := "const pluginModule = await import(" + string(moduleJSON) + ");\n" + scriptBody
	cmd := exec.Command(nodeBinary, "--input-type=module", "--eval", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("execute generated plugin: %v\n%s", err, output)
	}
	return string(output)
}

func writeOpenCodeModule(t *testing.T, hookPath string, guardPath string) string {
	t.Helper()
	modulePath := filepath.Join(t.TempDir(), "elydora-audit.mjs")
	if err := os.WriteFile(modulePath, []byte(buildOpenCodePlugin(hookPath, guardPath)), 0600); err != nil {
		t.Fatalf("write generated plugin: %v", err)
	}
	return modulePath
}

func TestOpenCodePluginUsesCurrentAPIAndBlocksFrozenAgent(t *testing.T) {
	tempDir := t.TempDir()
	guardPath := filepath.Join(tempDir, "guard.cjs")
	if err := os.WriteFile(
		guardPath,
		[]byte("process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n"),
		0600,
	); err != nil {
		t.Fatalf("write guard: %v", err)
	}
	modulePath := writeOpenCodeModule(t, filepath.Join(tempDir, "hook.cjs"), guardPath)
	runOpenCodeModule(t, modulePath, `
const hooks = await pluginModule.ElydoraAuditPlugin({ project: { name: 'project' } });
if (typeof hooks['tool.execute.before'] !== 'function') process.exit(10);
if (typeof hooks['tool.execute.after'] !== 'function') process.exit(11);
try {
  await hooks['tool.execute.before'](
    { tool: 'bash', sessionID: 'session-1', callID: 'call-1' },
    { args: { command: 'echo test' } },
  );
  process.exit(12);
} catch (error) {
  if (!String(error.message).includes('Agent is frozen by Elydora')) process.exit(13);
}
`)
}

func TestOpenCodePluginForwardsToolEvent(t *testing.T) {
	tempDir := t.TempDir()
	capturePath := filepath.Join(tempDir, "captured-event.json")
	hookPath := filepath.Join(tempDir, "hook.cjs")
	captureJSON, err := json.Marshal(capturePath)
	if err != nil {
		t.Fatalf("marshal capture path: %v", err)
	}
	hookScript := `
const fs = require('node:fs');
const chunks = [];
process.stdin.on('data', (chunk) => chunks.push(chunk));
process.stdin.on('end', () => fs.writeFileSync(` + string(captureJSON) + `, Buffer.concat(chunks)));
`
	if err := os.WriteFile(hookPath, []byte(hookScript), 0600); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	modulePath := writeOpenCodeModule(t, hookPath, filepath.Join(tempDir, "guard.cjs"))
	runOpenCodeModule(t, modulePath, `
const hooks = await pluginModule.ElydoraAuditPlugin({ project: { name: 'project' } });
await hooks['tool.execute.after'](
  {
    tool: 'bash',
    sessionID: 'session-1',
    callID: 'call-1',
    args: { command: 'echo test' },
  },
  { title: 'Shell', output: 'test' },
);
`)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(capturePath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	payload, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured event: %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("decode captured event: %v", err)
	}
	if event["tool_name"] != "bash" || event["session_id"] != "session-1" {
		t.Fatalf("unexpected captured event: %s", payload)
	}
	toolInput, ok := event["tool_input"].(map[string]any)
	if !ok || toolInput["command"] != "echo test" {
		t.Fatalf("unexpected tool input: %v", event["tool_input"])
	}
}

func TestOpenCodePluginUsesGlobalConfigDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	path, err := (&OpenCodePlugin{}).configPath()
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	wantSuffix := filepath.Join(".config", "opencode", "plugins", "elydora-audit.mjs")
	if !strings.HasSuffix(path, wantSuffix) {
		t.Fatalf("config path = %q, want suffix %q", path, wantSuffix)
	}
}
