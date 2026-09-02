package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

const durableTransactionVersion = 2

type durableTransactionPhase string

const (
	durablePhaseInitializing durableTransactionPhase = "initializing"
	durablePhasePrepared     durableTransactionPhase = "prepared"
	durablePhaseCommitting   durableTransactionPhase = "committing"
	durablePhaseCommitted    durableTransactionPhase = "committed"
	durablePhaseRollingBack  durableTransactionPhase = "rolling_back"
)

type durableChangeKind string

const (
	durableCreate durableChangeKind = "create"
	durableUpdate durableChangeKind = "update"
	durableDelete durableChangeKind = "delete"
)

type durableArtifact struct {
	Exists   bool   `json:"exists"`
	Identity string `json:"identity,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	Mode     uint32 `json:"mode,omitempty"`
}

type durableWorkspace struct {
	Path        string `json:"path"`
	MarkerPath  string `json:"marker_path"`
	OwnerToken  string `json:"owner_token"`
	DirectoryID string `json:"directory_id,omitempty"`
}

type durableEntry struct {
	Path         string            `json:"path"`
	Label        string            `json:"label"`
	Kind         durableChangeKind `json:"kind"`
	Workspace    string            `json:"workspace"`
	NextPath     string            `json:"next_path,omitempty"`
	OriginalPath string            `json:"original_path,omitempty"`
	DiscardPath  string            `json:"discard_path"`
	Original     durableArtifact   `json:"original"`
	Next         durableArtifact   `json:"next"`
}

type durableJournal struct {
	Version          int                     `json:"version"`
	ID               string                  `json:"id"`
	OwnerToken       string                  `json:"owner_token"`
	Label            string                  `json:"label"`
	Phase            durableTransactionPhase `json:"phase"`
	Sequence         uint64                  `json:"sequence"`
	CompletedEntries int                     `json:"completed_entries"`
	ActiveEntry      *int                    `json:"active_entry,omitempty"`
	JournalDir       string                  `json:"journal_dir"`
	Workspaces       []durableWorkspace      `json:"workspaces"`
	Entries          []durableEntry          `json:"entries"`
}

func artifactDigest(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func artifactFromSnapshot(snapshot *managedFileSnapshot) durableArtifact {
	if snapshot == nil {
		return durableArtifact{}
	}
	return durableArtifact{
		Exists:   true,
		Identity: snapshot.identity,
		SHA256:   artifactDigest(snapshot.contents),
		Mode:     uint32(snapshot.mode.Perm()),
	}
}

func artifactFromChangeOriginal(change fileChange) durableArtifact {
	if !change.existed {
		return durableArtifact{}
	}
	return durableArtifact{
		Exists:   true,
		Identity: change.originalID,
		SHA256:   artifactDigest(change.original),
		Mode:     uint32(change.originalMode.Perm()),
	}
}

func artifactMatchesSnapshot(expected durableArtifact, current *managedFileSnapshot) bool {
	if (current != nil) != expected.Exists {
		return false
	}
	if current == nil {
		return true
	}
	return current.identity == expected.Identity &&
		artifactDigest(current.contents) == expected.SHA256 &&
		sameManagedFileMode(current.mode, os.FileMode(expected.Mode))
}

func readDurableArtifact(path, label string) (*managedFileSnapshot, error) {
	return readManagedFile(path, label, maxManagedSourceBytes)
}

func requireDurableArtifact(
	path, label string,
	expected durableArtifact,
) (*managedFileSnapshot, error) {
	current, err := readDurableArtifact(path, label)
	if err != nil {
		return nil, err
	}
	if !artifactMatchesSnapshot(expected, current) {
		actual := artifactFromSnapshot(current)
		return current, fmt.Errorf(
			"%s identity, content, or mode changed at %s (expected id=%q hash=%q mode=%#o exists=%t; actual id=%q hash=%q mode=%#o exists=%t)",
			label,
			path,
			expected.Identity,
			expected.SHA256,
			expected.Mode,
			expected.Exists,
			actual.Identity,
			actual.SHA256,
			actual.Mode,
			actual.Exists,
		)
	}
	return current, nil
}
