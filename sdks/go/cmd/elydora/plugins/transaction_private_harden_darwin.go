//go:build darwin

package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func hardenPrivateTransactionDirectory(path string) error {
	output, err := exec.Command("/bin/chmod", "-N", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"remove inherited ACL from transaction namespace %s: %w: %s",
			path,
			err,
			strings.TrimSpace(string(output)),
		)
	}
	return os.Chmod(path, 0700)
}
