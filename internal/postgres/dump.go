package postgres

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/JamunaSoft/servervault/internal/execx"
)

// Metadata describes a completed dump.
type Metadata struct {
	Path     string
	Bytes    int64
	Duration time.Duration
}

// DumpError wraps a failure at a specific stage of Dump.
type DumpError struct {
	// Stage is one of "create", "pg_dump", "zstd".
	Stage string
	Err   error
}

func (e *DumpError) Error() string { return fmt.Sprintf("postgres: dump (%s): %v", e.Stage, e.Err) }
func (e *DumpError) Unwrap() error { return e.Err }

var dangerousFilenameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

func dumpFilePattern(database string) string {
	safe := dangerousFilenameChars.ReplaceAllString(database, "_")
	if safe == "" {
		safe = "database"
	}
	return fmt.Sprintf("%s_%s_*.dump.zst", safe, time.Now().UTC().Format("20060102-150405"))
}

// Dump runs `pg_dump --format=custom --no-owner --no-privileges`, piping
// its output through `zstd -T0 -<level>` into a new file created inside
// dir. The destination file is created atomically (os.CreateTemp, mode
// 0600) before either subprocess starts -- Dump owns the whole file
// lifecycle so its permissions and uniqueness are guaranteed by
// construction, not by convention.
//
// The caller is responsible for removing the returned file: Dump does not
// clean up its own output on success, since the caller (internal/backup)
// needs the path afterward to hand to Restic. It also does not remove the
// file on failure -- see DumpError -- callers should treat any returned
// error as "the destination file may exist and may be incomplete; the
// caller owns cleaning it up," which internal/backup does unconditionally
// via defer immediately after a successful Dump call.
func (c *Client) Dump(ctx context.Context, dir string) (Metadata, error) {
	start := time.Now()

	f, err := os.CreateTemp(dir, dumpFilePattern(c.cfg.Database))
	if err != nil {
		return Metadata{}, &DumpError{Stage: "create", Err: err}
	}
	destPath := f.Name()
	defer f.Close()

	dumpArgs := append(c.baseArgs(), "--format=custom", "--no-owner", "--no-privileges", c.cfg.Database)
	dumpName, dumpArgs := c.commandFor("pg_dump", dumpArgs)

	zstdArgs := []string{"-T0", "-" + strconv.Itoa(c.cfg.ZstdLevel)}

	pr, pw := io.Pipe()

	var dumpStderr, zstdStderr bytes.Buffer
	var dumpErr, zstdErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer pw.Close()
		dumpErr = c.runner.Run(ctx, execx.RunOptions{
			Name: dumpName, Args: dumpArgs, Stdout: pw, Stderr: &dumpStderr,
		})
	}()

	go func() {
		defer wg.Done()
		defer pr.Close()
		zstdErr = c.runner.Run(ctx, execx.RunOptions{
			Name: "zstd", Args: zstdArgs, Stdin: pr, Stdout: f, Stderr: &zstdStderr,
		})
	}()

	wg.Wait()

	if dumpErr != nil {
		return Metadata{Path: destPath}, &DumpError{Stage: "pg_dump", Err: wrapWithStderr(dumpErr, dumpStderr)}
	}
	if zstdErr != nil {
		return Metadata{Path: destPath}, &DumpError{Stage: "zstd", Err: wrapWithStderr(zstdErr, zstdStderr)}
	}

	info, err := f.Stat()
	if err != nil {
		return Metadata{Path: destPath}, &DumpError{Stage: "create", Err: err}
	}

	return Metadata{Path: destPath, Bytes: info.Size(), Duration: time.Since(start)}, nil
}
