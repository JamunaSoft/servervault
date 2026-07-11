// Package execx runs external commands safely and cancellably.
//
// Every command is invoked as an argv slice via os/exec, which never passes
// through a shell — arguments are never concatenated into a shell string,
// so caller-supplied values (paths, hostnames, database names) cannot be
// interpreted as shell syntax. Every call takes a context.Context so a
// caller can cancel or time out a running command.
package execx

import (
	"os/exec"
)

// Result holds the outcome of a completed command, as captured by the
// package-level Run convenience function in runner.go.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
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
