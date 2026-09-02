//go:build linux || darwin

package plugins

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func createPrivateTransactionDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil {
		return err
	}
	return verifyPrivateTransactionDirectory(path)
}

func ensurePrivateTransactionDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if createErr := createPrivateTransactionDirectory(path); createErr == nil {
			return nil
		} else if !errors.Is(createErr, os.ErrExist) {
			return createErr
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("transaction namespace is not a physical directory: %s", path)
	}
	return verifyPrivateTransactionDirectory(path)
}

func verifyPrivateTransactionDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("transaction namespace is not owner-only: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("transaction namespace owner does not match effective user: %s", path)
	}
	if err := hardenPrivateTransactionDirectory(path); err != nil {
		return err
	}
	info, err = os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() ||
		info.Mode().Perm() != 0700 {
		return errors.Join(fmt.Errorf("transaction namespace is not owner-only: %s", path), err)
	}
	stat, ok = info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("transaction namespace owner does not match effective user: %s", path)
	}
	return nil
}

func verifyTransactionNamespaceParent(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.Join(fmt.Errorf("transaction namespace parent is not a physical directory: %s", path), err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("transaction namespace parent owner does not match effective user: %s", path)
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("transaction namespace parent is writable by another user: %s", path)
	}
	return nil
}
