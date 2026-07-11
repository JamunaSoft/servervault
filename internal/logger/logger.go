// Package logger builds ServerVault's structured logger on top of
// log/slog. Output defaults to human-readable text; JSON is available for
// log shippers and journald. Callers are responsible for never passing a
// secret value (repository passwords, SSH keys, database passwords) as a
// log attribute — this package does not attempt to redact after the fact.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Options configures a logger. The zero value is not valid; use
// DefaultOptions as a starting point.
type Options struct {
	// Format is "text" or "json".
	Format string
	// Level is "debug", "info", "warn", or "error".
	Level string
	// Output is "stdout", "stderr", or an absolute file path.
	Output string
	// AddSource includes the source file:line of each log call.
	AddSource bool
}

// DefaultOptions returns ServerVault's default logging configuration:
// human-readable text, info level, to stderr.
func DefaultOptions() Options {
	return Options{
		Format: "text",
		Level:  "info",
		Output: "stderr",
	}
}

// New builds a *slog.Logger from opts. The returned close function flushes
// and closes any file opened for Output; it is a no-op for stdout/stderr
// and is always safe to call, including when New returns an error.
func New(opts Options) (logger *slog.Logger, closeFn func() error, err error) {
	closeFn = func() error { return nil }

	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, closeFn, fmt.Errorf("logger: %w", err)
	}

	writer, closeFn, err := openOutput(opts.Output)
	if err != nil {
		return nil, closeFn, fmt.Errorf("logger: %w", err)
	}

	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.AddSource,
	}

	var handler slog.Handler
	switch strings.ToLower(opts.Format) {
	case "text", "":
		handler = slog.NewTextHandler(writer, handlerOpts)
	case "json":
		handler = slog.NewJSONHandler(writer, handlerOpts)
	default:
		return nil, closeFn, fmt.Errorf("logger: unrecognized format %q (want \"text\" or \"json\")", opts.Format)
	}

	return slog.New(handler), closeFn, nil
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unrecognized level %q (want debug, info, warn, or error)", level)
	}
}

func openOutput(output string) (io.Writer, func() error, error) {
	switch strings.ToLower(output) {
	case "stdout", "":
		return os.Stdout, func() error { return nil }, nil
	case "stderr":
		return os.Stderr, func() error { return nil }, nil
	default:
		f, err := os.OpenFile(output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, func() error { return nil }, fmt.Errorf("open log output %q: %w", output, err)
		}
		return f, f.Close, nil
	}
}
