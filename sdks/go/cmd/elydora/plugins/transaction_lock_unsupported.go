//go:build !darwin && !linux && !windows

package plugins

import "os"

func lockTransactionFile(*os.File) error {
	return transactionPlatformSupported()
}

func unlockTransactionFile(*os.File) error {
	return nil
}
