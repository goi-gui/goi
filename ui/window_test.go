package ui

import (
	"errors"
	"testing"

	"github.com/ksckaan1/goi/internal/tree"
)

// loadForTest builds a Window from source without a GPU or event loop.
func loadForTest(t *testing.T, src string) (*Window, error) {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(src), nil)
	if !diag.Empty() {
		return nil, errors.New(diag.Error())
	}
	return &Window{tree: tr, title: tr.Root.Title.Get()}, nil
}

const counterUI = `Window {
	id: main
	title: "Counter"
	width: 480
	height: 320
	Column {
		padding: 16
		spacing: 8
		Text   { id: label text: "0" fontSize: 32 color: #222222 }
		Button { id: inc   label: "Increase" }
		TextField { id: name placeholder: "Your name" width: 240 }
	}
}`

func TestBuildAndLookup(t *testing.T) {
	w, err := loadForTest(t, counterUI)
	if err != nil {
		t.Fatal(err)
	}
	label, err := ByID[*Text](w, "label")
	if err != nil {
		t.Fatal(err)
	}
	if got := label.Text.Get(); got != "0" {
		t.Fatalf("text = %q", got)
	}
	if got := label.Color.Get(); got != 0x222222FF {
		t.Fatalf("color = %08x", got)
	}
	if got := label.FontSize.Get(); got != 32 {
		t.Fatalf("fontSize = %v", got)
	}

	_ = MustByID[*Button](w, "inc")
	_ = MustByID[*TextField](w, "name")
}

func TestLookupErrors(t *testing.T) {
	w, err := loadForTest(t, counterUI)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ByID[*Text](w, "missing"); err == nil {
		t.Fatal("expected an error for a missing id")
	}
	_, err = ByID[*Button](w, "label") // label is a Text
	if err == nil {
		t.Fatal("expected a type error")
	}
	if got := err.Error(); got == "" || !contains(got, "Text") {
		t.Fatalf("unhelpful error: %q", got)
	}
}

func TestMustByIDPanics(t *testing.T) {
	w, err := loadForTest(t, counterUI)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	MustByID[*Text](w, "inc")
}

func TestSetMarksDirty(t *testing.T) {
	w, err := loadForTest(t, counterUI)
	if err != nil {
		t.Fatal(err)
	}
	label := MustByID[*Text](w, "label")
	label.ClearDirty()
	label.Text.Set("1")
	kind, dirty := label.Dirty()
	if !dirty || kind != DirtyLayout {
		t.Fatalf("text change: kind=%v dirty=%v", kind, dirty)
	}

	label.ClearDirty()
	label.Color.Set(0xFF0000FF)
	kind, dirty = label.Dirty()
	if !dirty || kind != DirtyPaint {
		t.Fatalf("color change: kind=%v dirty=%v", kind, dirty)
	}
}

func TestSetSameValueIsNoOp(t *testing.T) {
	w, err := loadForTest(t, counterUI)
	if err != nil {
		t.Fatal(err)
	}
	label := MustByID[*Text](w, "label")
	label.ClearDirty()
	label.Text.Set("0") // already "0"
	if _, dirty := label.Dirty(); dirty {
		t.Fatal("setting the same value marked the node dirty")
	}
}

func TestLayoutDirtyPropagatesToAncestors(t *testing.T) {
	w, err := loadForTest(t, counterUI)
	if err != nil {
		t.Fatal(err)
	}
	label := MustByID[*Text](w, "label")

	label.Text.Set("changed")

	root := w.Tree().Root
	if _, dirty := root.Base().Dirty(); !dirty {
		t.Fatal("root should be layout-dirty after a child text change")
	}
	if k, _ := root.Base().Dirty(); k != DirtyLayout {
		t.Fatalf("root kind = %v", k)
	}
}

func TestEvents(t *testing.T) {
	w, err := loadForTest(t, counterUI)
	if err != nil {
		t.Fatal(err)
	}
	btn := MustByID[*Button](w, "inc")
	got := 0
	off := btn.OnClick(func(ClickEvent) { got++ })
	btn.Click.Emit(ClickEvent{})
	if got != 1 {
		t.Fatalf("got %d", got)
	}
	off()
	btn.Click.Emit(ClickEvent{})
	if got != 1 {
		t.Fatalf("unsubscribe failed, got %d", got)
	}
	// Unsubscribing twice must not panic or affect the count.
	off()
	btn.Click.Emit(ClickEvent{})
	if got != 1 {
		t.Fatalf("double unsubscribe changed the count: %d", got)
	}
}

func TestOnClickIsOnButton(t *testing.T) {
	w, err := loadForTest(t, counterUI)
	if err != nil {
		t.Fatal(err)
	}
	btn := MustByID[*Button](w, "inc")
	var seen ClickEvent
	btn.OnClick(func(e ClickEvent) { seen = e })
	btn.Click.Emit(ClickEvent{X: 12, Y: 34, Button: 1})
	if seen.X != 12 || seen.Y != 34 || seen.Button != 1 {
		t.Fatalf("event = %+v", seen)
	}
}

func TestDefaultsApplied(t *testing.T) {
	w, err := loadForTest(t, `Window { id: w Rectangle { id: r } }`)
	if err != nil {
		t.Fatal(err)
	}
	r := MustByID[*Rectangle](w, "r")
	if !r.Visible() {
		t.Fatal("visible default should be true")
	}
	if r.Width() != -1 {
		t.Fatalf("width default = %v, want -1 (auto)", r.Width())
	}
	if r.Margin() != 0 {
		t.Fatalf("margin default = %v", r.Margin())
	}
}

func TestChildrenAndParent(t *testing.T) {
	w, err := loadForTest(t, counterUI)
	if err != nil {
		t.Fatal(err)
	}
	nodes := w.Tree().All()
	if len(nodes) != 6 { // Window, Column, Text, Button, TextInput plus root counted twice
		// Window + Column + 3 children = 5
		if len(nodes) != 5 {
			t.Fatalf("node count = %d", len(nodes))
		}
	}
	label := MustByID[*Text](w, "label")
	if p := label.Parent(); p == nil || p.Kind() != "Column" {
		t.Fatalf("parent = %v", p)
	}
}

func TestDuplicateIDFailsBuild(t *testing.T) {
	_, err := tree.BuildSource([]byte(`Window { Text { id: a } Text { id: a } }`), nil)
	if err == nil {
		t.Fatal("expected a build error for duplicate ids")
	}
}

func TestInvalidSourceReturnsDiagnostics(t *testing.T) {
	_, err := loadForTest(t, `Window { width: "x" }`)
	if err == nil {
		t.Fatal("expected a diagnostic")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
