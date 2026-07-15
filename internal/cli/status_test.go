package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStatusCommand(t *testing.T) {
	tests := []struct {
		name         string
		configYAML   string
		explicitPath bool
		wantExit     int
		wantContains string
	}{
		{
			name: "valid config still fails repository reachability in a bare test sandbox",
			configYAML: `
restic:
  repository: "sftp:user@host:/backups/servervault"
  password_file: "/nonexistent/restic-password"
backup:
  paths:
    - /tmp
retention:
  keep_daily: 7
`,
			wantExit:     1,
			wantContains: "repository reachability",
		},
		{
			name:         "missing explicit config file is a usage error",
			explicitPath: true,
			wantExit:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var configPath string
			if tt.explicitPath {
				configPath = filepath.Join(t.TempDir(), "does-not-exist.yaml")
			} else {
				configPath = writeTestConfig(t, tt.configYAML)
			}

			cmd := NewStatusCommand()
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetArgs([]string{"--config", configPath})

			err := cmd.Execute()
			if got := ExitCode(err); got != tt.wantExit {
				t.Fatalf("ExitCode(Execute()) = %d, want %d (stdout: %s, stderr: %s)", got, tt.wantExit, out.String(), errOut.String())
			}
			if tt.wantContains != "" && !strings.Contains(out.String(), tt.wantContains) {
				t.Errorf("status output = %q, want it to contain %q", out.String(), tt.wantContains)
			}
		})
	}
}

func TestNewStatusCommand_JSON(t *testing.T) {
	configPath := writeTestConfig(t, `
restic:
  repository: "sftp:user@host:/backups/servervault"
  password_file: "/nonexistent/restic-password"
backup:
  paths:
    - /tmp
retention:
  keep_daily: 7
`)

	cmd := NewStatusCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--config", configPath, "--json"})

	err := cmd.Execute()
	if got := ExitCode(err); got != 1 {
		t.Fatalf("ExitCode(Execute()) = %d, want 1 (output: %s)", got, out.String())
	}

	var decoded struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"checks"`
		GeneratedAt string `json:"generated_at"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("status --json output is not valid JSON: %v\noutput: %s", err, out.String())
	}
	if len(decoded.Checks) == 0 {
		t.Fatal("decoded JSON report has no checks")
	}
	if decoded.GeneratedAt == "" {
		t.Error("decoded JSON report has an empty generated_at")
	}

	found := false
	for _, c := range decoded.Checks {
		if c.Name == "repository reachability" {
			found = true
			if c.Status != "FAIL" {
				t.Errorf("repository reachability status = %q, want %q", c.Status, "FAIL")
			}
		}
		if c.Status == "" {
			t.Errorf("check %q has an empty status string -- MarshalJSON should render it as OK/WARN/FAIL/UNKNOWN, not a raw int", c.Name)
		}
	}
	if !found {
		t.Error("decoded JSON report missing the \"repository reachability\" check")
	}
}

func TestNewStatusCommand_NoJobHistoryYetIsNotAFailure(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, `
restic:
  repository: ""
backup:
  paths:
    - /tmp
  lock_file: "`+filepath.Join(dir, "backup.lock")+`"
restore:
  lock_file: "`+filepath.Join(dir, "restore.lock")+`"
retention:
  keep_daily: 7
  lock_file: "`+filepath.Join(dir, "prune.lock")+`"
state_dir: "`+filepath.Join(dir, "state")+`"
`)

	cmd := NewStatusCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", configPath})

	err := cmd.Execute()
	if got := ExitCode(err); got != 0 {
		t.Fatalf("ExitCode(Execute()) = %d, want 0 -- an unconfigured repository and no job history yet should both report UNKNOWN, not FAIL (stdout: %s, stderr: %s)", got, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "UNKNOWN") {
		t.Errorf("status output = %q, want it to show UNKNOWN checks", out.String())
	}
}

func TestNewStatusCommand_HasFlags(t *testing.T) {
	cmd := NewStatusCommand()
	for _, name := range []string{"config", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("NewStatusCommand(): missing --%s flag", name)
		}
	}
}
