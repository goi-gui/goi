//go:build setup || all

package ui

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/goi-gui/goi/internal/setup"
	"github.com/goi-gui/goi/internal/tree"
)

// TestSetupFeatureMatrix exercises the setup features together: shared state
// per fragment, props, bridge values and handlers, typed bridge lists, setup
// values in if/switch/for, auto-imports, lifecycle visibility, and the literal
// properties that a setup block used to turn into expressions.
func TestSetupFeatureMatrix(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": {Data: []byte(`import Part "./Part.goi"

setup {
	var show bool
	var mode = "compact"
	var items = []string{"one", "two"}

	func toggle() { show = !show }
}

bridge {
	users []User
	count int
	onOpen(title string)
	getTitle() string
}

type User {
	ID    string
	name  string
	score int
	tags  []string
}

Window {
	Column {
		Button { label: "toggle" onClick: toggle() }

		Part { title: "A" count: 2 show: show visible: show }

		if show {
			Text { id: ifText text: "shown" }
		}

		switch mode {
			case "compact":
				Text { id: modeCompact text: "compact" }
			case "full":
				Text { id: modeFull text: "full" }
		}

		for _, item := range items {
			Text { text: item }
		}

		Rectangle { id: bg background: red borderColor: red }

		Text { id: enum role: titleLarge text: "x" }

		Text { id: users text: formatf("%d", len(bridge.users)) }
	}
}`)},
		"Part.goi": {Data: []byte(`props { title string count int show bool }

setup {
	import "sort" // unused on purpose

	var mounted bool
	var local = "p"

	type T struct{ n int }
	func (t T) Plus() int { return t.n }

	func onMount() { mounted = true }
	func onUnmount() { mounted = false }

	func label() string {
		return fmt.Sprintf("%s:%d:%v:%s", props.title, props.count, mounted, local)
	}

	func sum(xs ...int) int {
		n := 0
		for _, x := range xs {
			n += x
		}
		return n
	}

	func first() string {
		if len(bridge.users) == 0 {
			return ""
		}
		return bridge.users[0].name
	}

	func firstID() string {
		if len(bridge.users) == 0 {
			return ""
		}
		return bridge.users[0].ID
	}

	func callHandler() {
		bridge.onOpen(bridge.getTitle())
	}
}

Column {
	Text { text: label() }

	if props.show {
		Text { text: "inner" }
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
	if err := sb.err(); err != nil {
		t.Fatal(err)
	}
	sb.bind(tr)
	a := newHeadlessApp()
	a.startSetups(sb)
	pump := func() {
		for i := 0; i < 10; i++ {
			a.drainPosted()
			tr.ApplyExpressions()
		}
	}
	pump()

	partScope := ""
	for _, n := range tr.All() {
		base := n.Base()
		if base == nil || base.AST == nil || base.AST.Src != "Part.goi" {
			continue
		}
		if base.AST.Scope != "" {
			partScope = base.AST.Scope
			break
		}
	}
	if partScope == "" {
		t.Fatal("no Part scope")
	}
	part := func() interface {
		Value(string) (any, bool)
		Call(string, []any) (any, bool, error)
	} {
		return scopes.For("Part.goi", partScope)
	}
	mounted := func() string {
		v, _ := part().Value("mounted")
		return fmt.Sprint(v)
	}
	findText := func(want string) *Text {
		for _, n := range tr.All() {
			if txt, ok := n.(*Text); ok && txt.Text.Get() == want {
				return txt
			}
		}
		return nil
	}

	// Hidden by visible: show, so no mount and no inner prop arm.
	if got := mounted(); got != "false" {
		t.Fatalf("hidden Part mounted: %s", got)
	}
	if txt := findText("A:2:false:p"); txt == nil {
		t.Fatalf("props did not reach setup state")
	}
	if txt := findText("inner"); txt != nil && txt.Visible() {
		t.Fatal("props.show was true while hidden")
	}
	// A file with setup keeps its literal properties.
	if v, _ := tr.ByID("bg"); v == nil {
		t.Fatal("no background rectangle")
	} else if bg, _ := v.Base().PropValue("background"); bg != uint32(0xff0000ff) {
		t.Fatalf("background = %#v", bg)
	}
	if role := MustByID[*Text](&Window{tree: tr}, "enum").Role.Get(); role != "titleLarge" {
		t.Fatalf("role = %q", role)
	}
	if mode := MustByID[*Text](&Window{tree: tr}, "modeCompact").Text.Get(); mode != "compact" {
		t.Fatalf("switch = %q", mode)
	}
	if full := MustByID[*Text](&Window{tree: tr}, "modeFull"); full.Visible() {
		t.Fatal("the other switch arm should stay hidden")
	}
	for _, want := range []string{"one", "two"} {
		if txt := findText(want); txt == nil || !txt.Visible() {
			t.Fatalf("for loop did not show %q", want)
		}
	}

	// Bridge values, a typed list and handlers are visible from the fragment.
	type appUser struct {
		ID    string
		Name  string
		Score int
		Tags  []string
	}
	tr.SetData("users", []appUser{
		{ID: "u1", Name: "Ada", Score: 7, Tags: []string{"x", "y"}},
	})
	tr.SetData("count", 3)
	opened := ""
	if err := tr.SetHandler("getTitle", func() string { return "hello" }); err != nil {
		t.Fatal(err)
	}
	if err := tr.SetHandler("onOpen", func(title string) { opened = title }); err != nil {
		t.Fatal(err)
	}
	if v, _, err := part().Call("first", nil); err != nil || v != "Ada" {
		t.Fatalf("bridge list name = %v, %v", v, err)
	}
	if v, _, err := part().Call("firstID", nil); err != nil || v != "u1" {
		t.Fatalf("bridge list ID = %v, %v", v, err)
	}
	if v, _, err := part().Call("sum", []any{1, 2, 3}); err != nil || fmt.Sprint(v) != "6" {
		t.Fatalf("variadic sum = %v, %v", v, err)
	}
	if _, _, err := part().Call("callHandler", nil); err != nil || opened != "hello" {
		t.Fatalf("handler opened = %q, %v", opened, err)
	}
	tr.ApplyExpressions()
	if users := MustByID[*Text](&Window{tree: tr}, "users").Text.Get(); users != "1" {
		t.Fatalf("bridge list length = %q", users)
	}

	// Showing the fragment mounts it and resolves props.show.
	if _, _, err := scopes.For("Main.goi", "").Call("toggle", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	pump()
	if got := mounted(); got != "true" {
		t.Fatalf("shown Part did not mount: %s", got)
	}
	if txt := findText("A:2:true:p"); txt == nil || !txt.Visible() {
		t.Fatalf("mounted props were not visible")
	}
	if inner := findText("inner"); inner == nil || !inner.Visible() {
		t.Fatal("props.show did not show the inner arm")
	}
	if ifText := MustByID[*Text](&Window{tree: tr}, "ifText"); !ifText.Visible() {
		t.Fatal("setup show did not show the if arm")
	}

	// Hiding it unmounts, and the bridge list stays typed while hidden.
	if _, _, err := scopes.For("Main.goi", "").Call("toggle", nil); err != nil {
		t.Fatal(err)
	}
	tr.ApplyExpressions()
	if got := mounted(); got != "false" {
		t.Fatalf("hidden Part did not unmount: %s", got)
	}
}
