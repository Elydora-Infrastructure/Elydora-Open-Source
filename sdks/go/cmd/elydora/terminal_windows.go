//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

const (
	windowsEnableProcessedInput  = 0x0001
	windowsEnableLineInput       = 0x0002
	windowsEnableEchoInput       = 0x0004
	windowsEnableProcessedOutput = 0x0001
)

var setConsoleMode = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")

func isInteractiveTerminal(file *os.File) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(file.Fd()), &mode) == nil
}

func readHiddenLine(file *os.File) (raw []byte, returnErr error) {
	handle := syscall.Handle(file.Fd())
	var originalMode uint32
	if err := syscall.GetConsoleMode(handle, &originalMode); err != nil {
		return nil, fmt.Errorf("read console mode: %w", err)
	}
	hiddenMode := originalMode &^ (windowsEnableEchoInput | windowsEnableLineInput)
	hiddenMode |= windowsEnableProcessedInput | windowsEnableProcessedOutput
	if err := updateConsoleMode(handle, hiddenMode); err != nil {
		return nil, fmt.Errorf("disable terminal echo: %w", err)
	}
	defer func() {
		if err := updateConsoleMode(handle, originalMode); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("restore console mode: %w", err))
		}
	}()

	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return nil, fmt.Errorf("get current process: %w", err)
	}
	var duplicate syscall.Handle
	if err := syscall.DuplicateHandle(
		process,
		handle,
		process,
		&duplicate,
		0,
		false,
		syscall.DUPLICATE_SAME_ACCESS,
	); err != nil {
		return nil, fmt.Errorf("duplicate terminal handle: %w", err)
	}
	input := os.NewFile(uintptr(duplicate), file.Name())
	if input == nil {
		_ = syscall.CloseHandle(duplicate)
		return nil, errors.New("create terminal reader from duplicate handle")
	}
	defer func() {
		if err := input.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close terminal reader: %w", err))
		}
	}()

	return readWindowsPasswordLine(input)
}

func updateConsoleMode(handle syscall.Handle, mode uint32) error {
	result, _, callErr := setConsoleMode.Call(uintptr(handle), uintptr(mode))
	if result != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}

func readWindowsPasswordLine(reader io.Reader) ([]byte, error) {
	var value []byte
	var buffer [1]byte
	for {
		read, err := reader.Read(buffer[:])
		if read > 0 {
			switch buffer[0] {
			case '\b':
				if len(value) > 0 {
					value = value[:len(value)-1]
				}
			case '\r':
				return value, nil
			case '\n':
			default:
				value = append(value, buffer[0])
				if len(value) > maxSecretFileBytes {
					clear(value)
					return nil, fmt.Errorf("hidden input exceeds %d bytes", maxSecretFileBytes)
				}
			}
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(value) > 0 {
				return value, nil
			}
			clear(value)
			return nil, err
		}
	}
}
