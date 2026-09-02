//go:build linux

package plugins

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func atomicRenameNoReplace(source, destination string) error {
	if err := unix.Renameat2(
		unix.AT_FDCWD,
		source,
		unix.AT_FDCWD,
		destination,
		unix.RENAME_NOREPLACE,
	); err != nil {
		return err
	}
	return syncAtomicRenameDirectories(source, destination)
}

func isAtomicDestinationExists(err error) bool {
	return errors.Is(err, unix.EEXIST)
}

func atomicReplaceWithBackup(target, replacement, backup string) error {
	if replacement != backup {
		return fmt.Errorf("Linux atomic exchange requires the replacement to retain the backup")
	}
	if err := unix.Renameat2(
		unix.AT_FDCWD,
		target,
		unix.AT_FDCWD,
		replacement,
		unix.RENAME_EXCHANGE,
	); err != nil {
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
