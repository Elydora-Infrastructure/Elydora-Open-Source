//go:build linux

package plugins

import "os"

func hardenPrivateTransactionDirectory(path string) error {
	return os.Chmod(path, 0700)
}
