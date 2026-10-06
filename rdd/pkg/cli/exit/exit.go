// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package exit defines distinct CLI exit codes so callers can tell apart
// admission rejection, wait timeout, and generic internal errors.
package exit

import (
	"context"
	"errors"
	"os/exec"
)

// Predefined exit codes. 0 and 1 are the defaults handled by main; 2 is
// reserved for cobra usage errors, so named codes start at 3.  This list should
// use explicit values, as this is a public API; see `cmd_app.md`.
const (
	// CodeRejected indicates the API server's admission controller rejected the request.
	CodeRejected = 3

	// CodeTimeout indicates the wait deadline expired before the desired state was reached.
	CodeTimeout = 4

	// CodeIncompatibleServer indicates the server is incompatible with the client.
	CodeIncompatibleServer = 5
)

// Error is an error that carries a process exit code for main to return.
type Error struct {
	Code int
	Err  error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap exposes the wrapped error to errors.Is and errors.As.
func (e *Error) Unwrap() error {
	return e.Err
}

// Rejected wraps err with [CodeRejected].
func Rejected(err error) *Error {
	return &Error{Code: CodeRejected, Err: err}
}

// Timeout wraps err with [CodeTimeout].
func Timeout(err error) *Error {
	return &Error{Code: CodeTimeout, Err: err}
}

// Classify wraps err with [CodeTimeout] when err wraps
// [context.DeadlineExceeded]. Already-classified errors and unrelated
// errors pass through unchanged, so callers may Classify repeatedly.
func Classify(err error) error {
	if _, ok := errors.AsType[*Error](err); ok {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Timeout(err)
	}
	return err
}

// ChildExit returns an *Error that makes main exit with the exit code of a
// child process that failed, or nil when err does not wrap an [exec.ExitError].
// The *Error has no message, because the child has reported its own failure.
func ChildExit(err error) *Error {
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return nil
	}
	// A signal-killed child has no exit code. ExitCode() returns -1, which
	// os.Exit maps to 255 rather than the shell's 128+signal.
	return &Error{Code: exitErr.ExitCode()}
}
