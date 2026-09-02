//go:build darwin || linux

package plugins

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockTransactionFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}

func unlockTransactionFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
