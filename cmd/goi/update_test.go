//go:build !js && !wasip1

package main

import (
	"context"
	"errors"
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
	printDoctorWith(&out, func(context.Context) (string, error) { return "v0.0.0", nil })
	for _, want := range []string{"COMPONENT", "Update", "OS", "Go", "CGO", "goi", "goipls", "goifmt", "SDL3", "Vulkan", "ffmpeg", "git"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestUpdateCheck(t *testing.T) {
	latest := func(tag string) func(context.Context) (string, error) {
		return func(context.Context) (string, error) { return tag, nil }
	}
	cases := []struct {
		name, current, tag, status string
	}{
		{"newer", "v0.29.2", "v0.29.10", "available"},
		{"same", "v0.29.2", "v0.29.2", "ok"},
		{"newer-current", "v0.30.0", "v0.29.2", "ok"},
		{"dirty", "v0.29.2+dirty", "v0.30.0", "devel"},
		{"devel", "devel", "v0.30.0", "devel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := updateCheck(c.current, latest(c.tag)); got.Status != c.status {
				t.Fatalf("status = %q, want %q (%+v)", got.Status, c.status, got)
			}
		})
	}
	if got := updateCheck("v0.29.2", func(context.Context) (string, error) {
		return "", errors.New("offline")
	}); got.Status != "?" {
		t.Fatalf("offline status = %q, want ?", got.Status)
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("v0.29.10", "v0.29.9") <= 0 {
		t.Fatal("v0.29.10 should compare after v0.29.9")
	}
	if compareVersions("v0.29.2", "v0.29.2") != 0 {
		t.Fatal("equal versions should compare equal")
	}
	if compareVersions("devel", "v0.29.2") != 0 {
		t.Fatal("a non-release version should not compare")
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
