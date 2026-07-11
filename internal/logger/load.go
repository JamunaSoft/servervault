package logger

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// fileOptions mirrors configs/logging.example.yaml's shape: a single
// top-level "logging" key wrapping the same fields as Options.
type fileOptions struct {
	Logging struct {
		Format    string `yaml:"format"`
		Level     string `yaml:"level"`
		Output    string `yaml:"output"`
		AddSource bool   `yaml:"add_source"`
	} `yaml:"logging"`
}

// LoadOptions reads a logging.yaml file (see configs/logging.example.yaml)
// and layers it over DefaultOptions. A missing path is not an error: it
// returns DefaultOptions unchanged, matching internal/config.Load's
// "missing file at an unrequested path is fine" behavior.
func LoadOptions(path string) (Options, error) {
	opts := DefaultOptions()
	if path == "" {
		return opts, nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return opts, nil
	}
	if err != nil {
		return opts, fmt.Errorf("logger: read %s: %w", path, err)
	}

	var file fileOptions
	if err := yaml.Unmarshal(data, &file); err != nil {
		return opts, fmt.Errorf("logger: parse %s: %w", path, err)
	}

	if file.Logging.Format != "" {
		opts.Format = file.Logging.Format
	}
	if file.Logging.Level != "" {
		opts.Level = file.Logging.Level
	}
	if file.Logging.Output != "" {
		opts.Output = file.Logging.Output
	}
	opts.AddSource = file.Logging.AddSource

	return opts, nil
}
