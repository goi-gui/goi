//go:build !js && !wasip1

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func flagProject(t *testing.T, design string, config string) string {
	t.Helper()
	dir := buildProjectDir(t, config)
	if err := os.WriteFile(filepath.Join(dir, "assets", "ui", "Main.goi"), []byte(design), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolveFlagsAutoFindsFeatures(t *testing.T) {
	dir := flagProject(t, `Window {
		Column {
			SVG { src: "goi://images/goi.svg" }
			Mermaid { data: "graph TD; A-->B" }
			Image { src: "https://example.com/cover.jpg" }
			Text { text: "hello" }
		}
	}`, "name: app\nrunFlags: [auto, abc, myflag]\n")
	flags, err := resolveFlags(dir, "assets", []string{"auto", "abc", "myflag"})
	if err != nil {
		t.Fatal(err)
	}
	wantTags := []string{"svg", "mermaid", "net", "abc", "myflag"}
	if !reflect.DeepEqual(flags.Tags, wantTags) || !reflect.DeepEqual(flags.Display, wantTags) {
		t.Fatalf("flags = %+v, want tags/display %v", flags, wantTags)
	}
	if !flags.Auto || flags.FeatureKey != "svg,mermaid,net" {
		t.Fatalf("auto = %v key = %q", flags.Auto, flags.FeatureKey)
	}
}

func TestResolveFlagsAutoDropsStaleFeatureTags(t *testing.T) {
	dir := flagProject(t, "Window { }\n", "name: app\n")
	flags, err := resolveFlags(dir, "assets", []string{"auto", "mermaid", "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(flags.Tags, []string{"abc"}) {
		t.Fatalf("tags = %v, want the custom flag alone", flags.Tags)
	}
	if flags.FeatureKey != "" || !flags.Auto {
		t.Fatalf("flags = %+v", flags)
	}
}

func TestResolveFlagsAutoExpandsEmojiAndCode(t *testing.T) {
	dir := flagProject(t, `props { Title string }
Window {
	Column {
		Text { text: "😀" }
		Code { text: "package main" }
		Markdown { text: "# hi" }
	}
}`, "name: app\n")
	flags, err := resolveFlags(dir, "assets", []string{"auto"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"emoji", "markdown", "code"}
	if !reflect.DeepEqual(flags.Tags, want) {
		t.Fatalf("tags = %v, want %v", flags.Tags, want)
	}
}

func TestResolveFlagsAllWinsAndExpandsForTheLog(t *testing.T) {
	dir := flagProject(t, "Window { }\n", "name: app\n")
	flags, err := resolveFlags(dir, "assets", []string{"auto", "all", "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(flags.Tags, []string{"all", "abc"}) {
		t.Fatalf("tags = %v, want [all abc]", flags.Tags)
	}
	wantDisplay := append(append([]string{}, allFeatures...), "abc")
	if !reflect.DeepEqual(flags.Display, wantDisplay) {
		t.Fatalf("display = %v, want %v", flags.Display, wantDisplay)
	}
	if flags.Auto {
		t.Fatal("all should not start the auto watcher")
	}
}

func TestResolveFlagsRetiresDialog(t *testing.T) {
	dir := flagProject(t, "Window { }\n", "name: app\n")
	flags, err := resolveFlags(dir, "assets", []string{"auto", "dialog", "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(flags.Tags, []string{"abc"}) {
		t.Fatalf("tags = %v, dialog should be retired", flags.Tags)
	}
}

func TestResolveFlagsLeavesCustomFlagsAlone(t *testing.T) {
	dir := flagProject(t, "Window { }\n", "name: app\n")
	flags, err := resolveFlags(dir, "assets", []string{"abc", "myflag"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(flags.Tags, []string{"abc", "myflag"}) || flags.Auto {
		t.Fatalf("flags = %+v", flags)
	}
}

func TestLogTagsExpandsAllWithoutWritingAll(t *testing.T) {
	var buf bytes.Buffer
	logTags(&buf, "run", resolvedFlags{Display: append(append([]string{}, allFeatures...), "abc")})
	line := buf.String()
	if strings.Contains(line, "all") {
		t.Fatalf("log kept all: %q", line)
	}
	for _, tag := range append(append([]string{}, allFeatures...), "abc") {
		if !strings.Contains(line, tag) {
			t.Fatalf("log missing %q: %q", tag, line)
		}
	}
}

func TestBuildProjectAutoUsesDetectedTags(t *testing.T) {
	dir := flagProject(t, `Window { Column { Markdown { text: "# hi" } SVG { src: "goi://images/goi.svg" } } }`,
		"name: myapp\nbuildFlags: [auto, abc]\n")
	var args []string
	_, err := buildProject(dir, runtime.GOOS, runtime.GOARCH, func(_ string, a []string, _, _ io.Writer, _ []string) error {
		args = a
		return nil
	}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) < 3 || args[0] != "build" || args[1] != "-tags" {
		t.Fatalf("build args = %v", args)
	}
	if got := args[2]; !strings.Contains(got, "nofakecgo") || !strings.Contains(got, "svg") ||
		!strings.Contains(got, "markdown") || !strings.Contains(got, "abc") {
		t.Fatalf("tags = %q", got)
	}
}

func TestResolveFlagsAutoDetectsTerminal(t *testing.T) {
	dir := flagProject(t, `Window { Terminal { width: 640 height: 320 } }`, "name: app\n")
	flags, err := resolveFlags(dir, "assets", []string{"auto"})
	if err != nil {
		t.Fatal(err)
	}
	if len(flags.Tags) != 1 || flags.Tags[0] != "terminal" {
		t.Fatalf("tags = %v, want the terminal font", flags.Tags)
	}
}

func TestResolveFlagsAutoDetectsSetup(t *testing.T) {
	dir := flagProject(t, `setup {
	var name = "Kaan"
}

Window { Text { text: name } }`, "name: app\n")
	flags, err := resolveFlags(dir, "assets", []string{"auto"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(flags.Tags, []string{"setup"}) {
		t.Fatalf("tags = %v, want setup", flags.Tags)
	}

	plain := flagProject(t, `Window { }`, "name: app\n")
	flags, err = resolveFlags(plain, "assets", []string{"auto"})
	if err != nil {
		t.Fatal(err)
	}
	if len(flags.Tags) != 0 {
		t.Fatalf("setup was detected without a block: %v", flags.Tags)
	}
}
