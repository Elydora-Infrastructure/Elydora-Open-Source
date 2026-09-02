//go:build darwin

package plugins

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPrivateTransactionDirectoryRemovesMacOSACL(t *testing.T) {
	path := t.TempDir()
	if output, err := exec.Command(
		"/bin/chmod",
		"+a",
		"everyone allow list,search",
		path,
	).CombinedOutput(); err != nil {
		t.Fatalf("add test ACL: %v: %s", err, output)
	}
	if err := verifyPrivateTransactionDirectory(path); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("/bin/ls", "-lde", path).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect test ACL: %v: %s", err, output)
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 || strings.HasSuffix(fields[0], "+") {
		t.Fatalf("ACL remains after hardening: %s", output)
	}
}
