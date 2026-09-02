//go:build windows

package plugins

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func transactionFileIdentity(file *os.File, _ os.FileInfo) (string, error) {
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(
		windows.Handle(file.Fd()),
		&information,
	); err != nil {
		return "", err
	}
	index := uint64(information.FileIndexHigh)<<32 | uint64(information.FileIndexLow)
	return fmt.Sprintf("%x:%x", information.VolumeSerialNumber, index), nil
}
