package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// windowConfig is the part of goi.yaml that names the design directory.
type windowConfig struct {
	// UI is the directory holding .goi files, relative to goi.yaml.
	UI string `yaml:"ui"`
}

// designDir reads the ui directory from goi.yaml in the working directory.
// Without a goi.yaml the entry is used as written, so a single-file program
// needs no config; with one, ui defaults to "ui" and the entry is resolved
// inside it.
func designDir() (string, error) {
	b, err := os.ReadFile("goi.yaml")
	if errors.Is(err, fs.ErrNotExist) {
		return ".", nil
	}
	if err != nil {
		return "", err
	}
	var cfg windowConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return "", fmt.Errorf("goi.yaml: %w", err)
	}
	if cfg.UI == "" {
		return "ui", nil
	}
	return cfg.UI, nil
}
