//go:build windows

package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadWindowsPasswordLineHandlesConsoleInput(t *testing.T) {
	raw, err := readWindowsPasswordLine(strings.NewReader("secx\bret\n\rignored"))
	if err != nil {
		t.Fatalf("read Windows password line: %v", err)
	}
	if string(raw) != "secret" {
		t.Fatalf("password = %q, want secret", raw)
	}
}

func TestReadWindowsPasswordLineReturnsFinalLineAtEOF(t *testing.T) {
	raw, err := readWindowsPasswordLine(strings.NewReader("secret"))
	if err != nil {
		t.Fatalf("read Windows password line: %v", err)
	}
	if string(raw) != "secret" {
		t.Fatalf("password = %q, want secret", raw)
	}
}

func TestReadWindowsPasswordLineRejectsOversizedInput(t *testing.T) {
	raw, err := readWindowsPasswordLine(bytes.NewReader(
		bytes.Repeat([]byte{'x'}, maxSecretFileBytes+1),
	))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("read Windows password line error = %v", err)
	}
	if raw != nil {
		t.Fatalf("oversized password remained in memory")
	}
}

func TestReadWindowsPasswordLineReturnsReaderFailure(t *testing.T) {
	expected := errors.New("injected read failure")
	raw, err := readWindowsPasswordLine(&partialErrorReader{err: expected})
	if !errors.Is(err, expected) {
		t.Fatalf("read Windows password line error = %v", err)
	}
	if raw != nil {
		t.Fatalf("unexpected password bytes: %q", raw)
	}
}

type partialErrorReader struct {
	err      error
	returned bool
}

func (reader *partialErrorReader) Read(buffer []byte) (int, error) {
	if !reader.returned {
		reader.returned = true
		buffer[0] = 's'
		return 1, nil
	}
	return 0, reader.err
}

var _ io.Reader = &partialErrorReader{}
