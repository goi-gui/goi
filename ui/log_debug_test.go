package ui

import (
	"testing"

	"github.com/ksckaan1/logger"
)

func TestGoiDebug(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"TRUE", true},
		{"true", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"FALSE", false},
		{"0", false},
		{"no", false},
		{"", false},
	}
	for _, c := range cases {
		name := c.value
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("GOI_DEBUG", c.value)
			if got := goiDebug(); got != c.want {
				t.Fatalf("goiDebug(%q) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}

func TestLoggerConfigIsQuietWithoutDebug(t *testing.T) {
	t.Setenv("GOI_DEBUG", "FALSE")
	if cfg := loggerConfig(); cfg.Level != logger.LevelDisabled {
		t.Fatalf("level = %q, want disabled", cfg.Level)
	}
	t.Setenv("GOI_DEBUG", "TRUE")
	if cfg := loggerConfig(); cfg.Level == logger.LevelDisabled {
		t.Fatal("GOI_DEBUG=TRUE left the logger disabled")
	}
}
