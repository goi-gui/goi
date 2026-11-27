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
