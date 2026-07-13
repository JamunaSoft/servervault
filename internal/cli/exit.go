package cli

import (
	"errors"
	"fmt"
)

// ExitError lets a command specify its own process exit code (e.g.
// doctor's 0/1/2 contract) while still returning a normal error from
// Cobra's RunE, so command logic stays testable without calling os.Exit
// itself.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// ExitCode maps an error returned from Execute to a process exit code: 0
// for nil, the wrapped code for an *ExitError, and 1 for anything else.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return 1
}
