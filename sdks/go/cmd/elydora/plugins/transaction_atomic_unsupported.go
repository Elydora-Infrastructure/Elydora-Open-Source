//go:build !darwin && !linux && !windows

package plugins

import (
	"fmt"
	"runtime"
)

func atomicRenameNoReplace(_, _ string) error {
	return fmt.Errorf("atomic no-replace rename is unsupported on %s", runtime.GOOS)
}

func isAtomicDestinationExists(error) bool {
	return false
}

func atomicReplaceWithBackup(_, _, _ string) error {
	return fmt.Errorf("atomic file replacement is unsupported on %s", runtime.GOOS)
}

func atomicReplacementUsesSeparateBackup() bool {
	return false
}

func transactionPlatformSupported() error {
	return fmt.Errorf(
		"durable file transactions support Windows, Linux, and macOS; current platform is %s",
		runtime.GOOS,
	)
}

func syncTransactionDirectory(string) error {
	return transactionPlatformSupported()
}
