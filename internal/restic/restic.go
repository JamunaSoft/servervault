// Package restic wraps the restic CLI for the operations ServerVault's
// Phase A backup engine needs: backing up, checking, and listing
// snapshots. Every invocation goes through internal/execx.Runner, built as
// an argv slice — never a shell string — so a caller-supplied path, tag,
// or hostname can never be interpreted as shell syntax.
//
// The repository password never enters this package as a string: only the
// password *file path* is passed to restic, as an environment variable
// (RESTIC_PASSWORD_FILE), never a command-line argument. restic reads the
// file itself.
//
// Deliberately absent from this package: Init and Unlock. The "never
// delete an entire repository" rule is enforced structurally here --
// those capabilities don't exist in this package, not just left unused.
//
// Restore (see restore.go, v0.4.0-alpha.1) and Forget (see forget.go,
// v0.5.0) are deliberate, scoped exceptions: staging-only restore and
// policy-driven snapshot retention are themselves non-negotiable
// ServerVault capabilities (CLAUDE.md), so they have to exist
// somewhere. Neither method enforces ServerVault's own safety policy
// itself -- Restore will write to whatever Target it is given, and
// Forget will remove whatever Forget's keep-policy says to remove. The
// "never restore over live data" guarantee lives in internal/restore's
// Planner; the retention safety limits (minimum snapshot count, maximum
// deletion count, repository health validation) live in
// internal/retention's Planner/Executor. This package's job is
// correctly invoking restic, not deciding what's safe.
package restic

import (
	"bytes"
	"context"
	"fmt"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
)

// Repository is a configured Restic repository.
type Repository struct {
	runner execx.Runner
	cfg    config.ResticConfig
}

// New builds a Repository that invokes restic via runner, using cfg for
// the repository location and credentials.
func New(runner execx.Runner, cfg config.ResticConfig) *Repository {
	return &Repository{runner: runner, cfg: cfg}
}

func (r *Repository) env() []string {
	env := []string{
		"RESTIC_REPOSITORY=" + r.cfg.Repository,
		"RESTIC_PASSWORD_FILE=" + r.cfg.PasswordFile,
	}
	if r.cfg.SFTPCommand != "" {
		env = append(env, "RESTIC_SFTP_COMMAND="+r.cfg.SFTPCommand)
	}
	return env
}

// run is the single choke point every method below uses to invoke restic,
// so environment wiring and exit-code classification happen in one place.
func (r *Repository) run(ctx context.Context, args []string) (stdout, stderr bytes.Buffer, err error) {
	err = r.runner.Run(ctx, execx.RunOptions{
		Name:   "restic",
		Args:   args,
		Env:    r.env(),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	return stdout, stderr, err
}

// CatConfig performs the cheapest possible reachability and authentication
// check: it reads the repository's config blob without listing snapshots
// or touching any data. Used by `servervault doctor`.
func (r *Repository) CatConfig(ctx context.Context) error {
	_, stderr, err := r.run(ctx, []string{"cat", "config"})
	if err != nil {
		return &ExitError{Code: classifyResult(err, stderr), Err: wrapWithStderr(err, "restic cat config", stderr)}
	}
	return nil
}

// CheckOptions configures Check.
type CheckOptions struct {
	// ReadData requests the heavier `--read-data` pass (reads every data
	// blob back from the repository, not just metadata consistency).
	ReadData bool
}

// Check runs `restic check`, verifying repository consistency without
// modifying it.
func (r *Repository) Check(ctx context.Context, opts CheckOptions) error {
	args := []string{"check"}
	if opts.ReadData {
		args = append(args, "--read-data")
	}
	_, stderr, err := r.run(ctx, args)
	if err != nil {
		return &ExitError{Code: classifyResult(err, stderr), Err: wrapWithStderr(err, "restic check", stderr)}
	}
	return nil
}

// classifyResult combines exit-code-based classification (classify) with
// stderr-content-based classification (classifyStderr), preferring the
// stderr signal when it matches something specific -- see classifyStderr's
// doc comment for why.
func classifyResult(err error, stderr bytes.Buffer) ExitCode {
	if refined, ok := classifyStderr(stderr.String()); ok {
		return refined
	}
	return classify(err)
}

func wrapWithStderr(err error, op string, stderr bytes.Buffer) error {
	if s := boundedStderr(stderr); s != "" {
		return fmt.Errorf("%s: %w (stderr: %s)", op, err, s)
	}
	return fmt.Errorf("%s: %w", op, err)
}

const maxStderrLen = 2000

// boundedStderr trims and truncates captured stderr so error messages stay
// readable and don't balloon with restic's sometimes-verbose output.
func boundedStderr(buf bytes.Buffer) string {
	s := bytes.TrimSpace(buf.Bytes())
	if len(s) > maxStderrLen {
		return string(s[:maxStderrLen]) + "... (truncated)"
	}
	return string(s)
}
