// Package winexec runs Windows programs from WSL, including PowerShell
// scripts that need administrator rights.
package winexec

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf16"
)

// Runner runs a program and returns its stdout. Tests swap in a fake.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExitError is returned by ExecRunner when the program exits non-zero.
type ExitError struct {
	Name   string
	Code   int
	Stdout string
	Stderr string
}

func (e *ExitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(e.Stdout)
	}
	if msg == "" {
		return fmt.Sprintf("%s exited with code %d", e.Name, e.Code)
	}
	return fmt.Sprintf("%s exited with code %d: %s", e.Name, e.Code, msg)
}

// ExecRunner runs real processes.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return stdout.Bytes(), &ExitError{
			Name:   name,
			Code:   exitErr.ExitCode(),
			Stdout: Clean(stdout.Bytes()),
			Stderr: Clean(stderr.Bytes()),
		}
	}
	return stdout.Bytes(), err
}

// Clean strips a UTF-8 BOM, carriage returns, and surrounding whitespace from
// Windows program output.
func Clean(b []byte) string {
	b = bytes.TrimPrefix(b, []byte("\xEF\xBB\xBF"))
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\r", ""))
}

// EncodeCommand encodes a script for powershell.exe -EncodedCommand, which
// expects base64 of UTF-16LE. This sidesteps every quoting problem.
func EncodeCommand(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i] = byte(c)
		b[2*i+1] = byte(c >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// Quote returns s as a single-quoted PowerShell string literal.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
