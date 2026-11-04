package asset

import (
	"os"
	"strings"
	"testing"

	"github.com/ksckaan1/goi/internal/schema"
	"github.com/ksckaan1/goi/internal/syntax"
	"github.com/ksckaan1/goi/internal/tree"
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
		"Main.goi": `import "./Card.goi" as Card
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
		"Main.goi": `import "./Card.goi" as Card
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
		"Main.goi": `import "./Card.goi" as Card
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
