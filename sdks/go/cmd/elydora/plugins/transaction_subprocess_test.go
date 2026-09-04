package plugins

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDurableTransactionRecoversAfterProcessTermination(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := createPrivateTransactionDirectory(home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	target := filepath.Join(t.TempDir(), "managed.json")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	command := durableWorkerCommand(t, home, "crash-update", target)
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 86 {
		t.Fatalf("crash worker = %v, output=%s", err, output)
	}
	requireTransactionContent(t, target, "next")
	if err := writeChanges(nil, "restart recovery", nil); err != nil {
		t.Fatalf("recover interrupted transaction: %v", err)
	}
	requireTransactionContent(t, target, "original")
	requireNoDurableTransactionState(t, home, filepath.Dir(target))
}

func TestDurableTransactionResumesWindowsRollbackHardlink(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows hardlink rollback state")
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := createPrivateTransactionDirectory(home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	directory := t.TempDir()
	target := filepath.Join(directory, "managed.json")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	crash := durableWorkerCommand(t, home, "crash-update", target)
	output, err := crash.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 86 {
		t.Fatalf("commit crash worker = %v, output=%s", err, output)
	}
	hardlinkCrash := durableWorkerCommand(t, home, "crash-rollback-hardlink", target)
	output, err = hardlinkCrash.CombinedOutput()
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 91 {
		t.Fatalf("rollback hardlink worker = %v, output=%s", err, output)
	}
	if err := writeChanges(nil, "hardlink restart recovery", nil); err != nil {
		t.Fatalf("resume hardlink rollback: %v", err)
	}
	requireTransactionContent(t, target, "original")
	requireNoDurableTransactionState(t, home, directory)
}

func TestDurableTransactionRecoversCompletedAndActiveEntries(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := createPrivateTransactionDirectory(home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	directory := t.TempDir()
	firstTarget := filepath.Join(directory, "first.json")
	secondTarget := filepath.Join(directory, "second.json")
	for path, value := range map[string]string{
		firstTarget: "first-original", secondTarget: "second-original",
	} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := durableWorkerCommand(t, home, "crash-multi-update", firstTarget)
	command.Env = append(command.Env, "ELYDORA_DURABLE_SECOND_TARGET="+secondTarget)
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 87 {
		t.Fatalf("multi crash worker = %v, output=%s", err, output)
	}
	requireTransactionContent(t, firstTarget, "first-next")
	requireTransactionContent(t, secondTarget, "second-next")
	if err := writeChanges(nil, "multi-entry restart recovery", nil); err != nil {
		t.Fatalf("recover interrupted multi-entry transaction: %v", err)
	}
	requireTransactionContent(t, firstTarget, "first-original")
	requireTransactionContent(t, secondTarget, "second-original")
	requireNoDurableTransactionState(t, home, directory)
}

func TestDurableTransactionRecoversCreateAndDeleteBoundaries(t *testing.T) {
	for _, testCase := range []struct {
		name, mode string
		exitCode   int
		exists     bool
	}{
		{"create", "crash-create", 88, false},
		{"delete", "crash-delete", 89, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			if err := createPrivateTransactionDirectory(home); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			directory := t.TempDir()
			target := filepath.Join(directory, "managed.json")
			if testCase.exists {
				if err := os.WriteFile(target, []byte("delete-original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			command := durableWorkerCommand(t, home, testCase.mode, target)
			output, err := command.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != testCase.exitCode {
				t.Fatalf("%s crash worker = %v, output=%s", testCase.name, err, output)
			}
			if err := writeChanges(nil, testCase.name+" restart recovery", nil); err != nil {
				t.Fatalf("recover interrupted %s: %v", testCase.name, err)
			}
			if testCase.exists {
				requireTransactionContent(t, target, "delete-original")
			} else if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("created target remains after recovery: %v", err)
			}
			requireNoDurableTransactionState(t, home, directory)
		})
	}
}

func TestDurableTransactionFinishesCommittedCleanupAfterProcessExit(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := createPrivateTransactionDirectory(home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	directory := t.TempDir()
	target := filepath.Join(directory, "managed.json")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	command := durableWorkerCommand(t, home, "crash-committed-cleanup", target)
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 90 {
		t.Fatalf("committed cleanup worker = %v, output=%s", err, output)
	}
	requireTransactionContent(t, target, "committed")
	if err := writeChanges(nil, "committed cleanup restart recovery", nil); err != nil {
		t.Fatalf("finish committed cleanup: %v", err)
	}
	requireTransactionContent(t, target, "committed")
	requireNoDurableTransactionState(t, home, directory)
}

func TestDurableTransactionNeverExposesMissingUpdateToSubprocessReader(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := createPrivateTransactionDirectory(home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	directory := t.TempDir()
	target := filepath.Join(directory, "managed.json")
	stop := filepath.Join(directory, "stop")
	if err := os.WriteFile(target, []byte("value-a"), 0600); err != nil {
		t.Fatal(err)
	}
	reader := durableWorkerCommand(t, home, "reader", target)
	var readerOutput bytes.Buffer
	reader.Stdout = &readerOutput
	reader.Stderr = &readerOutput
	reader.Env = append(reader.Env, "ELYDORA_DURABLE_STOP="+stop)
	if err := reader.Start(); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 12; index++ {
		value := "value-a"
		if index%2 == 0 {
			value = "value-b"
		}
		change, err := prepareFileChange(target, "reader target", []byte(value), 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeChanges([]*fileChange{change}, "reader visibility transaction", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(stop, []byte("stop"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reader.Wait(); err != nil {
		t.Fatalf("reader process observed invalid state: %v: %s", err, readerOutput.String())
	}
}

func TestDurableTransactionCapabilityGateLeavesTargetAndStateClean(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := createPrivateTransactionDirectory(home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	directory := t.TempDir()
	target := filepath.Join(directory, "managed.json")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	change, err := prepareFileChange(target, "capability target", []byte("next"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = writeChanges([]*fileChange{change}, "capability transaction", nil)
	if err == nil {
		requireTransactionContent(t, target, "next")
	} else {
		if !strings.Contains(err.Error(), "lacks required durable transaction primitives") {
			t.Fatalf("capability gate error = %v", err)
		}
		requireTransactionContent(t, target, "original")
	}
	requireNoDurableTransactionState(t, home, directory)
}

func TestDurableTransactionSerializesSubprocessWriters(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := createPrivateTransactionDirectory(home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	directory := t.TempDir()
	target := filepath.Join(directory, "managed.json")
	start := filepath.Join(directory, "start")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	type worker struct {
		command *exec.Cmd
		ready   string
	}
	workers := make([]worker, 0, 2)
	for index, value := range []string{"writer-one", "writer-two"} {
		ready := filepath.Join(directory, fmt.Sprintf("ready-%d", index))
		command := durableWorkerCommand(t, home, "writer", target)
		command.Env = append(
			command.Env,
			"ELYDORA_DURABLE_VALUE="+value,
			"ELYDORA_DURABLE_READY="+ready,
			"ELYDORA_DURABLE_START="+start,
		)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		workers = append(workers, worker{command: command, ready: ready})
	}
	for _, item := range workers {
		waitForDurableTestPath(t, item.ready)
	}
	if err := os.WriteFile(start, []byte("start"), 0600); err != nil {
		t.Fatal(err)
	}
	exitCodes := make([]int, 0, 2)
	for _, item := range workers {
		err := item.command.Wait()
		if err == nil {
			exitCodes = append(exitCodes, 0)
			continue
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		exitCodes = append(exitCodes, exitErr.ExitCode())
	}
	if !(exitCodes[0] == 0 && exitCodes[1] == 23 || exitCodes[0] == 23 && exitCodes[1] == 0) {
		t.Fatalf("writer exit codes = %v", exitCodes)
	}
	content, err := os.ReadFile(target)
	if err != nil || !strings.HasPrefix(string(content), "writer-") {
		t.Fatalf("serialized target = %q, %v", content, err)
	}
}

func durableWorkerCommand(t *testing.T, home, mode, target string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestDurableTransactionSubprocessWorker$")
	command.Env = append(
		os.Environ(),
		durableWorkerEnvironment+"="+mode,
		"ELYDORA_DURABLE_TARGET="+target,
		"HOME="+home,
		"USERPROFILE="+home,
	)
	return command
}

func waitForDurableTestPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Lstat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(time.Millisecond)
	}
}

func requireNoDurableTransactionState(t *testing.T, home string, targetDirectories ...string) {
	t.Helper()
	stateRoot := filepath.Join(home, ".elydora", "transactions")
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "lock" {
			t.Fatalf("durable journal remains: %s", filepath.Join(stateRoot, entry.Name()))
		}
	}
	for _, directory := range targetDirectories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".elydora-txn-") {
				t.Fatalf("durable workspace remains: %s", filepath.Join(directory, entry.Name()))
			}
		}
	}
}
