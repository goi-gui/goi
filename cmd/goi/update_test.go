//go:build !js && !wasip1

package main

import (
	"io"
	"strings"
	"testing"
)

func TestSetupCommand(t *testing.T) {
	var calls [][]string
	cmd := newSetupCmd(func(_ string, args []string, _, _ io.Writer, _ []string) error {
		calls = append(calls, args)
		return nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"install", "github.com/goi-gui/goi/cmd/goipls@latest"},
		{"install", "github.com/goi-gui/goi/cmd/goifmt@latest"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if strings.Join(calls[i], " ") != strings.Join(want[i], " ") {
			t.Fatalf("call %d = %v, want %v", i, calls[i], want[i])
		}
	}
}

func TestUpdateCommand(t *testing.T) {
	var calls [][]string
	cmd := newUpdateCmd(func(_ string, args []string, _, _ io.Writer, _ []string) error {
		calls = append(calls, args)
		return nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"get", "github.com/goi-gui/goi@latest"},
		{"install", "github.com/goi-gui/goi/cmd/goipls@latest"},
		{"install", "github.com/goi-gui/goi/cmd/goifmt@latest"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if strings.Join(calls[i], " ") != strings.Join(want[i], " ") {
			t.Fatalf("call %d = %v, want %v", i, calls[i], want[i])
		}
	}
}

func TestDoctorReportsEveryComponent(t *testing.T) {
	var out strings.Builder
	printDoctor(&out)
	for _, want := range []string{"COMPONENT", "OS", "Go", "CGO", "goi", "goipls", "goifmt", "SDL3", "Vulkan", "ffmpeg", "git"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestModuleVersion(t *testing.T) {
	out := "path\tgithub.com/goi-gui/goi/cmd/goipls\n" +
		"mod\tgithub.com/goi-gui/goi\tv0.28.1\th1:abc\n"
	if got := moduleVersion(out); got != "v0.28.1" {
		t.Fatalf("moduleVersion = %q, want v0.28.1", got)
	}
	if got := moduleVersion("mod\texample.com/other\tv1.0.0\n"); got != "" {
		t.Fatalf("moduleVersion = %q, want an empty string", got)
	}
}
