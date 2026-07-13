package postgres

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/JamunaSoft/servervault/internal/execx"
)

// VerifyError wraps a failure at a specific stage of VerifyDump.
type VerifyError struct {
	// Stage is one of "create", "decompress", "pg_restore".
	Stage string
	Err   error
}

func (e *VerifyError) Error() string {
	return fmt.Sprintf("postgres: verify dump (%s): %v", e.Stage, e.Err)
}
func (e *VerifyError) Unwrap() error { return e.Err }

// VerifyDump decompresses dumpPath into a temporary file and runs
// `pg_restore --list` against it, matching the shell implementation's
// verification step. It never touches the live database. The temporary
// decompressed copy's lifetime is entirely scoped to this call -- it is
// always removed before VerifyDump returns, on every path, including a
// pg_restore failure (a gap the shell implementation has: under `set -e`,
// a failed `pg_restore --list` skips the shell's own `rm -f` for that
// copy, leaking an uncompressed temp file).
func (c *Client) VerifyDump(ctx context.Context, dumpPath string) error {
	tmp, err := os.CreateTemp(filepath.Dir(dumpPath), "verify-*.dump")
	if err != nil {
		return &VerifyError{Stage: "create", Err: err}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	var zstdStderr bytes.Buffer
	if err := c.runner.Run(ctx, execx.RunOptions{
		Name: "zstd", Args: []string{"-dc", dumpPath}, Stdout: tmp, Stderr: &zstdStderr,
	}); err != nil {
		return &VerifyError{Stage: "decompress", Err: wrapWithStderr(err, zstdStderr)}
	}

	var listStderr bytes.Buffer
	if err := c.runner.Run(ctx, execx.RunOptions{
		Name: "pg_restore", Args: []string{"--list", tmpPath}, Stderr: &listStderr,
	}); err != nil {
		return &VerifyError{Stage: "pg_restore", Err: wrapWithStderr(err, listStderr)}
	}

	return nil
}
