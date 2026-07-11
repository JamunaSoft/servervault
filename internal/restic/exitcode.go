package restic

import (
	"errors"
	"fmt"

	"github.com/JamunaSoft/servervault/internal/execx"
)

// ExitCode classifies restic's documented process exit codes.
type ExitCode int

const (
	// ExitSuccess: the command completed with no errors.
	ExitSuccess ExitCode = 0
	// ExitGenericError: an unclassified failure.
	ExitGenericError ExitCode = 1
	// ExitInvalidUsage: restic was invoked incorrectly (a ServerVault bug).
	ExitInvalidUsage ExitCode = 2
	// ExitBackupIncomplete: `restic backup` only -- some source files
	// could not be read (e.g. permission denied, vanished mid-read).
	// This is common and often expected; callers may choose to treat it
	// as a warning rather than a hard failure.
	ExitBackupIncomplete ExitCode = 3
	// ExitRepositoryNotFound: the repository does not exist at the
	// configured location.
	ExitRepositoryNotFound ExitCode = 10
	// ExitLockFailed: another restic process (possibly on another host
	// sharing this repository) holds the repository lock.
	ExitLockFailed ExitCode = 11
	// ExitWrongPassword: the password file's contents don't match the
	// repository.
	ExitWrongPassword ExitCode = 12
	// ExitInterrupted: restic was interrupted (SIGINT).
	ExitInterrupted ExitCode = 130
)

// String returns a short human-readable label for the exit code.
func (c ExitCode) String() string {
	switch c {
	case ExitSuccess:
		return "success"
	case ExitGenericError:
		return "generic error"
	case ExitInvalidUsage:
		return "invalid usage"
	case ExitBackupIncomplete:
		return "backup incomplete: some source files could not be read"
	case ExitRepositoryNotFound:
		return "repository not found"
	case ExitLockFailed:
		return "repository lock held by another process"
	case ExitWrongPassword:
		return "wrong repository password"
	case ExitInterrupted:
		return "interrupted"
	default:
		return fmt.Sprintf("unrecognized exit code %d", int(c))
	}
}

// classify extracts and classifies the process exit code from err, which
// is expected to (possibly) wrap an *execx.ExitError. A nil err classifies
// as ExitSuccess; an err with no extractable exit code (e.g. the process
// never started, or a context cancellation) classifies as
// ExitGenericError.
func classify(err error) ExitCode {
	if err == nil {
		return ExitSuccess
	}
	var execErr *execx.ExitError
	if !errors.As(err, &execErr) {
		return ExitGenericError
	}
	switch execErr.Code {
	case 0:
		return ExitSuccess
	case 1:
		return ExitGenericError
	case 2:
		return ExitInvalidUsage
	case 3:
		return ExitBackupIncomplete
	case 10:
		return ExitRepositoryNotFound
	case 11:
		return ExitLockFailed
	case 12:
		return ExitWrongPassword
	case 130:
		return ExitInterrupted
	default:
		return ExitGenericError
	}
}

// ExitError wraps a classified restic exit code. Use errors.As to inspect
// Code and react differently to, say, ExitWrongPassword vs.
// ExitLockFailed.
type ExitError struct {
	Code ExitCode
	Err  error
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("restic: %s: %v", e.Code, e.Err)
}

func (e *ExitError) Unwrap() error { return e.Err }
