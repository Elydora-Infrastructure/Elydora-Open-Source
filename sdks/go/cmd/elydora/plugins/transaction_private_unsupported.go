//go:build !windows && !linux && !darwin

package plugins

import "fmt"

func createPrivateTransactionDirectory(path string) error {
	return fmt.Errorf("private transaction directories are unsupported at %s", path)
}

func ensurePrivateTransactionDirectory(path string) error {
	return fmt.Errorf("private transaction directories are unsupported at %s", path)
}

func verifyPrivateTransactionDirectory(path string) error {
	return fmt.Errorf("private transaction directories are unsupported at %s", path)
}

func verifyTransactionNamespaceParent(path string) error {
	return fmt.Errorf("private transaction namespace parents are unsupported at %s", path)
}
