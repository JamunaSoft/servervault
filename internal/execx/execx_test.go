package execx

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name        string
		cmd         string
		args        []string
		wantErr     bool
		wantExit    int
		wantStdout  string
		checkStderr bool
	}{
		{name: "success", cmd: "echo", args: []string{"hello"}, wantErr: false, wantExit: 0, wantStdout: "hello\n"},
		{name: "nonzero exit", cmd: "false", args: nil, wantErr: true, wantExit: 1},
		{name: "unknown command", cmd: "servervault-does-not-exist", args: nil, wantErr: true, wantExit: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Run(context.Background(), tt.cmd, tt.args...)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantStdout != "" && result.Stdout != tt.wantStdout {
				t.Errorf("Stdout = %q, want %q", result.Stdout, tt.wantStdout)
			}
			if tt.name != "unknown command" && result.ExitCode != tt.wantExit {
				t.Errorf("ExitCode = %d, want %d", result.ExitCode, tt.wantExit)
			}
		})
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Run(ctx, "sleep", "5")
	if err == nil {
		t.Fatal("Run() with a canceled context: want error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestRun_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := Run(ctx, "sleep", "5")
	if err == nil {
		t.Fatal("Run() with a short timeout: want error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestPathChecker_LookPath(t *testing.T) {
	var checker CommandChecker = PathChecker{}

	if _, err := checker.LookPath("echo"); err != nil {
		t.Errorf("LookPath(%q) = %v, want a resolved path", "echo", err)
	}

	if _, err := checker.LookPath("servervault-does-not-exist"); err == nil {
		t.Error("LookPath() for a nonexistent command: want error, got nil")
	}
}
