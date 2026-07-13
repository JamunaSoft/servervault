package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewConfigValidateCommand(t *testing.T) {
	tests := []struct {
		name         string
		configYAML   string
		wantExit     int
		wantContains string
	}{
		{
			name: "valid config",
			configYAML: `
restic:
  repository: "sftp:user@host:/backups/servervault"
  password_file: "/etc/servervault/restic-password"
backup:
  paths:
    - /var/www
retention:
  keep_daily: 7
postgres:
  enabled: true
  database: app_production
  user: postgres
  port: 5432
  zstd_level: 10
`,
			wantExit:     0,
			wantContains: "configuration is valid",
		},
		{
			name: "invalid config",
			configYAML: `
retention:
  keep_daily: -1
`,
			wantExit:     1,
			wantContains: "configuration is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := writeTestConfig(t, tt.configYAML)

			cmd := NewConfigValidateCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"--config", configPath})

			err := cmd.Execute()
			if got := ExitCode(err); got != tt.wantExit {
				t.Fatalf("ExitCode(Execute()) = %d, want %d (output: %s)", got, tt.wantExit, out.String())
			}
			if !strings.Contains(out.String(), tt.wantContains) {
				t.Errorf("output = %q, want it to contain %q", out.String(), tt.wantContains)
			}
		})
	}
}

func TestNewConfigValidateCommand_MissingExplicitFile(t *testing.T) {
	cmd := NewConfigValidateCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2", got)
	}
}

func TestNewConfigCommand_HasValidateSubcommand(t *testing.T) {
	cmd := NewConfigCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "validate" {
			found = true
		}
	}
	if !found {
		t.Error("NewConfigCommand(): missing \"validate\" subcommand")
	}
}
