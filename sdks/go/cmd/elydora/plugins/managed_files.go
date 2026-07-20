package plugins

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const maxManagedSourceBytes = 2 * 1024 * 1024

type managedFileSnapshot struct {
	contents []byte
	info     os.FileInfo
	mode     os.FileMode
}

func readManagedFile(
	filePath string,
	label string,
	maximumBytes int64,
) (*managedFileSnapshot, error) {
	before, err := os.Lstat(filePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect %s at %s: %w", label, filePath, err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%s path is not a physical file: %s", label, filePath)
	}
	if before.Size() > maximumBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes: %s", label, maximumBytes, filePath)
	}

	file, err := os.Open(filePath) // #nosec G304 -- callers provide confined managed paths.
	if err != nil {
		return nil, fmt.Errorf("open %s at %s: %w", label, filePath, err)
	}
	after, statErr := file.Stat()
	if statErr != nil {
		return nil, errors.Join(
			fmt.Errorf("inspect open %s at %s: %w", label, filePath, statErr),
			file.Close(),
		)
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, errors.Join(
			fmt.Errorf("%s changed while opening: %s", label, filePath),
			file.Close(),
		)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		var failure error
		if readErr != nil {
			failure = fmt.Errorf("read %s at %s: %w", label, filePath, readErr)
		}
		return nil, errors.Join(
			failure,
			closeErr,
		)
	}
	if int64(len(raw)) > maximumBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes: %s", label, maximumBytes, filePath)
	}
	return &managedFileSnapshot{
		contents: raw,
		info:     after,
		mode:     after.Mode().Perm(),
	}, nil
}

func managedPhysicalFileExists(path, label string, maximumBytes int64) (bool, error) {
	snapshot, err := readManagedFile(path, label, maximumBytes)
	return snapshot != nil, err
}

func managedPhysicalDirectoryExists(path, label string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s at %s: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("%s is not a physical directory: %s", label, path)
	}
	return true, nil
}

func ensureManagedDirectory(path, label string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("create %s at %s: %w", label, path, err)
	}
	exists, err := managedPhysicalDirectoryExists(path, label)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s is missing: %s", label, path)
	}
	return nil
}
