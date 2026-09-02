//go:build windows

package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

func atomicRenameNoReplace(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return err
	}
	return syncWindowsRenameDirectories(source, destination)
}

func isAtomicDestinationExists(err error) bool {
	return errors.Is(err, windows.ERROR_ALREADY_EXISTS) ||
		errors.Is(err, windows.ERROR_FILE_EXISTS)
}

func atomicReplaceWithBackup(target, replacement, backup string) error {
	backupPointer, err := windows.UTF16PtrFromString(backup)
	if err != nil {
		return err
	}
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	if err := ensureWindowsAtomicBackup(
		target,
		backup,
		targetPointer,
		backupPointer,
	); err != nil {
		return err
	}
	replacementPointer, err := windows.UTF16PtrFromString(replacement)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = windows.MoveFileEx(
			replacementPointer,
			targetPointer,
			windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
		)
		if err == nil {
			return syncWindowsRenameDirectories(replacement, target)
		}
		if time.Now().After(deadline) ||
			!errors.Is(err, windows.ERROR_ACCESS_DENIED) &&
				!errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}

func ensureWindowsAtomicBackup(
	target, backup string,
	targetPointer, backupPointer *uint16,
) error {
	err := windows.CreateHardLink(backupPointer, targetPointer, 0)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) &&
		!errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return err
	}
	if err != nil {
		targetInfo, targetErr := os.Lstat(target)
		backupInfo, backupErr := os.Lstat(backup)
		if targetErr != nil || backupErr != nil ||
			targetInfo.Mode()&os.ModeSymlink != 0 ||
			backupInfo.Mode()&os.ModeSymlink != 0 ||
			!targetInfo.Mode().IsRegular() || !backupInfo.Mode().IsRegular() ||
			!os.SameFile(targetInfo, backupInfo) {
			return errors.Join(
				fmt.Errorf("atomic backup path is occupied by another object: %s", backup),
				targetErr,
				backupErr,
			)
		}
	}
	if err := syncTransactionDirectory(filepath.Dir(backup)); err != nil {
		return fmt.Errorf("flush atomic backup directory: %w", err)
	}
	return nil
}

func atomicReplacementUsesSeparateBackup() bool {
	return true
}

func transactionPlatformSupported() error {
	return nil
}

func syncWindowsRenameDirectories(source, destination string) error {
	destinationDirectory := filepath.Dir(destination)
	destinationErr := syncTransactionDirectory(destinationDirectory)
	if durablePathKey(filepath.Dir(source)) == durablePathKey(destinationDirectory) {
		return destinationErr
	}
	return errors.Join(destinationErr, syncTransactionDirectory(filepath.Dir(source)))
}

func syncTransactionDirectory(path string) error {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		pointer,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_WRITE_THROUGH,
		0,
	)
	if err != nil {
		return fmt.Errorf("open directory for durable flush at %s: %w", path, err)
	}
	flushErr := windows.FlushFileBuffers(handle)
	closeErr := windows.CloseHandle(handle)
	if flushErr != nil {
		flushErr = fmt.Errorf("flush directory metadata at %s: %w", path, flushErr)
	}
	return errors.Join(flushErr, closeErr)
}
