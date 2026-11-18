//go:build !js && !wasip1

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	cmd := newVersionCmd()
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	if !strings.HasPrefix(got, "goi ") || got == "goi" {
		t.Fatalf("version output = %q, want goi <version>", got)
	}
	if programVersion() == "" {
		t.Fatal("programVersion is empty")
	}
}
