package ui

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ksckaan1/goi/internal/tree"
)

// TestLoadWindowFromFS loads a design from an in-memory container, which is what
// an embed.FS looks like to the framework.
func TestLoadWindowFromFS(t *testing.T) {
	fsys := fstest.MapFS{
		"ui/Main.goi": &fstest.MapFile{Data: []byte(`import "./parts/Header.goi" as Header
Window {
	title: "From FS"
	Column {
		Header { heading: "Hello" }
	}
}`)},
		"ui/parts/Header.goi": &fstest.MapFile{Data: []byte(`param heading: ""
Text { text: $heading role: "headlineSmall" }`)},
	}
	a := newHeadlessApp()
	// LoadWindow needs a platform window, so exercise the loading path the same
	// way it does and check the tree it would install.
	b, err := loadBundle(fsys, "ui/Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Marked: a.markDirty})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Root.Title.Get() != "From FS" {
		t.Fatalf("title = %q", tr.Root.Title.Get())
	}
	var got string
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			got = n.(*Text).Text.Get()
		}
	}
	if got != "Hello" {
		t.Fatalf("fragment text = %q, want the param value", got)
	}
}

// TestLoadWindowReportsFragmentErrors checks the diagnostic names the fragment
// file and the line inside it.
func TestLoadWindowReportsFragmentErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": &fstest.MapFile{Data: []byte(`import "./Part.goi" as Part
Window { Part { } }`)},
		"Part.goi": &fstest.MapFile{Data: []byte("Column {\n  nope: 1\n}\n")},
	}
	_, err := loadBundle(fsys, "Main.goi")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Part.goi:2:3") {
		t.Fatalf("err = %v, want a Part.goi position", err)
	}
}

// TestHotReloadRequiresDirectoryContainer checks the failure users will hit:
// os.DirFS hides its path, so it cannot be watched.
func TestHotReloadRequiresDirectoryContainer(t *testing.T) {
	a := newHeadlessApp()
	a.assets = fstest.MapFS{}
	if err := a.enableWatcher(); err == nil {
		t.Fatal("expected a clear error for an unwatchable container")
	} else if !strings.Contains(err.Error(), "ui.DirFS") {
		t.Fatalf("err = %v, want it to name ui.DirFS", err)
	}
}

// TestDirFSImplementsFSWithADir keeps DirFS honest: it is an fs.FS and it exposes
// the directory the watcher needs.
func TestDirFSImplementsFSWithADir(t *testing.T) {
	f := DirFS(t.TempDir())
	if _, ok := any(f).(interface{ Dir() string }); !ok {
		t.Fatal("DirFS must expose its directory")
	}
}
