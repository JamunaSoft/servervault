package restic

import (
	"bytes"
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

// Real stderr text observed from restic on a GitHub Actions runner
// (go-rewrite, Integration workflow), reconstructed from the reported
// failure since the raw log wasn't pasted verbatim -- kept close to
// restic's actual wording (a Fatal: line, wrapped/split across lines the
// way restic emits it) rather than a synthetic one-liner, since that
// wrapping/multi-line shape is exactly what normalizeStderr has to
// handle correctly.
const (
	ciWrongPasswordStderr = "Fatal: wrong password or no key found\n"

	ciRepositoryNotFoundStderr = "Fatal: unable to open config file: stat /tmp/servervault-test-repo/config: no such file or directory\n" +
		"Is there a repository at the following location?\n" +
		"local:/tmp/servervault-test-repo\n"
)

func TestClassifyStderr(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   ExitCode
		wantOK bool
	}{
		{name: "empty", stderr: "", wantOK: false},
		{name: "whitespace only", stderr: "   \n\t \n", wantOK: false},
		{name: "unrelated error text", stderr: "Fatal: unable to create lock in backend: some other problem\n", wantOK: false},

		{name: "CI-captured wrong password", stderr: ciWrongPasswordStderr, want: ExitWrongPassword, wantOK: true},
		{name: "wrong password, different casing", stderr: "FATAL: WRONG PASSWORD OR NO KEY FOUND\n", want: ExitWrongPassword, wantOK: true},
		{name: "wrong password, older restic wording", stderr: "Fatal: wrong password\n", want: ExitWrongPassword, wantOK: true},
		{name: "no key found, standalone phrasing", stderr: "Fatal: no key found to open repository\n", want: ExitWrongPassword, wantOK: true},

		{name: "CI-captured repository not found", stderr: ciRepositoryNotFoundStderr, want: ExitRepositoryNotFound, wantOK: true},
		{
			name:   "repository not found, single line",
			stderr: "Fatal: unable to open config file: open /srv/restic-repo/config: no such file or directory",
			want:   ExitRepositoryNotFound, wantOK: true,
		},
		{
			name:   "repository not found, question phrasing only",
			stderr: "Is there a repository at the following location?\n",
			want:   ExitRepositoryNotFound, wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := classifyStderr(tt.stderr)
			if ok != tt.wantOK {
				t.Fatalf("classifyStderr(%q) ok = %v, want %v", tt.stderr, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("classifyStderr(%q) = %v, want %v", tt.stderr, got, tt.want)
			}
		})
	}
}

func TestClassifyResult_StderrTakesPrecedenceOverGenericExitCode(t *testing.T) {
	// The actual bug this fixes: restic observed in CI exiting with a
	// generic status (not the documented 10/12) for these two
	// conditions, which without stderr-based classification would fall
	// through classify() to ExitGenericError.
	tests := []struct {
		name   string
		code   int
		stderr string
		want   ExitCode
	}{
		{name: "wrong password on generic exit 1", code: 1, stderr: ciWrongPasswordStderr, want: ExitWrongPassword},
		{name: "repository not found on generic exit 1", code: 1, stderr: ciRepositoryNotFoundStderr, want: ExitRepositoryNotFound},
		{name: "wrong password on documented exit 12 still classifies correctly", code: 12, stderr: ciWrongPasswordStderr, want: ExitWrongPassword},
		{name: "no stderr match falls back to exit code", code: 1, stderr: "Fatal: some unrelated problem\n", want: ExitGenericError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			stderr.WriteString(tt.stderr)
			err := &execx.ExitError{Code: tt.code}

			if got := classifyResult(err, stderr); got != tt.want {
				t.Errorf("classifyResult(exit %d, stderr) = %v, want %v", tt.code, got, tt.want)
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
