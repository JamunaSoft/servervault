// Package execx runs external commands safely and cancellably.
//
// Every command is invoked as an argv slice via os/exec, which never passes
// through a shell — arguments are never concatenated into a shell string,
// so caller-supplied values (paths, hostnames, database names) cannot be
// interpreted as shell syntax. Every call takes a context.Context so a
// caller can cancel or time out a running command.
package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Result holds the outcome of a completed command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run executes name with args under ctx and waits for it to complete. A
// non-zero exit code is returned via Result.ExitCode and as a non-nil error;
// callers that only care whether the command ran (regardless of exit code)
// can inspect the returned Result even when err is non-nil.
func Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: cmd.ProcessState.ExitCode(),
	}

	// Prefer the context's error over the raw process-kill error (e.g.
	// "signal: killed") so callers can reliably detect cancellation and
	// timeouts with errors.Is(err, context.Canceled/DeadlineExceeded).
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, fmt.Errorf("execx: run %q: %w", name, ctxErr)
	}
	if err != nil {
		return result, fmt.Errorf("execx: run %q: %w", name, err)
	}
	return result, nil
}

// CommandChecker reports whether a named command is available. It exists so
// callers (such as internal/doctor) can depend on an interface instead of
// the real filesystem lookup, making "required commands" checks testable
// without needing the real binaries installed.
type CommandChecker interface {
	LookPath(name string) (string, error)
}

// PathChecker is the default CommandChecker, backed by exec.LookPath.
type PathChecker struct{}

// LookPath reports the resolved path of name, or an error if it isn't found
// on PATH.
func (PathChecker) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
