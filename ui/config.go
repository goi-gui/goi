package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// windowConfig is the part of goi.yaml that names the assets directory.
type windowConfig struct {
	// Assets is the directory holding the ui files and their assets,
	// relative to goi.yaml.
	Assets string `yaml:"assets"`
}

// designDir reads the assets directory from goi.yaml in the working directory.
// Without a goi.yaml the entry is used as written, so a single-file program
// needs no config; with one, assets defaults to "assets" and the entry is
// resolved inside it.
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
	if cfg.Assets == "" {
		return "assets", nil
	}
	return cfg.Assets, nil
}
