package cli

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil", err: nil, want: 0},
		{name: "exit error 2", err: &ExitError{Code: 2}, want: 2},
		{name: "exit error 1", err: &ExitError{Code: 1}, want: 1},
		{name: "wrapped exit error", err: fmt.Errorf("doctor: %w", &ExitError{Code: 2}), want: 2},
		{name: "plain error", err: errors.New("boom"), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
