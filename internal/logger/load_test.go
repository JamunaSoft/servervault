package logger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOptions_EmptyPath(t *testing.T) {
	opts, err := LoadOptions("")
	if err != nil {
		t.Fatalf("LoadOptions(\"\"): unexpected error: %v", err)
	}
	if opts != DefaultOptions() {
		t.Errorf("LoadOptions(\"\") = %+v, want %+v", opts, DefaultOptions())
	}
}

func TestLoadOptions_MissingFileIsNotError(t *testing.T) {
	opts, err := LoadOptions(filepath.Join(t.TempDir(), "missing-logging.yaml"))
	if err != nil {
		t.Fatalf("LoadOptions() with a missing file: unexpected error: %v", err)
	}
	if opts != DefaultOptions() {
		t.Errorf("LoadOptions() with a missing file = %+v, want %+v", opts, DefaultOptions())
	}
}

func TestLoadOptions_YAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logging.yaml")
	content := `
logging:
  format: "json"
  level: "debug"
  output: "stdout"
  add_source: true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing test fixture: %v", err)
	}

	opts, err := LoadOptions(path)
	if err != nil {
		t.Fatalf("LoadOptions(%q): unexpected error: %v", path, err)
	}

	want := Options{Format: "json", Level: "debug", Output: "stdout", AddSource: true}
	if opts != want {
		t.Errorf("LoadOptions(%q) = %+v, want %+v", path, opts, want)
	}
}

func TestLoadOptions_PartialYAMLKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logging.yaml")
	if err := os.WriteFile(path, []byte("logging:\n  level: debug\n"), 0o600); err != nil {
		t.Fatalf("writing test fixture: %v", err)
	}

	opts, err := LoadOptions(path)
	if err != nil {
		t.Fatalf("LoadOptions(%q): unexpected error: %v", path, err)
	}
	if opts.Level != "debug" {
		t.Errorf("Level = %q, want %q", opts.Level, "debug")
	}
	if opts.Format != DefaultOptions().Format {
		t.Errorf("Format = %q, want default %q", opts.Format, DefaultOptions().Format)
	}
}

func TestLoadOptions_MalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logging.yaml")
	if err := os.WriteFile(path, []byte("logging: [not a mapping"), 0o600); err != nil {
		t.Fatalf("writing test fixture: %v", err)
	}

	if _, err := LoadOptions(path); err == nil {
		t.Fatal("LoadOptions() with malformed YAML: want error, got nil")
	}
}
