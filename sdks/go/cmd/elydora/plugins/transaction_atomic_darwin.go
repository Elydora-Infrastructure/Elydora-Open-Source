//go:build darwin

package plugins

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func atomicRenameNoReplace(source, destination string) error {
	if err := unix.RenamexNp(source, destination, unix.RENAME_EXCL); err != nil {
		return err
	}
	return syncAtomicRenameDirectories(source, destination)
}

func isAtomicDestinationExists(err error) bool {
	return errors.Is(err, unix.EEXIST)
}

func atomicReplaceWithBackup(target, replacement, backup string) error {
	if replacement != backup {
		return fmt.Errorf("macOS atomic exchange requires the replacement to retain the backup")
	}
	if err := unix.RenamexNp(target, replacement, unix.RENAME_SWAP); err != nil {
		return err
	}
	return syncAtomicRenameDirectories(target, replacement)
}

func atomicReplacementUsesSeparateBackup() bool {
	return false
}

func transactionPlatformSupported() error {
	return nil
}

func syncTransactionDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
