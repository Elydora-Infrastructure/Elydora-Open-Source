package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type kiroIdeDirectoryIdentity struct {
	path  string
	label string
	info  os.FileInfo
}

func snapshotKiroIdeDirectory(path, label string) (kiroIdeDirectoryIdentity, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return kiroIdeDirectoryIdentity{path: path, label: label}, nil
	}
	if err != nil {
		return kiroIdeDirectoryIdentity{}, fmt.Errorf("inspect %s at %s: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return kiroIdeDirectoryIdentity{}, fmt.Errorf("%s is not a physical directory: %s", label, path)
	}
	return kiroIdeDirectoryIdentity{path: path, label: label, info: info}, nil
}

func (identity kiroIdeDirectoryIdentity) assertExpected(operation string) error {
	current, err := os.Lstat(identity.path)
	if os.IsNotExist(err) && identity.info == nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf(
			"%s changed during %s: %s: %w",
			identity.label,
			operation,
			identity.path,
			err,
		)
	}
	if identity.info == nil || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() ||
		!os.SameFile(identity.info, current) {
		return fmt.Errorf(
			"%s changed during %s: %s",
			identity.label,
			operation,
			identity.path,
		)
	}
	return nil
}

func assertKiroIdeDirectoryStates(
	directories []kiroIdeDirectoryIdentity,
	operation string,
) error {
	for _, directory := range directories {
		if err := directory.assertExpected(operation); err != nil {
			return err
		}
	}
	return nil
}

func establishKiroIdeDirectory(
	initial kiroIdeDirectoryIdentity,
	operation string,
	private bool,
) (kiroIdeDirectoryIdentity, error) {
	if err := initial.assertExpected(operation); err != nil {
		return kiroIdeDirectoryIdentity{}, err
	}
	if initial.info == nil {
		if err := os.Mkdir(initial.path, 0700); err != nil {
			return kiroIdeDirectoryIdentity{}, fmt.Errorf(
				"%s changed during %s before Elydora could create it: %s: %w",
				initial.label,
				operation,
				initial.path,
				err,
			)
		}
	} else if private && runtime.GOOS != "windows" {
		if err := os.Chmod(initial.path, 0700); err != nil {
			return kiroIdeDirectoryIdentity{}, fmt.Errorf(
				"restrict %s at %s: %w",
				initial.label,
				initial.path,
				err,
			)
		}
	}
	current, err := snapshotKiroIdeDirectory(initial.path, initial.label)
	if err != nil {
		return kiroIdeDirectoryIdentity{}, err
	}
	if current.info == nil {
		return kiroIdeDirectoryIdentity{}, fmt.Errorf(
			"%s is missing after creation: %s",
			initial.label,
			initial.path,
		)
	}
	if initial.info != nil && !os.SameFile(initial.info, current.info) {
		return kiroIdeDirectoryIdentity{}, fmt.Errorf(
			"%s changed during %s: %s",
			initial.label,
			operation,
			initial.path,
		)
	}
	return current, nil
}

func kiroIdePathInside(directory, candidate string) bool {
	relative, err := filepath.Rel(directory, candidate)
	if err != nil || filepath.IsAbs(relative) || relative == ".." {
		return false
	}
	if strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return false
	}
	if runtime.GOOS == "windows" {
		return !strings.HasPrefix(strings.ToLower(relative), ".."+string(os.PathSeparator))
	}
	return true
}

func guardedKiroIdeRename(
	rename renameFunc,
	directories []kiroIdeDirectoryIdentity,
) renameFunc {
	if rename == nil {
		rename = atomicRenameNoReplace
	}
	return func(source, destination string) error {
		relevant := make([]kiroIdeDirectoryIdentity, 0, len(directories))
		for _, directory := range directories {
			if kiroIdePathInside(directory.path, source) ||
				kiroIdePathInside(directory.path, destination) {
				relevant = append(relevant, directory)
			}
		}
		for _, directory := range relevant {
			if err := directory.assertExpected("Kiro IDE transaction"); err != nil {
				return err
			}
		}
		if err := rename(source, destination); err != nil {
			return err
		}
		for _, directory := range relevant {
			if err := directory.assertExpected("Kiro IDE transaction"); err != nil {
				return err
			}
		}
		return nil
	}
}

func guardedKiroIdeTransactionOps(
	rename renameFunc,
	directories []kiroIdeDirectoryIdentity,
) transactionFileOps {
	if rename != nil {
		return transactionFileOps{
			moveNoReplace:       guardedKiroIdeRename(rename, directories),
			compensateNoReplace: guardedKiroIdeRename(atomicRenameNoReplace, directories),
		}
	}
	return transactionFileOps{
		moveNoReplace:       guardedKiroIdeRename(atomicRenameNoReplace, directories),
		compensateNoReplace: guardedKiroIdeRename(atomicRenameNoReplace, directories),
		replaceWithBackup:   guardedKiroIdeReplace(directories),
		production:          true,
	}
}

func guardedKiroIdeReplace(
	directories []kiroIdeDirectoryIdentity,
) replaceWithBackupFunc {
	return func(target, replacement, backup string) error {
		relevant := make([]kiroIdeDirectoryIdentity, 0, len(directories))
		for _, directory := range directories {
			if kiroIdePathInside(directory.path, target) ||
				kiroIdePathInside(directory.path, replacement) ||
				kiroIdePathInside(directory.path, backup) {
				relevant = append(relevant, directory)
			}
		}
		if err := assertKiroIdeDirectoryStates(relevant, "Kiro IDE transaction"); err != nil {
			return err
		}
		if err := atomicReplaceWithBackup(target, replacement, backup); err != nil {
			return err
		}
		return assertKiroIdeDirectoryStates(relevant, "Kiro IDE transaction")
	}
}

func kiroIdeDirectoryKey(path string) string {
	key := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(key)
	}
	return key
}

func kiroIdeInitialDirectory(
	prepared *kiroIdePreparedTransaction,
	path string,
) (kiroIdeDirectoryIdentity, error) {
	key := kiroIdeDirectoryKey(path)
	for _, directory := range prepared.initialDirectories {
		if kiroIdeDirectoryKey(directory.path) == key {
			return directory, nil
		}
	}
	return kiroIdeDirectoryIdentity{}, fmt.Errorf(
		"Kiro IDE transaction is missing a directory snapshot for %s",
		path,
	)
}

func appendKiroIdeDirectory(
	directories []kiroIdeDirectoryIdentity,
	directory kiroIdeDirectoryIdentity,
) []kiroIdeDirectoryIdentity {
	key := kiroIdeDirectoryKey(directory.path)
	for index, existing := range directories {
		if kiroIdeDirectoryKey(existing.path) == key {
			directories[index] = directory
			return directories
		}
	}
	return append(directories, directory)
}

func kiroIdeDirectoryPreconditions(
	directories []kiroIdeDirectoryIdentity,
) []filePrecondition {
	conditions := make([]filePrecondition, 0, len(directories))
	for _, directory := range directories {
		if directory.info == nil {
			continue
		}
		conditions = append(conditions, filePrecondition{
			filePath:  directory.path,
			label:     directory.label,
			directory: directory.info,
		})
	}
	return conditions
}
