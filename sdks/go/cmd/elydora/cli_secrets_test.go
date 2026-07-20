package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testAPIToken = "ely_test_token"

type fakeSecretTerminal struct {
	interactive bool
	answers     []string
	prompts     []string
}

func (terminal *fakeSecretTerminal) Interactive() bool {
	return terminal.interactive
}

func (terminal *fakeSecretTerminal) ReadHidden(prompt string) (string, error) {
	terminal.prompts = append(terminal.prompts, prompt)
	if len(terminal.answers) == 0 {
		return "", os.ErrInvalid
	}
	answer := terminal.answers[0]
	terminal.answers = terminal.answers[1:]
	return answer, nil
}

func testPrivateKey() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
}

func writeSecretFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatalf("restrict secret file: %v", err)
		}
	}
}

func TestResolveInstallSecretsUsesHiddenPrompts(t *testing.T) {
	terminal := &fakeSecretTerminal{
		interactive: true,
		answers:     []string{testPrivateKey(), testAPIToken},
	}
	secrets, err := resolveInstallSecrets(installSecretSources{}, terminal)
	if err != nil {
		t.Fatalf("resolve install secrets: %v", err)
	}
	if secrets.privateKey != testPrivateKey() || secrets.token != testAPIToken {
		t.Fatalf("resolved secrets have unexpected values")
	}
	wantPrompts := []string{"Private key: ", "API token (optional): "}
	if strings.Join(terminal.prompts, "|") != strings.Join(wantPrompts, "|") {
		t.Fatalf("prompts = %q, want %q", terminal.prompts, wantPrompts)
	}
}

func TestResolveInstallSecretsRequiresFileWhenNoninteractive(t *testing.T) {
	terminal := &fakeSecretTerminal{interactive: false}
	_, err := resolveInstallSecrets(installSecretSources{}, terminal)
	if err == nil || !strings.Contains(err.Error(), "--private-key-file <path>") {
		t.Fatalf("resolve install secrets error = %v", err)
	}
	if len(terminal.prompts) != 0 {
		t.Fatalf("unexpected prompts: %q", terminal.prompts)
	}
}

func TestResolveInstallSecretsReadsCredentialFiles(t *testing.T) {
	directory := t.TempDir()
	privateKeyPath := filepath.Join(directory, "private.key")
	tokenPath := filepath.Join(directory, "token")
	writeSecretFile(t, privateKeyPath, testPrivateKey()+"\r\n")
	writeSecretFile(t, tokenPath, testAPIToken+"\n")

	secrets, err := resolveInstallSecrets(installSecretSources{
		privateKeyFile: privateKeyPath,
		tokenFile:      tokenPath,
	}, &fakeSecretTerminal{})
	if err != nil {
		t.Fatalf("resolve install secrets: %v", err)
	}
	if secrets.privateKey != testPrivateKey() || secrets.token != testAPIToken {
		t.Fatalf("resolved secrets have unexpected values")
	}
}

func TestReadSecretFileRejectsInvalidContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		pattern string
	}{
		{name: "empty", content: "", pattern: "is empty"},
		{name: "multiple lines", content: testPrivateKey() + "\nsecond\n", pattern: "one line"},
		{name: "nul", content: "key\x00value", pattern: "one line"},
		{name: "invalid UTF-8", content: string([]byte{0xff}), pattern: "UTF-8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private.key")
			writeSecretFile(t, path, test.content)
			_, err := readSecretFile(path, "private key")
			if err == nil || !strings.Contains(err.Error(), test.pattern) {
				t.Fatalf("read secret file error = %v", err)
			}
		})
	}
}

func TestReadSecretFileRejectsOversizedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.key")
	writeSecretFile(t, path, strings.Repeat("a", maxSecretFileBytes+1))
	_, err := readSecretFile(path, "private key")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("read secret file error = %v", err)
	}
}

func TestReadSecretFileRejectsSymbolicLink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.key")
	link := filepath.Join(directory, "linked.key")
	writeSecretFile(t, target, testPrivateKey())
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("file symbolic links unavailable: %v", err)
	}
	_, err := readSecretFile(link, "private key")
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("read secret file error = %v", err)
	}
}

func TestReadSecretFileRequiresOwnerOnlyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission contract")
	}
	path := filepath.Join(t.TempDir(), "private.key")
	writeSecretFile(t, path, testPrivateKey())
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatalf("widen secret file permissions: %v", err)
	}
	_, err := readSecretFile(path, "private key")
	if err == nil || !strings.Contains(err.Error(), "only by its owner") {
		t.Fatalf("read secret file error = %v", err)
	}
}

func TestReadSecretFileDetectsReplacementWhileOpening(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "private.key")
	replacement := filepath.Join(directory, "replacement.key")
	writeSecretFile(t, path, testPrivateKey())
	writeSecretFile(t, replacement, testPrivateKey())

	_, err := readSecretFileWithOpen(path, "private key", func(name string) (*os.File, error) {
		if removeErr := os.Remove(path); removeErr != nil {
			return nil, removeErr
		}
		if renameErr := os.Rename(replacement, path); renameErr != nil {
			return nil, renameErr
		}
		return os.Open(name)
	})
	if err == nil || !strings.Contains(err.Error(), "changed while opening") {
		t.Fatalf("read secret file error = %v", err)
	}
}

func TestValidatePrivateKeyRejectsInvalidSeeds(t *testing.T) {
	tests := []string{"invalid!", base64.RawURLEncoding.EncodeToString([]byte("short"))}
	for _, privateKey := range tests {
		if err := validatePrivateKey(privateKey); err == nil {
			t.Fatalf("validatePrivateKey(%q) succeeded", privateKey)
		}
	}
	if err := validatePrivateKey(testPrivateKey()); err != nil {
		t.Fatalf("validate private key: %v", err)
	}
}

func TestRejectLegacySecretArgumentsRedactsValues(t *testing.T) {
	tests := [][]string{
		{"--private-key", "must-not-appear"},
		{"--private-key=must-not-appear"},
		{"--token", "must-not-appear"},
		{"--token=must-not-appear"},
	}
	for _, arguments := range tests {
		err := rejectLegacySecretArguments(arguments)
		if err == nil {
			t.Fatalf("rejectLegacySecretArguments(%q) succeeded", arguments)
		}
		if strings.Contains(err.Error(), "must-not-appear") {
			t.Fatalf("legacy secret value leaked in error: %v", err)
		}
		if !strings.Contains(err.Error(), "-file") {
			t.Fatalf("replacement option missing from error: %v", err)
		}
	}
}
