//go:build setup || all

package ui

import (
	"fmt"
	"strings"
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

// TestSetupLifecycleRunsAfterLoad covers the app side: onMount is queued after
// the first frame, updates state, and onUnmount runs when the design is
// replaced.
func TestSetupLifecycleRunsAfterLoad(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`setup {
	var ready bool
	var closed bool

	func onMount() { ready = true }
	func onUnmount() { closed = true }
	func label() string {
		if ready {
			return "ready"
		}
		return "waiting"
	}
}

Window { Text { id: label text: label() } }`), "Main.goi")
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	scopes, err := setup.New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	sb := setupBuild{
		hook: scopes.For, bind: scopes.Bind, err: scopes.Err,
		instance: scopes.Note, start: scopes.Start, stop: scopes.Unmount,
	}
	tr, err := tree.Build(f, &tree.Hooks{Setup: sb.hook})
	if err != nil {
		t.Fatal(err)
	}
	noteSetups(sb, tr)
	if err := sb.err(); err != nil {
		t.Fatal(err)
	}
	sb.bind(tr)
	a := newHeadlessApp()
	a.startSetups(sb)
	label := MustByID[*Text](&Window{tree: tr}, "label")
	for i := 0; i < 10 && label.Text.Get() != "ready"; i++ {
		a.drainPosted()
		tr.ApplyExpressions()
	}
	if got := label.Text.Get(); got != "ready" {
		t.Fatalf("label = %q, mounted after %d pumps", got, 10)
	}
	a.stopSetups()
	if v, ok := scopes.For("Main.goi", "").Value("closed"); !ok || fmt.Sprint(v) != "true" {
		t.Fatalf("closed = %v, %v", v, ok)
	}
}

// TestSetupBridgeFieldsIgnoreCase keeps a Go field named ID readable when the
// design spells the shape field ID and the default mapping produces "iD".
func TestSetupBridgeFieldsIgnoreCase(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`bridge {
	users []User
}

type User {
	ID   string
	Name string
}

setup {
	func first() string {
		for _, u := range bridge.users {
			return u.ID + "/" + u.Name
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
	tr.SetData("users", []struct {
		ID   string
		Name string
	}{{ID: "1", Name: "Ada"}})
	sc := scopes.For("Main.goi", "")
	if v, _, err := sc.Call("first", nil); err != nil || v != "1/Ada" {
		t.Fatalf("first = %v, %v", v, err)
	}
	tr.SetData("users", []map[string]any{{"ID": "2", "Name": "Bob"}})
	if v, _, err := sc.Call("first", nil); err != nil || v != "2/Bob" {
		t.Fatalf("first from a map = %v, %v", v, err)
	}
}

// TestSetupBridgeFieldsIgnoreCaseFallsBack covers the same matching through the
// interpreter path.
func TestSetupBridgeFieldsIgnoreCaseFallsBack(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`bridge {
	users []User
}

type User {
	ID   string
	Name string
}

setup {
	var seen int

	type T struct{ n int }
	func (x T) Plus() int { return x.n + seen }

	func first() string {
		for _, u := range bridge.users {
			return u.ID + "/" + u.Name
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
	tr.SetData("users", []struct {
		ID   string
		Name string
	}{{ID: "3", Name: "Grace"}})
	if v, _, err := scopes.For("Main.goi", "").Call("first", nil); err != nil || v != "3/Grace" {
		t.Fatalf("first = %v, %v", v, err)
	}
}

// TestSetupReadsProps makes the values a fragment instance passed readable as
// props.name in its setup code.
func TestSetupReadsProps(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"
Window { Column {
	Part { title: "A" count: 2 }
	Part { title: "B" count: 3 }
} }`)},
		"Part.goi": {Data: []byte(`props { title string count int }

setup {
	func label() string { return fmt.Sprintf("%s:%d", props.title, props.count) }
}

Text { text: label() }`)},
	}
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
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
	var got []string
	for _, n := range tr.All() {
		if txt, ok := n.(*Text); ok {
			got = append(got, txt.Text.Get())
		}
	}
	if len(got) != 2 || got[0] != "A:2" || got[1] != "B:3" {
		t.Fatalf("texts = %v", got)
	}
}

// TestSetupReadsPropsFallsBack covers props through the interpreter path.
func TestSetupReadsPropsFallsBack(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"
Window { Column {
	Part { title: "A" count: 2 }
	Part { title: "B" count: 3 }
} }`)},
		"Part.goi": {Data: []byte(`props { title string count int }

setup {
	var seen int

	type T struct{ n int }
	func (x T) Plus() int { return x.n + seen }

	func label() string { return fmt.Sprintf("%s:%d", props.title, props.count) }
}

Text { text: label() }`)},
	}
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
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
	var got []string
	for _, n := range tr.All() {
		if txt, ok := n.(*Text); ok {
			got = append(got, txt.Text.Get())
		}
	}
	if len(got) != 2 || got[0] != "A:2" || got[1] != "B:3" {
		t.Fatalf("texts = %v", got)
	}
}

// TestSetupPropsFollowBridge checks that a prop passed as a bridge value is
// resolved when setup runs, not once at build time.
func TestSetupPropsFollowBridge(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"
bridge { name string }
Window { Part { title: bridge.name } }`)},
		"Part.goi": {Data: []byte(`props { title string }
setup {
	func label() string { return props.title }
}
Text { text: label() }`)},
	}
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
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
	tr.SetData("name", "Zed")
	tr.ApplyExpressions()
	for _, n := range tr.All() {
		if txt, ok := n.(*Text); ok {
			if got := txt.Text.Get(); got != "Zed" {
				t.Fatalf("text = %q", got)
			}
			return
		}
	}
	t.Fatal("no Text")
}

// TestSetupPropsKeepListTypes covers a prop passed from a bridge list, which
// setup reads through the shape it declared.
func TestSetupPropsKeepListTypes(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"
bridge { users []User }
type User { name string }
Window { Part { users: bridge.users } }`)},
		"Part.goi": {Data: []byte(`props { users []User }
setup {
	func summary() string {
		if len(props.users) == 0 {
			return "none"
		}
		return fmt.Sprintf("%d:%s", len(props.users), props.users[0].name)
	}
}
Text { text: summary() }`)},
	}
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
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
	tr.SetData("users", []struct {
		Name string `goi:"name"`
	}{{Name: "Ada"}, {Name: "Grace"}})
	tr.ApplyExpressions()
	for _, n := range tr.All() {
		if txt, ok := n.(*Text); ok {
			if got := txt.Text.Get(); got != "2:Ada" {
				t.Fatalf("text = %q", got)
			}
			return
		}
	}
	t.Fatal("no Text")
}

// TestSetupValuesInBlocks lets a setup variable drive if, switch and for.
func TestSetupValuesInBlocks(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`setup {
	var show bool
	var mode = "b"
	var items = []string{"x", "y"}

	func toggle() { show = !show }
}

Window {
	Column {
		if show {
			Text { text: "on" }
		}
		switch mode {
			case "a":
				Text { text: "mode-a" }
			case "b":
				Text { text: "mode-b" }
		}
		for _, item := range items {
			Text { text: item }
		}
	}
}`), "Main.goi")
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
	find := func(text string) *Text {
		for _, n := range tr.All() {
			if txt, ok := n.(*Text); ok && txt.Text.Get() == text {
				return txt
			}
		}
		return nil
	}
	if on := find("on"); on == nil || on.Visible() {
		t.Fatalf("the if arm should start hidden: %v", on != nil)
	}
	if a := find("mode-a"); a == nil || a.Visible() {
		t.Fatalf("mode-a should be hidden")
	}
	if b := find("mode-b"); b == nil || !b.Visible() {
		t.Fatalf("mode-b should be shown")
	}
	if x := find("x"); x == nil || !x.Visible() {
		t.Fatalf("the for loop should build x")
	}
	if y := find("y"); y == nil || !y.Visible() {
		t.Fatalf("the for loop should build y")
	}
	if _, _, err := scopes.For("Main.goi", "").Call("toggle", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	if on := find("on"); on == nil || !on.Visible() {
		t.Fatalf("toggle should show the if arm")
	}
}

// TestSetupLifecycleFollowsVisibility mounts a fragment only while its if arm
// is shown and unmounts it when the arm hides it.
func TestSetupLifecycleFollowsVisibility(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"
setup {
	var show bool
	func toggle() { show = !show }
}
Window { Column {
	if show { Part {} }
} }`)},
		"Part.goi": {Data: []byte(`setup {
	var mounted bool
	var count int
	func onMount() { mounted = true }
	func onUnmount() { mounted = false }
}
Text { text: formatf("%d", count) }`)},
	}
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := setup.New(b.File.Setups, b.File.Vars, b.File.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	sb := setupBuild{
		hook: scopes.For, bind: scopes.Bind, err: scopes.Err,
		instance: scopes.Note, visible: scopes.SetVisible,
		start: scopes.Start, stop: scopes.Unmount,
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Setup: sb.hook, Visibility: sb.visible})
	if err != nil {
		t.Fatal(err)
	}
	noteSetups(sb, tr)
	sb.bind(tr)
	a := newHeadlessApp()
	a.startSetups(sb)
	for i := 0; i < 8; i++ {
		a.drainPosted()
	}
	partScope := ""
	for _, n := range tr.All() {
		if base := n.Base(); base != nil && base.AST != nil && base.AST.Src == "Part.goi" {
			partScope = base.AST.Scope
			break
		}
	}
	if partScope == "" {
		t.Fatal("no Part scope")
	}
	mounted := func() string {
		v, _ := scopes.For("Part.goi", partScope).Value("mounted")
		return fmt.Sprint(v)
	}
	if got := mounted(); got != "false" {
		t.Fatalf("hidden fragment mounted: %s", got)
	}
	if _, _, err := scopes.For("Main.goi", "").Call("toggle", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	for i := 0; i < 8; i++ {
		a.drainPosted()
	}
	if got := mounted(); got != "true" {
		t.Fatalf("shown fragment did not mount: %s", got)
	}
	if _, _, err := scopes.For("Main.goi", "").Call("toggle", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	if got := mounted(); got != "false" {
		t.Fatalf("hidden fragment did not unmount: %s", got)
	}
}

// TestSetupKeepsBarePropertyLiterals guards the case where a setup block used
// to make every bare word look like a setup value, so `background: red` and an
// enum such as `role: titleLarge` were missed.
func TestSetupKeepsBarePropertyLiterals(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`setup {
	var count = 1
}
Window {
	Rectangle {
		id: box
		background: red
		borderColor: red
	}
	Text { id: label text: formatf("%d", count) role: titleLarge }
}`), "Main.goi")
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
	box, ok := tr.ByID("box")
	if !ok {
		t.Fatal("no box")
	}
	if v, _ := box.Base().PropValue("background"); v != uint32(0xff0000ff) {
		t.Fatalf("background = %#v", v)
	}
	if v, _ := box.Base().PropValue("borderColor"); v != uint32(0xff0000ff) {
		t.Fatalf("borderColor = %#v", v)
	}
	if v := MustByID[*Text](&Window{tree: tr}, "label").Role.Get(); v != "titleLarge" {
		t.Fatalf("role = %q", v)
	}
}

// TestFragmentOverrideExpressionUsesCallerSetup keeps a fragment root's literal
// defaults while the caller's visible: show comes from the caller's setup.
func TestFragmentOverrideExpressionUsesCallerSetup(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"
setup {
	var show bool
	func toggle() { show = !show }
}
Window { Part { visible: show } }`)},
		"Part.goi": {Data: []byte(`setup {
	var count = 1
}
Column {
	id: part
	visible: false
	background: red
	Text { text: formatf("%d", count) }
}`)},
	}
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
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
	part, ok := tr.ByID("part")
	if !ok {
		t.Fatal("no part")
	}
	if part.Base().Visible() {
		t.Fatal("visible: show should start hidden")
	}
	if v, _ := part.Base().PropValue("background"); v != uint32(0xff0000ff) {
		t.Fatalf("background = %#v", v)
	}
	if _, _, err := scopes.For("Main.goi", "").Call("toggle", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	if !part.Base().Visible() {
		t.Fatal("the caller's show should make the fragment visible")
	}
	if v, _ := part.Base().PropValue("background"); v != uint32(0xff0000ff) {
		t.Fatalf("background after toggle = %#v", v)
	}
}

// TestSetupPropValueUsesCallerSetup covers a prop passed as a caller setup
// value: the substituted property must resolve in the caller's file.
func TestSetupPropValueUsesCallerSetup(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"
setup {
	var name = "Kaan"
	var show = true
}
Window { Column {
	Part { title: name }
	Part { show: show }
} }`)},
		"Part.goi": {Data: []byte(`props { title string show bool }
Column {
	Text { text: props.title }
	if props.show {
		Text { text: "shown" }
	}
}`)},
	}
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := setup.New(b.File.Setups, b.File.Vars, b.File.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Setup: scopes.For})
	if err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	var texts []string
	for _, n := range tr.All() {
		if txt, ok := n.(*Text); ok && txt.Visible() {
			texts = append(texts, txt.Text.Get())
		}
	}
	joined := fmt.Sprint(texts)
	if !strings.Contains(joined, "Kaan") || !strings.Contains(joined, "shown") {
		t.Fatalf("texts = %v", texts)
	}
}

// TestSetupLifecycleFollowsVisibleProp is the same rule for `visible: show` on
// a fragment call rather than an if arm.
func TestSetupLifecycleFollowsVisibleProp(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"
setup {
	var show bool
	func toggle() { show = !show }
}
Window { Part { visible: show } }`)},
		"Part.goi": {Data: []byte(`setup {
	var mounted bool
	var count int
	func onMount() { mounted = true }
	func onUnmount() { mounted = false }
}
Text { text: formatf("%d", count) }`)},
	}
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := setup.New(b.File.Setups, b.File.Vars, b.File.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	sb := setupBuild{
		hook: scopes.For, bind: scopes.Bind, err: scopes.Err,
		instance: scopes.Note, visible: scopes.SetVisible,
		start: scopes.Start, stop: scopes.Unmount,
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Setup: sb.hook, Visibility: sb.visible})
	if err != nil {
		t.Fatal(err)
	}
	noteSetups(sb, tr)
	sb.bind(tr)
	a := newHeadlessApp()
	a.startSetups(sb)
	for i := 0; i < 8; i++ {
		a.drainPosted()
		tr.ApplyExpressions()
	}
	partScope := ""
	for _, n := range tr.All() {
		if base := n.Base(); base != nil && base.AST != nil && base.AST.Src == "Part.goi" {
			partScope = base.AST.Scope
			break
		}
	}
	mounted := func() string {
		v, _ := scopes.For("Part.goi", partScope).Value("mounted")
		return fmt.Sprint(v)
	}
	if got := mounted(); got != "false" {
		t.Fatalf("hidden fragment mounted: %s", got)
	}
	if _, _, err := scopes.For("Main.goi", "").Call("toggle", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	for i := 0; i < 8; i++ {
		a.drainPosted()
	}
	if got := mounted(); got != "true" {
		t.Fatalf("visible fragment did not mount: %s", got)
	}
	if _, _, err := scopes.For("Main.goi", "").Call("toggle", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	if got := mounted(); got != "false" {
		t.Fatalf("hidden fragment did not unmount: %s", got)
	}
}

// TestSetupWritesBridgeValues lets setup assign a bridge variable, which must
// reach the tree and every expression and loop that reads it.
func TestSetupWritesBridgeValues(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`bridge {
	myValue string
	count   int
	users   []User
}

type User { name string }

setup {
	func setIt() {
		bridge.myValue = "Kaan"
		bridge.count = 3
		bridge.users = []User{{name: "Ada"}}
	}
}

Window { Column {
	Text { id: value text: bridge.myValue }
	Text { id: count text: formatf("%d", bridge.count) }
	for _, u := range bridge.users {
		Text { text: u.name }
	}
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
	if _, _, err := scopes.For("Main.goi", "").Call("setIt", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	if v := MustByID[*Text](&Window{tree: tr}, "value").Text.Get(); v != "Kaan" {
		t.Fatalf("myValue = %q", v)
	}
	if v := MustByID[*Text](&Window{tree: tr}, "count").Text.Get(); v != "3" {
		t.Fatalf("count = %q", v)
	}
	rows := 0
	for _, n := range tr.All() {
		if txt, ok := n.(*Text); ok && txt.Text.Get() == "Ada" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("bridge list rows = %d", rows)
	}
}

// TestSetupWritesBridgeValuesFallsBack covers the same assignment through the
// interpreter path.
func TestSetupWritesBridgeValuesFallsBack(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`bridge { myValue string }

setup {
	var seen int
	type T struct{ n int }
	func (x T) Plus() int { return x.n + seen }

	func setIt() { bridge.myValue = "Kaan" }
}

Window { Text { id: value text: bridge.myValue } }`), "Main.goi")
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	scopes, err := setup.New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	if scopes.For("Main.goi", "") == nil {
		t.Fatal("fallback did not load")
	}
	tr, err := tree.Build(f, &tree.Hooks{Setup: scopes.For})
	if err != nil {
		t.Fatal(err)
	}
	scopes.Bind(tr)
	if _, _, err := scopes.For("Main.goi", "").Call("setIt", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	if v := MustByID[*Text](&Window{tree: tr}, "value").Text.Get(); v != "Kaan" {
		t.Fatalf("myValue = %q", v)
	}
}

// TestSetupWritesBridgeFromMount covers the hook flush path.
func TestSetupWritesBridgeFromMount(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`bridge { ready bool }
setup {
	func onMount() { bridge.ready = true }
}
Window { Text { id: value text: formatf("%v", bridge.ready) } }`), "Main.goi")
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	scopes, err := setup.New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	sb := setupBuild{
		hook: scopes.For, bind: scopes.Bind, err: scopes.Err,
		instance: scopes.Note, visible: scopes.SetVisible,
		start: scopes.Start, stop: scopes.Unmount,
	}
	tr, err := tree.Build(f, &tree.Hooks{Setup: sb.hook, Visibility: sb.visible})
	if err != nil {
		t.Fatal(err)
	}
	noteSetups(sb, tr)
	sb.bind(tr)
	a := newHeadlessApp()
	a.startSetups(sb)
	for i := 0; i < 10; i++ {
		a.drainPosted()
		tr.ApplyExpressions()
	}
	if v := MustByID[*Text](&Window{tree: tr}, "value").Text.Get(); v != "true" {
		t.Fatalf("ready = %q", v)
	}
}

// TestSetupBridgeWriteSurvivesHandlerData is the logout case: setup assigns a
// bridge value and then calls a handler that writes different data. The
// handler's data refresh must not overwrite the pending assignment.
func TestSetupBridgeWriteSurvivesHandlerData(t *testing.T) {
	f, diag := syntax.ParseNamed([]byte(`bridge {
	loggedout bool
	activePage string
	onLogout func()
}

setup {
	func logout() {
		bridge.loggedout = true
		bridge.onLogout()
	}
}

Window { Text { id: page text: bridge.activePage } }`), "Main.goi")
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
	if err := tr.SetHandler("onLogout", func() { tr.SetData("activePage", "auth") }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scopes.For("Main.goi", "").Call("logout", nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := tr.Data("loggedout"); v != true {
		t.Fatalf("loggedout = %v", v)
	}
	if v, _ := tr.Data("activePage"); v != "auth" {
		t.Fatalf("activePage = %v", v)
	}
}

// TestSetupScheduleRequestsAFrame keeps a mount from waiting for the next
// input: each queued hop must mark the tree dirty before it sleeps.
func TestSetupScheduleRequestsAFrame(t *testing.T) {
	a := newHeadlessApp()
	sb := setupBuild{start: func(func(func()), func(error)) {}}
	a.startSetups(sb)
	a.needsFrame = false
	a.drainPosted()
	if !a.needsFrame {
		t.Fatal("the second mount hop did not request a frame")
	}
	a.needsFrame = false
	a.setupPost(func() {})
	if !a.needsFrame {
		t.Fatal("a later mount did not request a frame")
	}
}

// TestSetupBareWordsDoNotMountAFile is the device-list case: a hidden
// fragment with bare literals such as `cursor: pointer` used to create its
// setup scope just to ask whether the word was a setup value, and mounted
// onMount before the fragment was shown.
func TestSetupBareWordsDoNotMountAFile(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"
bridge { page string }
Window { switch bridge.page {
	case "part":
		Part {}
} }`)},
		"Part.goi": {Data: []byte(`setup {
	var mounted bool
	func onMount() { mounted = true }
}
Column {
	cursor:     pointer
	background: red
	Text { text: "part" }
}`)},
	}
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := setup.New(b.File.Setups, b.File.Vars, b.File.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	sb := setupBuild{
		hook: scopes.For, has: scopes.Has, bind: scopes.Bind, err: scopes.Err,
		instance: scopes.Note, visible: scopes.SetVisible,
		start: scopes.Start, stop: scopes.Unmount,
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Setup: sb.hook, SetupHas: sb.has, Visibility: sb.visible})
	if err != nil {
		t.Fatal(err)
	}
	noteSetups(sb, tr)
	sb.bind(tr)
	a := newHeadlessApp()
	a.startSetups(sb)
	for i := 0; i < 10; i++ {
		a.drainPosted()
		tr.ApplyExpressions()
	}
	partScope := ""
	for _, n := range tr.All() {
		if base := n.Base(); base != nil && base.AST != nil && base.AST.Src == "Part.goi" && base.AST.Scope != "" {
			partScope = base.AST.Scope
			break
		}
	}
	if partScope == "" {
		t.Fatal("no Part scope")
	}
	mounted := func() string {
		v, _ := scopes.For("Part.goi", partScope).Value("mounted")
		return fmt.Sprint(v)
	}
	if got := mounted(); got != "false" {
		t.Fatalf("hidden Part mounted: %s", got)
	}
	tr.SetData("page", "part")
	tr.ApplyExpressions()
	for i := 0; i < 10; i++ {
		a.drainPosted()
		tr.ApplyExpressions()
	}
	if got := mounted(); got != "true" {
		t.Fatalf("shown Part did not mount: %s", got)
	}
}
