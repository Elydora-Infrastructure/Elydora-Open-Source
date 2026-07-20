//go:build !windows

package main

import (
	"os"

	"golang.org/x/term"
)

func isInteractiveTerminal(file *os.File) bool {
	return term.IsTerminal(int(file.Fd()))
}

func readHiddenLine(file *os.File) ([]byte, error) {
	return term.ReadPassword(int(file.Fd()))
}
