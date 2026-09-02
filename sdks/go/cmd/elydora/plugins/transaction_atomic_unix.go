//go:build linux || darwin

package plugins

import (
	"errors"
	"path/filepath"
)

func syncAtomicRenameDirectories(source, destination string) error {
	sourceDirectory := filepath.Dir(source)
	destinationDirectory := filepath.Dir(destination)
	destinationErr := syncTransactionDirectory(destinationDirectory)
	if durablePathKey(sourceDirectory) == durablePathKey(destinationDirectory) {
		return destinationErr
	}
	return errors.Join(destinationErr, syncTransactionDirectory(sourceDirectory))
}
