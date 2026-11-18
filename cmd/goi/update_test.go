//go:build !js && !wasip1

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestToolsCommands(t *testing.T) {
	installs := [][]string{
		{"install", "github.com/goi-gui/goi/cmd/goi@latest"},
		{"install", "github.com/goi-gui/goi/cmd/goipls@latest"},
		{"install", "github.com/goi-gui/goi/cmd/goifmt@latest"},
	}
	for _, name := range []string{"setup", "update"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var calls [][]string
			run := func(_ string, args []string, _, _ io.Writer, _ []string) error {
				calls = append(calls, args)
				return nil
			}
			cmd := newSetupCmd(run)
			if name == "update" {
				cmd = newUpdateCmd(run)
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			assertCalls(t, calls, installs)
		})
	}
}

func TestToolsCommandsUpdateDependency(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("go.mod", []byte("module example.com/myapp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	assertCalls(t, calls, [][]string{
		{"install", "github.com/goi-gui/goi/cmd/goi@latest"},
		{"install", "github.com/goi-gui/goi/cmd/goipls@latest"},
		{"install", "github.com/goi-gui/goi/cmd/goifmt@latest"},
		{"get", "-u", "github.com/goi-gui/goi@latest"},
	})
}

func assertCalls(t *testing.T, got, want [][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if strings.Join(got[i], " ") != strings.Join(want[i], " ") {
			t.Fatalf("call %d = %v, want %v", i, got[i], want[i])
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
