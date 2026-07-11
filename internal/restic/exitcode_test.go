package restic

import (
	"errors"
	"fmt"
	"testing"

	"github.com/JamunaSoft/servervault/internal/execx"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ExitCode
	}{
		{name: "nil error", err: nil, want: ExitSuccess},
		{name: "exit 0", err: &execx.ExitError{Code: 0}, want: ExitSuccess},
		{name: "exit 1", err: &execx.ExitError{Code: 1}, want: ExitGenericError},
		{name: "exit 2", err: &execx.ExitError{Code: 2}, want: ExitInvalidUsage},
		{name: "exit 3", err: &execx.ExitError{Code: 3}, want: ExitBackupIncomplete},
		{name: "exit 10", err: &execx.ExitError{Code: 10}, want: ExitRepositoryNotFound},
		{name: "exit 11", err: &execx.ExitError{Code: 11}, want: ExitLockFailed},
		{name: "exit 12", err: &execx.ExitError{Code: 12}, want: ExitWrongPassword},
		{name: "exit 130", err: &execx.ExitError{Code: 130}, want: ExitInterrupted},
		{name: "unrecognized exit code", err: &execx.ExitError{Code: 99}, want: ExitGenericError},
		{name: "error with no extractable exit code", err: errors.New("boom"), want: ExitGenericError},
		{
			name: "multi-level wrapped exit error still unwraps",
			err:  fmt.Errorf("restic backup: %w", &execx.ExitError{Code: 11}),
			want: ExitLockFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(tt.err); got != tt.want {
				t.Errorf("classify(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestExitCode_String(t *testing.T) {
	tests := []struct {
		code ExitCode
		want string
	}{
		{ExitSuccess, "success"},
		{ExitLockFailed, "repository lock held by another process"},
		{ExitWrongPassword, "wrong repository password"},
		{ExitCode(42), "unrecognized exit code 42"},
	}
	for _, tt := range tests {
		if got := tt.code.String(); got != tt.want {
			t.Errorf("ExitCode(%d).String() = %q, want %q", tt.code, got, tt.want)
		}
	}
}
