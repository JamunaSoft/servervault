package cli

import (
	"bytes"
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

func writeTestConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "servervault.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("writeTestConfig: %v", err)
	}
	return path
}
