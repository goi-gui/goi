//go:build setup || all

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
	scopes, err := setup.New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(f, &tree.Hooks{Setup: scopes.For})
	if err != nil {
		t.Fatal(err)
	}
	scopes.Bind(tr)
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
	scopes, err := setup.New(b.File.Setups, b.File.Vars, b.File.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Setup: scopes.For})
	if err != nil {
		t.Fatal(err)
	}
	scopes.Bind(tr)
	tr.ApplyExpressions()
	win := &Window{tree: tr}
	if got := MustByID[*Text](win, "own").Text.Get(); got != "main" {
		t.Fatalf("main text = %q", got)
	}
	if got := MustByID[*Text](win, "inner").Text.Get(); got != "part" {
		t.Fatalf("fragment text = %q", got)
	}
}

// TestSetupFunctionsRunFromEvents drives the counter shape: the block owns the
// state, event calls mutate it, and the next expression pass reads the new value.
func TestSetupFunctionsRunFromEvents(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`setup {
	var count = 0

	func increase() { count++ }
	func decrease() { count-- }
}

Window { Column {
	Text { id: value text: formatf("%d", count) }
	Button { id: inc label: "+" onClick: increase() }
	Button { id: dec label: "-" onClick: decrease() }
} }`), "Main.goi")
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	scopes, err := setup.New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(f, &tree.Hooks{Setup: scopes.For})
	if err != nil {
		t.Fatal(err)
	}
	scopes.Bind(tr)
	tr.ApplyExpressions()
	win := &Window{tree: tr}
	if got := MustByID[*Text](win, "value").Text.Get(); got != "0" {
		t.Fatalf("initial text = %q", got)
	}
	MustByID[*Button](win, "inc").Click.Emit(ClickEvent{})
	MustByID[*Button](win, "inc").Click.Emit(ClickEvent{})
	tr.ApplyExpressions()
	if got := MustByID[*Text](win, "value").Text.Get(); got != "2" {
		t.Fatalf("text after two increases = %q", got)
	}
	MustByID[*Button](win, "dec").Click.Emit(ClickEvent{})
	tr.ApplyExpressions()
	if got := MustByID[*Text](win, "value").Text.Get(); got != "1" {
		t.Fatalf("text after a decrease = %q", got)
	}
}

// TestSetupReadsBridge covers the setup side of the bridge: a variable read and
// a handler call, plus a return value from a handler.
func TestSetupReadsBridge(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`bridge {
	onSave(fileName string)
	getFileName() string
	content string
}

setup {
	var seen string

	func doSomething() {
		fileName := bridge.getFileName()
		bridge.onSave(fileName)
		seen = bridge.content
	}
}

Window { }`), "Main.goi")
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	scopes, err := setup.New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(f, &tree.Hooks{Setup: scopes.For})
	if err != nil {
		t.Fatal(err)
	}
	scopes.Bind(tr)
	tr.SetData("content", "hello")
	saved := ""
	if err := tr.SetHandler("getFileName", func() string { return "notes.txt" }); err != nil {
		t.Fatal(err)
	}
	if err := tr.SetHandler("onSave", func(name string) { saved = name }); err != nil {
		t.Fatal(err)
	}
	sc := scopes.For("Main.goi")
	if _, found, err := sc.Call("doSomething", nil); err != nil || !found {
		t.Fatalf("doSomething = %v, %v", found, err)
	}
	if saved != "notes.txt" {
		t.Fatalf("onSave got %q", saved)
	}
	if v, ok := sc.Value("seen"); !ok || v != "hello" {
		t.Fatalf("seen = %v, %v", v, ok)
	}
}
