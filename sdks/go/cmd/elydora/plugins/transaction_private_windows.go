//go:build windows

package plugins

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const transactionFileAllAccess windows.ACCESS_MASK = 0x1f01ff

const transactionParentMutationAccess windows.ACCESS_MASK = windows.FILE_WRITE_DATA |
	windows.FILE_APPEND_DATA |
	windows.DELETE |
	windows.WRITE_DAC |
	windows.WRITE_OWNER |
	windows.GENERIC_WRITE |
	windows.GENERIC_ALL |
	0x00000040 // FILE_DELETE_CHILD

func createPrivateTransactionDirectory(path string) error {
	descriptor, _, err := privateTransactionSecurityDescriptor()
	if err != nil {
		return err
	}
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	if err := windows.CreateDirectory(pointer, &attributes); err != nil {
		return err
	}
	return verifyPrivateTransactionDirectory(path)
}

func ensurePrivateTransactionDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if createErr := createPrivateTransactionDirectory(path); createErr == nil {
			return nil
		} else if !errors.Is(createErr, os.ErrExist) &&
			!errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
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
	descriptor, user, err := privateTransactionSecurityDescriptor()
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|
			windows.DACL_SECURITY_INFORMATION|
			windows.PROTECTED_DACL_SECURITY_INFORMATION,
		user,
		nil,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("protect transaction namespace %s: %w", path, err)
	}
	return verifyPrivateTransactionDirectory(path)
}

func privateTransactionSecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, *windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, nil, err
	}
	sid, err := user.User.Sid.Copy()
	if err != nil {
		return nil, nil, err
	}
	sidText := sid.String()
	if sidText == "" {
		return nil, nil, fmt.Errorf("resolve current Windows user SID")
	}
	descriptor, err := windows.SecurityDescriptorFromString(
		"O:" + sidText + "D:P(A;OICI;FA;;;" + sidText + ")",
	)
	return descriptor, sid, err
}

func verifyPrivateTransactionDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("transaction namespace is not a physical directory: %s", path)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect transaction namespace DACL at %s: %w", path, err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return errors.Join(fmt.Errorf("transaction namespace owner changed: %s", path), err)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return errors.Join(fmt.Errorf("transaction namespace DACL inherits access: %s", path), err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		return errors.Join(fmt.Errorf("transaction namespace DACL is not owner-only: %s", path), err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil || ace == nil {
		return errors.Join(fmt.Errorf("inspect transaction namespace owner ACE: %s", path), err)
	}
	aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
		ace.Header.AceFlags != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE ||
		ace.Mask != transactionFileAllAccess || !aceSID.Equals(user.User.Sid) {
		return fmt.Errorf("transaction namespace DACL is not owner-only: %s", path)
	}
	return nil
}

func verifyTransactionNamespaceParent(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.Join(fmt.Errorf("transaction namespace parent is not a physical directory: %s", path), err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect transaction namespace parent owner at %s: %w", path, err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return errors.Join(
			fmt.Errorf("transaction namespace parent owner does not match current user: %s", path),
			err,
		)
	}
	if err := verifyTransactionNamespaceParentDACL(path, descriptor, user.User.Sid); err != nil {
		return err
	}
	return nil
}

func verifyTransactionNamespaceParentDACL(
	path string,
	descriptor *windows.SECURITY_DESCRIPTOR,
	currentUser *windows.SID,
) error {
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return errors.Join(fmt.Errorf("transaction namespace parent has no restrictive DACL: %s", path), err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil {
			return errors.Join(fmt.Errorf("inspect transaction namespace parent ACE at %s", path), err)
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf(
				"transaction namespace parent has unsupported DACL ACE type %d: %s",
				ace.Header.AceType,
				path,
			)
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 ||
			ace.Mask&transactionParentMutationAccess == 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() {
			return fmt.Errorf("transaction namespace parent has invalid DACL SID: %s", path)
		}
		if !sid.Equals(currentUser) && !sid.Equals(system) && !sid.Equals(administrators) {
			return fmt.Errorf(
				"transaction namespace parent grants mutation access to untrusted SID %s: %s",
				sid.String(),
				path,
			)
		}
	}
	return nil
}
