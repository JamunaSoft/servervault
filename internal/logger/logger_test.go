package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{name: "defaults", opts: DefaultOptions()},
		{name: "json format", opts: Options{Format: "json", Level: "debug", Output: "stdout"}},
		{name: "text format explicit", opts: Options{Format: "text", Level: "warn", Output: "stdout"}},
		{name: "unrecognized format", opts: Options{Format: "xml", Level: "info", Output: "stdout"}, wantErr: true},
		{name: "unrecognized level", opts: Options{Format: "text", Level: "verbose", Output: "stdout"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, closeFn, err := New(tt.opts)
			t.Cleanup(func() { _ = closeFn() })

			if (err != nil) != tt.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if log == nil {
				t.Fatal("New(): logger is nil despite no error")
			}
			if closeFn == nil {
				t.Fatal("New(): closeFn is nil despite no error")
			}
		})
	}
}

func TestNew_TextOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servervault.log")

	log, closeFn, err := New(Options{Format: "text", Level: "info", Output: path})
	if err != nil {
		t.Fatalf("New(): unexpected error: %v", err)
	}
	log.Info("backup completed", "operation", "backup", "duration_ms", 1234)
	if err := closeFn(); err != nil {
		t.Fatalf("closeFn(): unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "backup completed") {
		t.Errorf("log file = %q, want it to contain %q", got, "backup completed")
	}
	if !strings.Contains(got, "operation=backup") {
		t.Errorf("log file = %q, want it to contain %q", got, "operation=backup")
	}
}

func TestNew_JSONOutputIsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log.Info("verify completed", "snapshot_id", "a730d4a2")

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("log line is not valid JSON: %v (line: %s)", err, buf.String())
	}
	if decoded["msg"] != "verify completed" {
		t.Errorf("msg = %v, want %q", decoded["msg"], "verify completed")
	}
	if decoded["snapshot_id"] != "a730d4a2" {
		t.Errorf("snapshot_id = %v, want %q", decoded["snapshot_id"], "a730d4a2")
	}
}

func TestNew_LevelFiltering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servervault.log")

	log, closeFn, err := New(Options{Format: "text", Level: "warn", Output: path})
	if err != nil {
		t.Fatalf("New(): unexpected error: %v", err)
	}
	log.Info("this should be filtered out")
	log.Warn("this should appear")
	if err := closeFn(); err != nil {
		t.Fatalf("closeFn(): unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "should be filtered out") {
		t.Errorf("log file contains an info-level line despite Level=warn: %q", got)
	}
	if !strings.Contains(got, "should appear") {
		t.Errorf("log file missing the warn-level line: %q", got)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    slog.Level
		wantErr bool
	}{
		{input: "debug", want: slog.LevelDebug},
		{input: "info", want: slog.LevelInfo},
		{input: "", want: slog.LevelInfo},
		{input: "WARN", want: slog.LevelWarn},
		{input: "warning", want: slog.LevelWarn},
		{input: "error", want: slog.LevelError},
		{input: "nope", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseLevel(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLevel(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
