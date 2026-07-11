// Package postgres wraps the pg_dump/pg_restore/psql CLI tools for the
// operations ServerVault's Phase A backup engine needs: connectivity
// checks, dumping, and verifying a dump. It matches the shell
// implementation's authentication model exactly: PostgreSQL peer
// authentication over the local Unix socket, invoked as cfg.User via
// non-interactive sudo when the current process isn't already running as
// that user. There is no password anywhere in this package -- config.
// PostgresConfig has no password field, by design.
//
// Every invocation goes through internal/execx.Runner, built as an argv
// slice -- never a shell string.
package postgres

import (
	"bytes"
	"context"
	"fmt"
	"os/user"
	"strconv"
	"strings"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
)

// Client runs PostgreSQL CLI tools against a configured database.
type Client struct {
	runner execx.Runner
	cfg    config.PostgresConfig
}

// New builds a Client that invokes pg_dump/pg_restore/psql via runner,
// using cfg for connection and auth settings.
func New(runner execx.Runner, cfg config.PostgresConfig) *Client {
	return &Client{runner: runner, cfg: cfg}
}

// commandFor builds the argv to invoke name (e.g. "psql", "pg_dump") as
// c.cfg.User via non-interactive sudo, unless the current process is
// already running as that user. `-n` makes sudo fail immediately rather
// than prompt for a password it will never receive (no subprocess this
// package starts is ever connected to a real stdin/TTY by default).
func (c *Client) commandFor(name string, args []string) (string, []string) {
	if !needsSudo(c.cfg.User) {
		return name, args
	}
	sudoArgs := append([]string{"-n", "-u", c.cfg.User, name}, args...)
	return "sudo", sudoArgs
}

func needsSudo(targetUser string) bool {
	if targetUser == "" {
		return false
	}
	current, err := user.Current()
	if err != nil {
		return true // can't tell -- default to the safer (shell-matching) path
	}
	return current.Username != targetUser
}

// baseArgs returns the connection flags shared by every psql/pg_dump
// invocation. Deliberately empty when cfg.Host is empty: omitting -h/-p
// entirely is what makes the client connect via the local Unix socket,
// which is required for peer authentication to apply. Passing -h would
// force a TCP connection and a different (password-based) auth method
// this package does not implement.
func (c *Client) baseArgs() []string {
	if c.cfg.Host == "" {
		return nil
	}
	args := []string{"-h", c.cfg.Host}
	if c.cfg.Port != 0 {
		args = append(args, "-p", strconv.Itoa(c.cfg.Port))
	}
	return args
}

// Ping verifies connectivity with `SELECT 1`, matching the shell
// implementation's pre-backup check.
func (c *Client) Ping(ctx context.Context) error {
	args := append(c.baseArgs(), "-d", c.cfg.Database, "-Atc", "SELECT 1")
	name, args := c.commandFor("psql", args)

	var stdout, stderr bytes.Buffer
	err := c.runner.Run(ctx, execx.RunOptions{Name: name, Args: args, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return fmt.Errorf("postgres: ping: %w", wrapWithStderr(err, stderr))
	}

	got := strings.TrimSpace(stdout.String())
	if got != "1" {
		return fmt.Errorf("postgres: ping: unexpected response %q", got)
	}
	return nil
}

const maxStderrLen = 2000

// boundedStderr trims and truncates captured stderr so error messages stay
// readable and bounded in size.
func boundedStderr(buf bytes.Buffer) string {
	s := bytes.TrimSpace(buf.Bytes())
	if len(s) > maxStderrLen {
		return string(s[:maxStderrLen]) + "... (truncated)"
	}
	return string(s)
}

func wrapWithStderr(err error, stderr bytes.Buffer) error {
	if s := boundedStderr(stderr); s != "" {
		return fmt.Errorf("%w (stderr: %s)", err, s)
	}
	return err
}
