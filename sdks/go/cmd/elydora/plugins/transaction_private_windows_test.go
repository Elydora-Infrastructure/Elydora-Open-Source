//go:build windows

package plugins

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestTransactionNamespaceParentRejectsUntrustedMutationACE(t *testing.T) {
	path := t.TempDir()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString(
		"O:" + user.User.Sid.String() +
			"D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")" +
			"(A;;0x00000040;;;WD)",
	)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|
			windows.DACL_SECURITY_INFORMATION|
			windows.PROTECTED_DACL_SECURITY_INFORMATION,
		user.User.Sid,
		nil,
		dacl,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyTransactionNamespaceParent(path); err == nil ||
		!strings.Contains(err.Error(), "untrusted SID") {
		t.Fatalf("untrusted parent DACL error = %v", err)
	}
}
