//go:build setup || all

package ui

import (
	"fmt"
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
	sc := scopes.For("Main.goi", "")
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

// TestSetupIsIndependentPerFragmentInstance is the three-counter case: each
// call of a fragment gets its own setup variables.
func TestSetupIsIndependentPerFragmentInstance(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"

Window { Column {
	Part {}
	Part {}
	Part {}
} }`)},
		"Part.goi": {Data: []byte(`setup {
	var count int

	func inc() { count++ }
}

Column {
	Text { text: count }
	Button { label: "inc" onClick: inc() }
}`)},
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
	var buttons []*tree.Button
	var texts []*tree.Text
	for _, n := range tr.All() {
		switch n := n.(type) {
		case *tree.Button:
			buttons = append(buttons, n)
		case *tree.Text:
			texts = append(texts, n)
		}
	}
	if len(buttons) != 3 || len(texts) != 3 {
		t.Fatalf("buttons=%d texts=%d, want three each", len(buttons), len(texts))
	}
	// The first instance gets its own interpreter; the other two still share
	// the untouched base and must stay at zero.
	buttons[0].Click.Emit(ClickEvent{})
	tr.ApplyExpressions()
	if got := []string{texts[0].Text.Get(), texts[1].Text.Get(), texts[2].Text.Get()}; got[0] != "1" || got[1] != "0" || got[2] != "0" {
		t.Fatalf("after first click = %v", got)
	}
	buttons[1].Click.Emit(ClickEvent{})
	buttons[2].Click.Emit(ClickEvent{})
	tr.ApplyExpressions()
	for i, text := range texts {
		if got := text.Text.Get(); got != "1" {
			t.Fatalf("instance %d count = %q, want 1", i, got)
		}
	}
}

// TestSetupBridgeListKeepsItsType is the case where setup walks a bridge list:
// the design declares []ticket, so the rows arrive as ticket values with their
// declared fields, not as any.
func TestSetupBridgeListKeepsItsType(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`bridge {
	tickets []ticket
	labels  []string
	scores  map[string]int
}

type ticket {
	title  string
	tags   []string
	unread bool
}

setup {
	func count() int { return len(bridge.tickets) }

	func joined() string {
		s := ""
		for _, l := range bridge.labels {
			s += l
		}
		return s
	}

	func total() int {
		n := 0
		for _, v := range bridge.scores {
			n += v
		}
		return n
	}

	func unread() int {
		n := 0
		for _, t := range bridge.tickets {
			if t.unread {
				n++
			}
			n += len(t.tags)
		}
		return n
	}

	func titles(prefix string) []string {
		out := []string{}
		for _, t := range bridge.tickets {
			out = append(out, prefix+t.title)
		}
		return out
	}
}

Window { Text { id: label text: formatf("%d", count()) } }`), "Main.goi")
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
	type ticket struct {
		Title  string   `goi:"title"`
		Tags   []string `goi:"tags"`
		Unread bool     `goi:"unread"`
	}
	tr.SetData("tickets", []ticket{
		{Title: "VPN", Tags: []string{"net"}, Unread: true},
		{Title: "Printer", Unread: false},
	})
	tr.SetData("labels", []string{"a", "b"})
	tr.SetData("scores", map[string]int{"x": 2, "y": 3})
	sc := scopes.For("Main.goi", "")
	if v, _, err := sc.Call("joined", nil); err != nil || v != "ab" {
		t.Fatalf("joined = %v, %v", v, err)
	}
	if v, _, err := sc.Call("total", nil); err != nil || fmt.Sprint(v) != "5" {
		t.Fatalf("total = %v, %v", v, err)
	}
	if v, _, err := sc.Call("count", nil); err != nil || fmt.Sprint(v) != "2" {
		t.Fatalf("count = %v, %v", v, err)
	}
	if v, _, err := sc.Call("unread", nil); err != nil || fmt.Sprint(v) != "2" {
		t.Fatalf("unread = %v, %v", v, err)
	}
	v, _, err := sc.Call("titles", []any{"#"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.([]string)
	if len(got) != 2 || got[0] != "#VPN" || got[1] != "#Printer" {
		t.Fatalf("titles = %#v, %v", v, err)
	}
}

// TestSetupBridgeListFallsBack covers the interpreter path: a method that reads
// setup state keeps the file there, and the list is still typed.
func TestSetupBridgeListFallsBack(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`bridge {
	tickets []ticket
}

type ticket {
	title string
}

setup {
	var seen int

	type counter struct{ n int }
	func (c counter) Total() int { return c.n + seen }

	func bump() { seen++ }
	func count() int { return len(bridge.tickets) }
	func first() string {
		for _, t := range bridge.tickets {
			return t.title
		}
		return ""
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
	tr.SetData("tickets", []struct {
		Title string `goi:"title"`
	}{{Title: "VPN"}})
	sc := scopes.For("Main.goi", "")
	if _, _, err := sc.Call("bump", nil); err != nil {
		t.Fatal(err)
	}
	if v, _, err := sc.Call("count", nil); err != nil || fmt.Sprint(v) != "1" {
		t.Fatalf("count = %v, %v", v, err)
	}
	if v, _, err := sc.Call("first", nil); err != nil || v != "VPN" {
		t.Fatalf("first = %v, %v", v, err)
	}
}
