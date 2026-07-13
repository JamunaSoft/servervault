package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewDoctorCommand(t *testing.T) {
	tests := []struct {
		name         string
		configYAML   string
		explicitPath bool
		wantExit     int
		wantContains string
	}{
		{
			name: "valid config still fails required-commands/secret checks in a bare test sandbox",
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
			wantContains: "secret permissions",
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

			cmd := NewDoctorCommand()
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetArgs([]string{"--config", configPath})

			err := cmd.Execute()
			if got := ExitCode(err); got != tt.wantExit {
				t.Fatalf("ExitCode(Execute()) = %d, want %d (stdout: %s, stderr: %s)", got, tt.wantExit, out.String(), errOut.String())
			}
			if tt.wantContains != "" && !strings.Contains(out.String(), tt.wantContains) {
				t.Errorf("doctor output = %q, want it to contain %q", out.String(), tt.wantContains)
			}
		})
	}
}

func TestNewDoctorCommand_JSON(t *testing.T) {
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

	cmd := NewDoctorCommand()
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
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("doctor --json output is not valid JSON: %v\noutput: %s", err, out.String())
	}
	if len(decoded.Checks) == 0 {
		t.Fatal("decoded JSON report has no checks")
	}

	found := false
	for _, c := range decoded.Checks {
		if c.Name == "secret permissions" {
			found = true
			if c.Status != "FAIL" {
				t.Errorf("secret permissions status = %q, want %q", c.Status, "FAIL")
			}
		}
		if c.Status == "" {
			t.Errorf("check %q has an empty status string -- MarshalJSON should render it as OK/WARN/FAIL/SKIP, not a raw int", c.Name)
		}
	}
	if !found {
		t.Error("decoded JSON report missing the \"secret permissions\" check")
	}
}

func writeTestConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "servervault.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("writeTestConfig: %v", err)
	}
	return path
}
