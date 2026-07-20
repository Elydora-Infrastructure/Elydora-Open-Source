package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"unicode/utf8"
)

const maxSecretFileBytes = 64 * 1024

type installSecretSources struct {
	privateKeyFile string
	tokenFile      string
}

type installSecrets struct {
	privateKey string
	token      string
}

type secretTerminal interface {
	Interactive() bool
	ReadHidden(prompt string) (string, error)
}

type standardSecretTerminal struct {
	input  *os.File
	output *os.File
}

func newStandardSecretTerminal() standardSecretTerminal {
	return standardSecretTerminal{input: os.Stdin, output: os.Stderr}
}

func (terminal standardSecretTerminal) Interactive() bool {
	return isInteractiveTerminal(terminal.input) &&
		isInteractiveTerminal(terminal.output)
}

func (terminal standardSecretTerminal) ReadHidden(prompt string) (string, error) {
	if !terminal.Interactive() {
		return "", errors.New("hidden input requires an interactive terminal")
	}
	if _, err := fmt.Fprint(terminal.output, prompt); err != nil {
		return "", fmt.Errorf("write secret prompt: %w", err)
	}
	raw, readErr := readHiddenLine(terminal.input)
	_, newlineErr := fmt.Fprintln(terminal.output)
	if readErr != nil {
		clear(raw)
		return "", errors.Join(
			fmt.Errorf("read hidden input: %w", readErr),
			newlineErr,
		)
	}
	if newlineErr != nil {
		clear(raw)
		return "", fmt.Errorf("finish secret prompt: %w", newlineErr)
	}
	value := string(raw)
	clear(raw)
	return value, nil
}

func parseSingleLineSecret(raw, label string, allowEmpty bool) (string, error) {
	if len(raw) > maxSecretFileBytes {
		return "", fmt.Errorf("%s exceeds %d bytes", label, maxSecretFileBytes)
	}
	if !utf8.ValidString(raw) {
		return "", fmt.Errorf("%s must contain UTF-8 text", label)
	}
	value := raw
	if strings.HasSuffix(value, "\r\n") {
		value = strings.TrimSuffix(value, "\r\n")
	} else if strings.HasSuffix(value, "\n") {
		value = strings.TrimSuffix(value, "\n")
	}
	if !allowEmpty && value == "" {
		return "", fmt.Errorf("%s is empty", label)
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("%s must contain exactly one line", label)
	}
	return value, nil
}

func validateSecretFile(info os.FileInfo, path, label string) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s file is not a regular file: %s", label, path)
	}
	if info.Size() > maxSecretFileBytes {
		return fmt.Errorf("%s file exceeds %d bytes: %s", label, maxSecretFileBytes, path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%s file must be accessible only by its owner: %s", label, path)
	}
	return nil
}

func readSecretFile(path, label string) (string, error) {
	return readSecretFileWithOpen(path, label, os.Open)
}

func readSecretFileWithOpen(
	path string,
	label string,
	openFile func(string) (*os.File, error),
) (value string, returnErr error) {
	before, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect %s file at %s: %w", label, path, err)
	}
	if err := validateSecretFile(before, path, label); err != nil {
		return "", err
	}
	if !os.SameFile(before, before) {
		return "", fmt.Errorf("establish %s file identity at %s", label, path)
	}

	file, err := openFile(path)
	if err != nil {
		return "", fmt.Errorf("open %s file at %s: %w", label, path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf("close %s file at %s: %w", label, path, closeErr),
			)
		}
	}()

	after, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect open %s file at %s: %w", label, path, err)
	}
	if err := validateSecretFile(after, path, label); err != nil {
		return "", err
	}
	if !os.SameFile(before, after) {
		return "", fmt.Errorf("%s file changed while opening: %s", label, path)
	}

	raw, err := io.ReadAll(io.LimitReader(file, maxSecretFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("read %s file at %s: %w", label, path, err)
	}
	defer clear(raw)
	if len(raw) > maxSecretFileBytes {
		return "", fmt.Errorf("%s file exceeds %d bytes: %s", label, maxSecretFileBytes, path)
	}
	return parseSingleLineSecret(string(raw), label, false)
}

func resolveInstallSecrets(
	sources installSecretSources,
	terminal secretTerminal,
) (installSecrets, error) {
	var privateKey string
	var err error
	if sources.privateKeyFile != "" {
		privateKey, err = readSecretFile(sources.privateKeyFile, "private key")
	} else if terminal.Interactive() {
		privateKey, err = terminal.ReadHidden("Private key: ")
		if err == nil {
			privateKey, err = parseSingleLineSecret(privateKey, "private key", false)
		}
	} else {
		return installSecrets{}, errors.New(
			"private key input requires an interactive terminal or --private-key-file <path>",
		)
	}
	if err != nil {
		return installSecrets{}, err
	}

	var token string
	if sources.tokenFile != "" {
		token, err = readSecretFile(sources.tokenFile, "API token")
	} else if terminal.Interactive() {
		token, err = terminal.ReadHidden("API token (optional): ")
		if err == nil {
			token, err = parseSingleLineSecret(token, "API token", true)
		}
	}
	if err != nil {
		return installSecrets{}, err
	}
	return installSecrets{privateKey: privateKey, token: token}, nil
}

func validatePrivateKey(privateKey string) error {
	seed, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil {
		return fmt.Errorf("decode private key: %w", err)
	}
	defer clear(seed)
	if len(seed) != ed25519.SeedSize {
		return fmt.Errorf(
			"private key seed must be %d bytes, got %d",
			ed25519.SeedSize,
			len(seed),
		)
	}
	return nil
}

func rejectLegacySecretArguments(arguments []string) error {
	legacyOptions := map[string]string{
		"--private-key": "--private-key-file",
		"-private-key":  "--private-key-file",
		"--token":       "--token-file",
		"-token":        "--token-file",
	}
	for _, argument := range arguments {
		option := strings.SplitN(argument, "=", 2)[0]
		if replacement, exists := legacyOptions[option]; exists {
			return fmt.Errorf(
				"%s exposes credentials in process arguments; use %s or hidden terminal input",
				option,
				replacement,
			)
		}
	}
	return nil
}
