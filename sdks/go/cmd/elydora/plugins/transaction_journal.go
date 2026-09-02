package plugins

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const maxDurableJournalBytes = 8 * 1024 * 1024

func durableTransactionStateRoot() (string, error) {
	root, err := AgentRuntimeRoot()
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(root)
	if _, statErr := os.Lstat(parent); errors.Is(statErr, os.ErrNotExist) {
		if err := EnsurePrivateDirectory(parent); err != nil {
			return "", err
		}
	} else if statErr != nil {
		return "", statErr
	}
	if err := verifyTransactionNamespaceParent(parent); err != nil {
		return "", err
	}
	if err := ensurePrivateTransactionDirectory(root); err != nil {
		return "", err
	}
	if err := syncTransactionDirectory(filepath.Dir(root)); err != nil {
		return "", fmt.Errorf("persist transaction runtime root %s: %w", root, err)
	}
	stateRoot := filepath.Join(root, "transactions")
	if err := ensurePrivateTransactionDirectory(stateRoot); err != nil {
		return "", err
	}
	if err := syncTransactionDirectory(root); err != nil {
		return "", fmt.Errorf("persist transaction state root %s: %w", stateRoot, err)
	}
	return stateRoot, nil
}

func randomTransactionToken() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func createDurableJournal(stateRoot, label string) (*durableJournal, error) {
	id, err := randomTransactionToken()
	if err != nil {
		return nil, fmt.Errorf("generate transaction ID: %w", err)
	}
	ownerToken, err := randomTransactionToken()
	if err != nil {
		return nil, fmt.Errorf("generate transaction owner token: %w", err)
	}
	directory := filepath.Join(stateRoot, "txn-"+id)
	if err := createPrivateTransactionDirectory(directory); err != nil {
		return nil, fmt.Errorf("create transaction journal namespace %s: %w", directory, err)
	}
	journal := &durableJournal{
		Version: durableTransactionVersion, ID: id, OwnerToken: ownerToken,
		Label: label, Phase: durablePhaseInitializing, JournalDir: directory,
	}
	if err := writeOwnedFile(
		filepath.Join(directory, "owner"),
		[]byte(id+":"+ownerToken+"\n"),
		0600,
	); err != nil {
		return nil, fmt.Errorf(
			"initialize transaction journal namespace %s: %w; recovery path preserved",
			directory,
			err,
		)
	}
	if err := syncTransactionDirectory(directory); err != nil {
		return nil, fmt.Errorf("sync transaction journal namespace %s: %w", directory, err)
	}
	if err := syncTransactionDirectory(stateRoot); err != nil {
		return nil, fmt.Errorf("persist transaction journal namespace %s: %w", directory, err)
	}
	return journal, nil
}

func writeOwnedFile(path string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Sync(), file.Close())
}

func appendDurableJournal(journal *durableJournal, phase durableTransactionPhase) error {
	next, err := nextDurableJournalSequence(journal.JournalDir)
	if err != nil {
		return err
	}
	journal.Sequence = next
	journal.Phase = phase
	raw, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode transaction journal: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > maxDurableJournalBytes {
		return fmt.Errorf(
			"transaction journal state exceeds %d bytes",
			maxDurableJournalBytes,
		)
	}
	path := filepath.Join(
		journal.JournalDir,
		fmt.Sprintf("state-%020d.json", journal.Sequence),
	)
	pendingPath := filepath.Join(
		journal.JournalDir,
		fmt.Sprintf("state-%020d.pending", journal.Sequence),
	)
	if err := writeOwnedFile(pendingPath, raw, 0600); err != nil {
		return fmt.Errorf("stage transaction journal state at %s: %w", pendingPath, err)
	}
	if err := syncTransactionDirectory(journal.JournalDir); err != nil {
		return fmt.Errorf("sync staged transaction journal state at %s: %w", pendingPath, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(fmt.Errorf("transaction journal state path is occupied: %s", path), err)
	}
	if err := os.Rename(pendingPath, path); err != nil {
		return fmt.Errorf("publish transaction journal state at %s: %w", path, err)
	}
	if err := syncTransactionDirectory(journal.JournalDir); err != nil {
		return fmt.Errorf("sync transaction journal state at %s: %w", path, err)
	}
	return nil
}

func nextDurableJournalSequence(directory string) (uint64, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, err
	}
	var maximum uint64
	for _, entry := range entries {
		sequence, state := durableJournalStateSequence(entry.Name())
		if !state {
			sequence, state = durableJournalPendingSequence(entry.Name())
		}
		if state && sequence > maximum {
			maximum = sequence
		}
	}
	if maximum == ^uint64(0) {
		return 0, fmt.Errorf("transaction journal sequence is exhausted at %s", directory)
	}
	return maximum + 1, nil
}

func loadDurableJournal(directory string) (*durableJournal, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	stateNames := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "state-") &&
			strings.HasSuffix(entry.Name(), ".json") {
			stateNames = append(stateNames, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(stateNames)))
	var failures []error
	for _, name := range stateNames {
		journal, readErr := readDurableJournalState(filepath.Join(directory, name))
		if readErr == nil {
			if validateErr := validateLoadedJournal(journal, directory, name); validateErr == nil {
				return journal, nil
			} else {
				readErr = validateErr
			}
		}
		failures = append(failures, readErr)
	}
	return nil, errors.Join(
		fmt.Errorf("no valid transaction state in recovery namespace %s", directory),
		errors.Join(failures...),
	)
}

func readDurableJournalState(path string) (*durableJournal, error) {
	snapshot, err := readManagedFile(
		path,
		"transaction journal state",
		maxDurableJournalBytes,
	)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, os.ErrNotExist
	}
	decoder := json.NewDecoder(bytes.NewReader(snapshot.contents))
	decoder.DisallowUnknownFields()
	var journal durableJournal
	decodeErr := decoder.Decode(&journal)
	var trailing any
	if decodeErr == nil {
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			decodeErr = fmt.Errorf("transaction journal contains trailing data")
		}
	}
	return &journal, decodeErr
}

func validateLoadedJournal(journal *durableJournal, directory, name string) error {
	if journal.Version != durableTransactionVersion || journal.ID == "" ||
		journal.OwnerToken == "" ||
		durablePathKey(journal.JournalDir) != durablePathKey(directory) {
		return fmt.Errorf("invalid transaction journal identity at %s", directory)
	}
	expectedName := fmt.Sprintf("state-%020d.json", journal.Sequence)
	if name != expectedName {
		return fmt.Errorf("transaction journal sequence does not match %s", name)
	}
	if err := validateDurableJournalProgress(journal); err != nil {
		return fmt.Errorf("invalid transaction journal progress at %s: %w", directory, err)
	}
	base := filepath.Base(directory)
	if base != "txn-"+journal.ID {
		return fmt.Errorf("transaction journal path does not match transaction ID: %s", directory)
	}
	owner, err := readManagedFile(
		filepath.Join(directory, "owner"),
		"transaction journal owner marker",
		maxDurableJournalBytes,
	)
	if err != nil || owner == nil ||
		string(owner.contents) != journal.ID+":"+journal.OwnerToken+"\n" {
		return errors.Join(fmt.Errorf("transaction journal owner marker changed: %s", directory), err)
	}
	if err := verifyPrivateTransactionDirectory(directory); err != nil {
		return err
	}
	for _, workspace := range journal.Workspaces {
		if workspace.OwnerToken != journal.OwnerToken {
			return fmt.Errorf("transaction workspace owner token mismatch: %s", workspace.Path)
		}
		if !filepath.IsAbs(workspace.Path) ||
			filepath.Base(workspace.Path) != ".elydora-txn-"+journal.ID ||
			filepath.Clean(workspace.MarkerPath) != filepath.Join(workspace.Path, "owner") {
			return fmt.Errorf("invalid transaction workspace path: %s", workspace.Path)
		}
	}
	workspacePaths := make(map[string]bool)
	for _, workspace := range journal.Workspaces {
		workspacePaths[durablePathKey(workspace.Path)] = true
	}
	for _, entry := range journal.Entries {
		if !filepath.IsAbs(entry.Path) || !workspacePaths[durablePathKey(entry.Workspace)] ||
			durablePathKey(filepath.Dir(entry.Path)) != durablePathKey(filepath.Dir(entry.Workspace)) {
			return fmt.Errorf("invalid durable transaction target: %s", entry.Path)
		}
		for _, artifactPath := range []string{entry.NextPath, entry.OriginalPath, entry.DiscardPath} {
			if artifactPath != "" &&
				durablePathKey(filepath.Dir(artifactPath)) != durablePathKey(entry.Workspace) {
				return fmt.Errorf("transaction artifact escapes workspace: %s", artifactPath)
			}
		}
	}
	return nil
}

func validateDurableJournalProgress(journal *durableJournal) error {
	if journal.CompletedEntries < 0 || journal.CompletedEntries > len(journal.Entries) {
		return fmt.Errorf("completed entry count %d is outside transaction bounds", journal.CompletedEntries)
	}
	if journal.ActiveEntry != nil {
		if *journal.ActiveEntry < 0 || *journal.ActiveEntry >= len(journal.Entries) {
			return fmt.Errorf("active entry %d is outside transaction bounds", *journal.ActiveEntry)
		}
		if *journal.ActiveEntry != journal.CompletedEntries {
			return fmt.Errorf(
				"active entry %d does not follow %d completed entries",
				*journal.ActiveEntry,
				journal.CompletedEntries,
			)
		}
	}
	switch journal.Phase {
	case durablePhaseInitializing, durablePhasePrepared:
		if journal.CompletedEntries != 0 || journal.ActiveEntry != nil {
			return fmt.Errorf("phase %s contains commit progress", journal.Phase)
		}
	case durablePhaseCommitting, durablePhaseRollingBack:
	case durablePhaseCommitted:
		if journal.CompletedEntries != len(journal.Entries) || journal.ActiveEntry != nil {
			return fmt.Errorf("committed phase is incomplete")
		}
	default:
		return fmt.Errorf("unknown transaction phase %q", journal.Phase)
	}
	return nil
}

func durableJournalStateSequence(name string) (uint64, bool) {
	if !strings.HasPrefix(name, "state-") || !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, "state-"), ".json")
	if len(value) != 20 {
		return 0, false
	}
	sequence, err := strconv.ParseUint(value, 10, 64)
	return sequence, err == nil
}

func durableJournalPendingSequence(name string) (uint64, bool) {
	if !strings.HasPrefix(name, "state-") || !strings.HasSuffix(name, ".pending") {
		return 0, false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, "state-"), ".pending")
	if len(value) != 20 {
		return 0, false
	}
	sequence, err := strconv.ParseUint(value, 10, 64)
	return sequence, err == nil
}
