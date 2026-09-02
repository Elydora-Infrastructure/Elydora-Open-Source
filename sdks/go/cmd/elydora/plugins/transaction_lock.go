package plugins

import (
	"errors"
	"fmt"
	"os"
)

type durableTransactionLock struct {
	file *os.File
	path string
}

func acquireDurableTransactionLock(root string) (*durableTransactionLock, error) {
	path := root + string(os.PathSeparator) + "lock"
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open transaction lock at %s: %w", path, err)
	}
	fail := func(cause error) (*durableTransactionLock, error) {
		return nil, errors.Join(cause, file.Close())
	}
	opened, err := file.Stat()
	if err != nil {
		return fail(fmt.Errorf("inspect open transaction lock at %s: %w", path, err))
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fail(fmt.Errorf("inspect transaction lock at %s: %w", path, err))
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() ||
		!os.SameFile(opened, current) {
		return fail(fmt.Errorf("transaction lock is not a stable physical file: %s", path))
	}
	if err := lockTransactionFile(file); err != nil {
		return fail(fmt.Errorf("lock transactions at %s: %w", path, err))
	}
	current, err = os.Lstat(path)
	if err != nil || current.Mode()&os.ModeSymlink != 0 ||
		!current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return fail(errors.Join(
			fmt.Errorf("transaction lock changed while acquiring: %s", path),
			err,
			unlockTransactionFile(file),
		))
	}
	return &durableTransactionLock{file: file, path: path}, nil
}

func (lock *durableTransactionLock) close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	file := lock.file
	lock.file = nil
	return errors.Join(unlockTransactionFile(file), file.Close())
}
