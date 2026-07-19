package plugins

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	clineAgentKey        = "cline"
	clineGuardFileName   = "PreToolUse.mjs"
	clineAuditFileName   = "PostToolUse.mjs"
	clineGuardScript     = "guard.js"
	clineAuditScript     = "hook.js"
	clineMetadataMarker  = "// @elydora-cline-hook "
	clineMetadataVersion = 1
)

type clineHookMetadata struct {
	Version     int    `json:"version"`
	Kind        string `json:"kind"`
	AgentID     string `json:"agentId"`
	RuntimePath string `json:"runtimePath"`
}

type clineHookFile struct {
	exists   bool
	filePath string
	source   string
	metadata *clineHookMetadata
}

type clineHookPaths struct {
	hooksDirectory string
	guardPath      string
	auditPath      string
}

type clineRuntimeContract struct {
	agentID        string
	agentDirectory string
	guardPath      string
	auditPath      string
}

func clineHomeDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

func clineElydoraRoot() (string, error) {
	home, err := clineHomeDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".elydora"), nil
}

func resolveClineHookFiles() (clineHookPaths, error) {
	clineDirectory := strings.TrimSpace(os.Getenv("CLINE_DIR"))
	if clineDirectory == "" {
		home, err := clineHomeDirectory()
		if err != nil {
			return clineHookPaths{}, err
		}
		clineDirectory = filepath.Join(home, ".cline")
	} else {
		absolute, err := filepath.Abs(clineDirectory)
		if err != nil {
			return clineHookPaths{}, fmt.Errorf("resolve CLINE_DIR path: %w", err)
		}
		clineDirectory = absolute
	}
	hooksDirectory := filepath.Join(clineDirectory, "hooks")
	return clineHookPaths{
		hooksDirectory: hooksDirectory,
		guardPath:      filepath.Join(hooksDirectory, clineGuardFileName),
		auditPath:      filepath.Join(hooksDirectory, clineAuditFileName),
	}, nil
}

func sameClineAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func normalizeClinePath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	normalized := filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		normalized = strings.ToLower(normalized)
	}
	return normalized, nil
}

func sameClinePath(left, right string) (bool, error) {
	normalizedLeft, err := normalizeClinePath(left)
	if err != nil {
		return false, err
	}
	normalizedRight, err := normalizeClinePath(right)
	if err != nil {
		return false, err
	}
	return normalizedLeft == normalizedRight, nil
}

func buildClineMetadata(kind, agentID, runtimePath string) (clineHookMetadata, error) {
	metadata := clineHookMetadata{
		Version:     clineMetadataVersion,
		Kind:        kind,
		AgentID:     agentID,
		RuntimePath: runtimePath,
	}
	if err := validateClineMetadata(metadata); err != nil {
		return clineHookMetadata{}, err
	}
	return metadata, nil
}

func validateClineMetadata(metadata clineHookMetadata) error {
	if metadata.Version != clineMetadataVersion {
		return fmt.Errorf("metadata version must be 1")
	}
	if metadata.Kind != "guard" && metadata.Kind != "audit" {
		return fmt.Errorf(`metadata kind must be "guard" or "audit"`)
	}
	if metadata.AgentID == "" {
		return fmt.Errorf("metadata agentId must be a non-empty string")
	}
	if !filepath.IsAbs(metadata.RuntimePath) {
		return fmt.Errorf("metadata runtimePath must be an absolute path")
	}
	return nil
}

func encodeClineMetadata(metadata clineHookMetadata) (string, error) {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("encode Cline hook metadata: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func clineJSONString(value string) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func buildClineWrapper(metadata clineHookMetadata) (string, error) {
	if err := validateClineMetadata(metadata); err != nil {
		return "", err
	}
	encodedMetadata, err := encodeClineMetadata(metadata)
	if err != nil {
		return "", err
	}
	hookKind, err := clineJSONString(metadata.Kind)
	if err != nil {
		return "", fmt.Errorf("encode Cline hook kind: %w", err)
	}
	runtimePath, err := clineJSONString(metadata.RuntimePath)
	if err != nil {
		return "", fmt.Errorf("encode Cline runtime path: %w", err)
	}
	const template = `#!/usr/bin/env node
__METADATA_LINE__
import { spawn } from 'node:child_process';

const hookKind = __HOOK_KIND__;
const runtimePath = __RUNTIME_PATH__;

function runRuntime(input) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [runtimePath], {
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    });
    const stdout = [];
    const stderr = [];
    let stdinError;
    child.stdout.on('data', (chunk) => stdout.push(chunk));
    child.stderr.on('data', (chunk) => stderr.push(chunk));
    child.stdin.on('error', (error) => {
      if (error?.code !== 'EPIPE') stdinError = error;
    });
    child.once('error', reject);
    child.once('close', (code, signal) => {
      if (stdinError) {
        reject(stdinError);
        return;
      }
      resolve({
        code,
        signal,
        stdout: Buffer.concat(stdout).toString('utf-8'),
        stderr: Buffer.concat(stderr).toString('utf-8'),
      });
    });
    child.stdin.end(input);
  });
}

async function main() {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  const result = await runRuntime(Buffer.concat(chunks));
  if (result.stdout) process.stderr.write(result.stdout);
  if (result.stderr) process.stderr.write(result.stderr);

  if (hookKind === 'guard' && result.code === 2) {
    const errorMessage = result.stderr.trim() || 'Agent is frozen by Elydora.';
    process.stdout.write('HOOK_CONTROL\t' + JSON.stringify({ cancel: true, errorMessage }) + '\n');
    return;
  }
  if (result.signal) throw new Error('runtime terminated by signal ' + result.signal);
  if (result.code !== 0) throw new Error('runtime exited with code ' + result.code);
}

main().catch((error) => {
  const message = error instanceof Error ? error.stack || error.message : String(error);
  process.stderr.write('Elydora Cline ' + hookKind + ' wrapper failed: ' + message + '\n');
  process.exitCode = 1;
});
`
	return strings.NewReplacer(
		"__METADATA_LINE__", clineMetadataMarker+encodedMetadata,
		"__HOOK_KIND__", hookKind,
		"__RUNTIME_PATH__", runtimePath,
	).Replace(template), nil
}

func decodeClineMetadata(encoded string) (clineHookMetadata, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return clineHookMetadata{}, fmt.Errorf("invalid encoded metadata: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return clineHookMetadata{}, fmt.Errorf("invalid encoded metadata: %w", err)
	}
	expected := []string{"version", "kind", "agentId", "runtimePath"}
	if len(fields) != len(expected) {
		return clineHookMetadata{}, fmt.Errorf("metadata contains an unexpected field set")
	}
	for _, field := range expected {
		if _, exists := fields[field]; !exists {
			return clineHookMetadata{}, fmt.Errorf("metadata contains an unexpected field set")
		}
	}
	var metadata clineHookMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return clineHookMetadata{}, fmt.Errorf("invalid encoded metadata: %w", err)
	}
	if err := validateClineMetadata(metadata); err != nil {
		return clineHookMetadata{}, err
	}
	return metadata, nil
}

func parseClineMetadata(filePath, source string) (*clineHookMetadata, error) {
	lines := strings.SplitN(source, "\n", 3)
	if len(lines) < 2 {
		return nil, nil
	}
	markerLine := strings.TrimSuffix(lines[1], "\r")
	if !strings.HasPrefix(markerLine, clineMetadataMarker) {
		return nil, nil
	}
	metadata, err := decodeClineMetadata(strings.TrimPrefix(markerLine, clineMetadataMarker))
	if err != nil {
		return nil, fmt.Errorf("parse Elydora Cline hook metadata at %s: %w", filePath, err)
	}
	return &metadata, nil
}

func assertClineWrapperIntegrity(file clineHookFile) error {
	if file.metadata == nil {
		return nil
	}
	expected, err := buildClineWrapper(*file.metadata)
	if err != nil {
		return err
	}
	if file.source != expected {
		return fmt.Errorf("Elydora Cline hook at %s does not match the managed template", file.filePath)
	}
	return nil
}

func validateClineAgentSegment(agentID string) error {
	if agentID == "." || agentID == ".." || filepath.Base(agentID) != agentID {
		return fmt.Errorf("Elydora Cline hook metadata contains an invalid agentId")
	}
	return nil
}

func clineContractForFiles(
	guardFile, auditFile clineHookFile,
) (*clineRuntimeContract, error) {
	if guardFile.metadata == nil || auditFile.metadata == nil {
		return nil, nil
	}
	if err := assertClineWrapperIntegrity(guardFile); err != nil {
		return nil, err
	}
	if err := assertClineWrapperIntegrity(auditFile); err != nil {
		return nil, err
	}
	guard := guardFile.metadata
	audit := auditFile.metadata
	if guard.Kind != "guard" || audit.Kind != "audit" {
		return nil, fmt.Errorf("Elydora Cline hook files contain mismatched event metadata")
	}
	if !sameClineAgentID(guard.AgentID, audit.AgentID) {
		return nil, fmt.Errorf("Elydora Cline hook files reference different agents")
	}
	if err := validateClineAgentSegment(guard.AgentID); err != nil {
		return nil, err
	}
	runtimeRoot, err := clineElydoraRoot()
	if err != nil {
		return nil, err
	}
	agentDirectory := filepath.Join(runtimeRoot, guard.AgentID)
	expectedGuard := filepath.Join(agentDirectory, clineGuardScript)
	expectedAudit := filepath.Join(agentDirectory, clineAuditScript)
	guardMatches, err := sameClinePath(guard.RuntimePath, expectedGuard)
	if err != nil {
		return nil, fmt.Errorf("compare Cline guard runtime path: %w", err)
	}
	auditMatches, err := sameClinePath(audit.RuntimePath, expectedAudit)
	if err != nil {
		return nil, fmt.Errorf("compare Cline audit runtime path: %w", err)
	}
	if !guardMatches || !auditMatches {
		return nil, fmt.Errorf("Elydora Cline hook metadata references an unexpected runtime path")
	}
	return &clineRuntimeContract{
		agentID:        guard.AgentID,
		agentDirectory: agentDirectory,
		guardPath:      expectedGuard,
		auditPath:      expectedAudit,
	}, nil
}
