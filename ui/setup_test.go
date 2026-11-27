package ui

import (
	"testing"
	"testing/fstest"

	"github.com/goi-gui/goi/internal/asset"
	"github.com/goi-gui/goi/internal/setup"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

func TestSetupDrivesAProperty(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`setup {
	var name = "Kaan"

	func greet(n string) string {
		return "Hi, " + n
	}
}

Window { Text { id: label text: greet(name) } }`), "Main.goi")
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	scopes, err := setup.New(f.Setups)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(f, &tree.Hooks{Setup: scopes.For})
	if err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	label := MustByID[*Text](&Window{tree: tr}, "label")
	if got := label.Text.Get(); got != "Hi, Kaan" {
		t.Fatalf("text = %q", got)
	}
}

// TestSetupScopesFollowEachFile gives two files the same name: each element
// reads its own file's value, including a fragment expanded into the page.
func TestSetupScopesFollowEachFile(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"

setup {
	var name = "main"
}

Window { Column {
	Text { id: own text: name }
	Part { title: "x" }
} }`)},
		"Part.goi": {Data: []byte(`props {
	title string
}

setup {
	var name = "part"
}

Column { Text { id: inner text: name } }`)},
	}
	b, diag := asset.Load(fsys, "Main.goi")
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	scopes, err := setup.New(b.File.Setups)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Setup: scopes.For})
	if err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	win := &Window{tree: tr}
	if got := MustByID[*Text](win, "own").Text.Get(); got != "main" {
		t.Fatalf("main text = %q", got)
	}
	if got := MustByID[*Text](win, "inner").Text.Get(); got != "part" {
		t.Fatalf("fragment text = %q", got)
	}
}
