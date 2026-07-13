package execx

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDefaultRunner_Run_Stdout(t *testing.T) {
	var out bytes.Buffer
	err := DefaultRunner{}.Run(context.Background(), RunOptions{
		Name: "printf", Args: []string{"hello"}, Stdout: &out,
	})
	if err != nil {
		t.Fatalf("Run(): unexpected error: %v", err)
	}
	if out.String() != "hello" {
		t.Errorf("stdout = %q, want %q", out.String(), "hello")
	}
}

func TestDefaultRunner_Run_Stdin(t *testing.T) {
	var out bytes.Buffer
	err := DefaultRunner{}.Run(context.Background(), RunOptions{
		Name: "cat", Stdin: strings.NewReader("piped input"), Stdout: &out,
	})
	if err != nil {
		t.Fatalf("Run(): unexpected error: %v", err)
	}
	if out.String() != "piped input" {
		t.Errorf("stdout = %q, want %q", out.String(), "piped input")
	}
}

func TestDefaultRunner_Run_Env(t *testing.T) {
	var out bytes.Buffer
	err := DefaultRunner{}.Run(context.Background(), RunOptions{
		Name: "sh", Args: []string{"-c", "echo $SERVERVAULT_TEST_VAR"},
		Env:    []string{"SERVERVAULT_TEST_VAR=injected"},
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("Run(): unexpected error: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "injected" {
		t.Errorf("env var not visible to subprocess: got %q, want %q", got, "injected")
	}
}

func TestDefaultRunner_Run_EnvInheritsPATH(t *testing.T) {
	// A regression guard: cmd.Env = opts.Env alone (instead of appending
	// to os.Environ()) would wipe PATH and break every subsequent lookup.
	var out bytes.Buffer
	err := DefaultRunner{}.Run(context.Background(), RunOptions{
		Name: "sh", Args: []string{"-c", "command -v sh >/dev/null && echo found"},
		Env:    []string{"SERVERVAULT_UNRELATED=1"},
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("Run(): unexpected error (PATH likely not inherited): %v", err)
	}
	if strings.TrimSpace(out.String()) != "found" {
		t.Errorf("stdout = %q, want %q (PATH not inherited alongside Env)", out.String(), "found")
	}
}

func TestDefaultRunner_Run_ExitErrorCode(t *testing.T) {
	err := DefaultRunner{}.Run(context.Background(), RunOptions{Name: "sh", Args: []string{"-c", "exit 7"}})
	if err == nil {
		t.Fatal("Run(): want an error for a non-zero exit, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run() error = %v, want it to unwrap to *ExitError", err)
	}
	if exitErr.Code != 7 {
		t.Errorf("ExitError.Code = %d, want 7", exitErr.Code)
	}
}

func TestDefaultRunner_Run_CanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := DefaultRunner{}.Run(ctx, RunOptions{Name: "sleep", Args: []string{"5"}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() with an already-canceled context: error = %v, want context.Canceled", err)
	}
}

func TestRun_ExitCodeOnFailure(t *testing.T) {
	result, err := Run(context.Background(), "sh", "-c", "exit 3")
	if err == nil {
		t.Fatal("Run(): want an error for a non-zero exit, got nil")
	}
	if result.ExitCode != 3 {
		t.Errorf("Result.ExitCode = %d, want 3", result.ExitCode)
	}
}
