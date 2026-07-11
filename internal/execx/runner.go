package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// gracePeriod is how long a canceled command is given to exit after
// receiving SIGTERM before Runner escalates to SIGKILL. Giving restic in
// particular a chance to release its own repository lock cleanly on
// cancellation is worth a short, bounded wait.
const gracePeriod = 5 * time.Second

// RunOptions configures one command invocation. Name and Args are always
// passed to the OS as a literal argv slice — never concatenated into a
// shell string — so caller-supplied values can never be interpreted as
// shell syntax.
type RunOptions struct {
	Name string
	Args []string

	// Env is appended to the current process's inherited environment
	// (PATH, etc. are preserved); nil means "inherit only, add nothing."
	// Callers never need to pass a shell string to set these.
	Env []string

	// Stdin is streamed to the process if non-nil. The zero value means
	// the process gets no stdin at all — never the calling process's own
	// stdin — so a subprocess (e.g. a misconfigured `sudo`) can never
	// block waiting on input nor observe whatever was on our stdin.
	Stdin io.Reader

	// Stdout is streamed from the process as it's produced, if non-nil;
	// otherwise discarded.
	Stdout io.Writer

	// Stderr, if non-nil, is written to as the process produces output.
	// Callers that want a bounded capture for error messages should pass
	// something like a size-limited writer or a plain bytes.Buffer and
	// truncate it themselves before including it in an error.
	Stderr io.Writer
}

// Runner executes external commands. Packages that shell out (internal/
// restic, internal/postgres) depend on this interface rather than calling
// os/exec directly, so tests can substitute a fake without the real
// binaries installed — see each package's tests for the fake
// implementation used.
type Runner interface {
	// Run executes opts.Name with opts.Args under ctx and waits for it to
	// complete. A non-zero exit is returned as an error that unwraps to
	// *ExitError via errors.As, so callers can classify it (see
	// internal/restic's exit code table). A canceled or expired ctx is
	// returned as an error that unwraps to the ctx's own error via
	// errors.Is.
	Run(ctx context.Context, opts RunOptions) error
}

// DefaultRunner is the real Runner, backed by os/exec.
type DefaultRunner struct{}

// Run implements Runner.
func (DefaultRunner) Run(ctx context.Context, opts RunOptions) error {
	// Check before starting, too: no point launching a subprocess for a
	// context that's already done.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("execx: run %q: %w", opts.Name, err)
	}

	cmd := exec.CommandContext(ctx, opts.Name, opts.Args...)
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr

	// Give the process a chance to exit cleanly (SIGTERM) before it's
	// force-killed on cancellation, so e.g. restic can release its own
	// internal repository lock instead of leaving it stuck.
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = gracePeriod

	err := cmd.Run()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("execx: run %q: %w", opts.Name, ctxErr)
	}
	if err != nil {
		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		return fmt.Errorf("execx: run %q: %w", opts.Name, &ExitError{Code: code, Err: err})
	}
	return nil
}

// ExitError wraps a subprocess's non-zero exit. Code is -1 when the
// process never produced an exit code at all (e.g. it couldn't be
// started).
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d: %v", e.Code, e.Err)
}

func (e *ExitError) Unwrap() error { return e.Err }

// Run executes name with args under ctx, capturing stdout/stderr in
// memory. It is a convenience wrapper around DefaultRunner for simple,
// small-output commands (version checks, short queries); callers that need
// to stream large output or wire two commands together via a pipe should
// use DefaultRunner (or an injected Runner) with RunOptions directly.
func Run(ctx context.Context, name string, args ...string) (Result, error) {
	var stdout, stderr bytes.Buffer
	err := DefaultRunner{}.Run(ctx, RunOptions{
		Name:   name,
		Args:   args,
		Stdout: &stdout,
		Stderr: &stderr,
	})

	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.Code
		} else {
			result.ExitCode = -1
		}
	}
	return result, err
}
