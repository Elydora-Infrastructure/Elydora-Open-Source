//go:build !darwin && !linux && !windows

package plugins

import (
	"fmt"
	"os"
)

func transactionFileIdentity(_ *os.File, info os.FileInfo) (string, error) {
	return fmt.Sprintf("unsupported:%T:%v", info.Sys(), info.Sys()), nil
}
