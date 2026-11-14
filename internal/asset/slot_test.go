package asset

import (
	"os"
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

// TestPreviewFillsPropsWithDefaults is what goicheck uses to lay a fragment out
// on its own: `props.title` becomes the default before the tree is built.
func TestPreviewFillsPropsWithZeroValues(t *testing.T) {
	dir := design(t, map[string]string{
		"Card.goi": `props { title string}
Column {
	Text { text: props.title }
}`,
	})
	b, diag := Load(os.DirFS(dir), "Card.goi")
	if !diag.Empty() {
		t.Fatal(diag.FormatFiles("Card.goi", b.Sources["Card.goi"], b.Sources))
	}
	p, diag := Preview(b.File)
	if !diag.Empty() {
		t.Fatal(diag.FormatFiles("Card.goi", b.Sources["Card.goi"], b.Sources))
	}
	if _, diag := schema.Validate(p); !diag.Empty() {
		t.Fatalf("the preview is not valid: %v", diag)
	}
	if txt := p.Root.Children()[0].Prop("text"); txt == nil {
		t.Fatal("the preview has no text property")
	} else if str, ok := txt.Value.(*syntax.String); !ok || str.Val != "" {
		t.Fatalf("text = %#v, want the zero value", txt.Value)
	}
}

// TestNamedSlotReceivesItsElements: `$header { ... }` in a call lands where the
// fragment writes a bare `$header`, and plain children still go to `$children`.
func TestNamedSlotReceivesItsElements(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Card "./Card.goi"
Window {
	Card {
		$header {
			Text { text: "head" }
		}
		Text { text: "body" }
	}
}`,
		"Card.goi": `Column {
	$header
	$children
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			order = append(order, n.(*tree.Text).Text.Get())
		}
	}
	if len(order) != 2 || order[0] != "head" || order[1] != "body" {
		t.Fatalf("texts = %v, want [head body]", order)
	}
}

// TestUnknownNamedSlotIsAnError points at the call, not at the fragment.
func TestUnknownNamedSlotIsAnError(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Card "./Card.goi"
Window {
	Card {
		$footer {
			Text { text: "x" }
		}
	}
}`,
		"Card.goi": `Column {
	$header
}`,
	})
	b, diag := Load(os.DirFS(dir), "Main.goi")
	if diag.Empty() {
		t.Fatal("a slot the fragment does not declare was accepted")
	}
	got := diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources)
	if !strings.Contains(got, "has no slot $footer") || !strings.Contains(got, "Main.goi:4") {
		t.Fatalf("diagnostic = %s", got)
	}
}

// TestNamedSlotWithNoElementsIsFine: an empty `$header { }` still counts as
// filling the slot, so a conditional header can be written as nothing.
func TestNamedSlotWithNoElementsIsFine(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Card "./Card.goi"
Window {
	Card {
		$header {
		}
	}
}`,
		"Card.goi": `Column {
	$header
	height: 10
}`,
	})
	if _, err := load(t, dir, "Main.goi"); err != nil {
		t.Fatal(err)
	}
}

// TestPreviewStandsInForACompositeProp keeps `goicheck -layout` honest on a
// fragment it opens alone: a handler, a slice and a map have no zero literal, so
// the preview aliases each to a bridge variable of the same type.
func TestPreviewStandsInForACompositeProp(t *testing.T) {
	dir := design(t, map[string]string{
		"Counter.goi": `props {
	Rows   []Row
	Labels map[string]string
	OnStep func(delta int)
}
type Row { Title string }
Column {
	for _, row := range props.Rows {
		Text { text: row.Title onClick: props.OnStep(1) }
	}
	for k, v := range props.Labels {
		Text { text: formatf("%s=%s", k, v) }
	}
}`,
	})
	b, diag := Load(os.DirFS(dir), "Counter.goi")
	if !diag.Empty() {
		t.Fatal(diag.FormatFiles("Counter.goi", b.Sources["Counter.goi"], b.Sources))
	}
	p, diag := Preview(b.File)
	if !diag.Empty() {
		t.Fatal(diag.FormatFiles("Counter.goi", b.Sources["Counter.goi"], b.Sources))
	}
	if _, diag := schema.Validate(p); !diag.Empty() {
		t.Fatalf("the preview is not valid: %v", diag)
	}
	for _, name := range []string{"Rows", "Labels", "OnStep"} {
		if p.Var(name) == nil {
			t.Fatalf("the preview has no stand-in bridge variable %q", name)
		}
	}
}

// TestNestedCallSlotBelongsToTheCall keeps an outer slot out of a nested
// fragment call: `$header` inside Inner { … } is Inner's argument, not a slot of
// the fragment the caller is filling, so the caller's `$header` has nowhere to
// go and says so.
func TestNestedCallSlotBelongsToTheCall(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Card "./Card.goi"
Window {
	Card {
		$header {
			Text { text: "outer" }
		}
	}
}`,
		"Card.goi": `import Inner "./Inner.goi"
Column {
	$children

	Inner {
		$header {
			Text { text: "inner" }
		}
	}
}`,
		"Inner.goi": `Column {
	$header
	$children
}`,
	})
	b, diag := Load(os.DirFS(dir), "Main.goi")
	if diag.Empty() {
		t.Fatal("the outer $header was accepted as a slot of a nested call")
	}
	got := diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources)
	if !strings.Contains(got, "Card has no slot $header") {
		t.Fatalf("diagnostic = %s", got)
	}
}

// TestSlotInEntryFileIsAnError: a slot nobody can fill is a mistake, not a
// silent hole.
func TestSlotInEntryFileIsAnError(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `Window {
	Column {
		$children
	}
}`,
	})
	b, diag := Load(os.DirFS(dir), "Main.goi")
	if diag.Empty() {
		t.Fatal("a slot in the file the app loads was accepted")
	}
	if got := diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources); !strings.Contains(got, "is a slot") {
		t.Fatalf("diagnostic = %s", got)
	}
}

// TestFragmentSlotTakesNoContent: `$header { … }` inside a fragment reads like a
// call filling a slot; in the fragment it is a declaration, and it cannot carry
// content of its own.
func TestFragmentSlotTakesNoContent(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Card "./Card.goi"
Window { Card { Text { text: "x" } } }`,
		"Card.goi": `Column {
	$children {
		Text { text: "default" }
	}
}`,
	})
	b, diag := Load(os.DirFS(dir), "Main.goi")
	if diag.Empty() {
		t.Fatal("a slot with content in a fragment was accepted")
	}
	if got := diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources); !strings.Contains(got, "takes no properties or children") {
		t.Fatalf("diagnostic = %s", got)
	}
}

// TestSlotFillTakesNoProperties: the wrapper at a call passes elements, so a
// property on it would be silently ignored.
func TestSlotFillTakesNoProperties(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Card "./Card.goi"
Window {
	Card {
		$header {
			width: 10
		}
	}
}`,
		"Card.goi": `Column { $header }`,
	})
	b, diag := Load(os.DirFS(dir), "Main.goi")
	if diag.Empty() {
		t.Fatal("a property on a slot fill was accepted")
	}
	if got := diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources); !strings.Contains(got, "takes elements only") {
		t.Fatalf("diagnostic = %s", got)
	}
}

// TestFragmentForwardsItsChildren is the other half of the nested-call rule: a
// bare slot written inside a nested call hands the outer children on to it, so
// a wrapper fragment can pass what it was given without naming a child.
func TestFragmentForwardsItsChildren(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Wrapper "./Wrapper.goi"
Window {
	Wrapper {
		Text { text: "passed" }
	}
}`,
		"Wrapper.goi": `import Inner "./Inner.goi"
Column {
	Inner {
		$children
	}
}`,
		"Inner.goi": `Column {
	$children
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			texts = append(texts, n.(*tree.Text).Text.Get())
		}
	}
	if len(texts) != 1 || texts[0] != "passed" {
		t.Fatalf("texts = %v, want the forwarded child", texts)
	}
}

// TestARepeatedSlotIsAnError is why a slot is written once: the same children
// cannot land in two places in one tree, and a child with an id would be
// declared twice. A repeated slot is a copy-paste mistake, so it is reported
// rather than half-filled.
func TestARepeatedSlotIsAnError(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Two "./Two.goi"
Window {
	Two {
		Text { text: "same" }
	}
}`,
		"Two.goi": `Column {
	$children
	Rectangle { height: 2 }
	$children
}`,
	})
	b, diag := Load(os.DirFS(dir), "Main.goi")
	if diag.Empty() {
		t.Fatal("a repeated slot was accepted")
	}
	if got := diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources); !strings.Contains(got, "appears more than once") {
		t.Fatalf("diagnostic = %s", got)
	}
}

// TestBareSlotAtACallIsAnError: `$header` is a marker in a fragment body, and a
// fragment's own marker is gone before its call expands, so one written at a
// call would pass nothing and read as a slot that silently lost its children.
func TestBareSlotAtACallIsAnError(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Card "./Card.goi"
Window {
	Card {
		$header
	}
}`,
		"Card.goi": `Column { $header }`,
	})
	b, diag := Load(os.DirFS(dir), "Main.goi")
	if diag.Empty() {
		t.Fatal("a bare slot at a call was accepted")
	}
	if got := diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources); !strings.Contains(got, "passes nothing") {
		t.Fatalf("diagnostic = %s", got)
	}
}

// TestForwardedNamedSlotKeepsItsName: a wrapper that writes `Inner { $header }`
// hands the caller's `$header` to Inner's `$header`, not to Inner's plain
// children.
func TestForwardedNamedSlotKeepsItsName(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Wrapper "./Wrapper.goi"
Window {
	Wrapper {
		$header {
			Text { text: "head" }
		}
		Text { text: "body" }
	}
}`,
		"Wrapper.goi": `import Inner "./Inner.goi"
Column {
	Inner {
		$header
		$children
	}
}`,
		"Inner.goi": `Column {
	$header
	$children
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			got = append(got, n.(*tree.Text).Text.Get())
		}
	}
	// Inner puts the header before the body, and that is the order the caller
	// wrote them in.
	if len(got) != 2 || got[0] != "head" || got[1] != "body" {
		t.Fatalf("texts = %v, want [head body]", got)
	}
}

// TestPassedChildKeepsItsOwnImports is the alias-shadowing bug: a child written
// in the caller is resolved in the caller's file, and the fragment's import
// aliases must not touch it afterwards. Here the caller passes a `Card`
// component to a wrapper that happens to import a fragment under the same alias;
// the child must stay the component.
func TestPassedChildKeepsItsOwnImports(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Wrapper "./Wrapper.goi"
Window {
	Wrapper {
		Card { label: "from caller" }
	}
}`,
		"Wrapper.goi": `import Card "./Card.goi"
Column {
	$children
}`,
		"Card.goi": `Rectangle { width: 10 height: 10 }`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	var cards, rects int
	for _, n := range tr.All() {
		switch n.Kind() {
		case "Card":
			cards++
		case "Rectangle":
			rects++
		}
	}
	if cards != 1 || rects != 0 {
		t.Fatalf("tree has %d cards and %d rectangles; the child was read as the fragment", cards, rects)
	}
}
