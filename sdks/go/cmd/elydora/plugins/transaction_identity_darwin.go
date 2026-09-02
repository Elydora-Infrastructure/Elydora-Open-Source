//go:build darwin

package plugins

import (
	"fmt"
	"os"
	"syscall"
)

func transactionFileIdentity(_ *os.File, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("macOS file identity is unavailable")
	}
	return fmt.Sprintf("%x:%x", uint64(stat.Dev), stat.Ino), nil
}
