package asset

import (
	"os"
	"testing"

	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

// TestTwoInstancesKeepTheirOwnIDs is the point of scoping: a fragment with an
// id inside it can be used twice, because the instance's id namespaces it.
func TestTwoInstancesKeepTheirOwnIDs(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Card "./Card.goi"
Window {
	Column {
		Card { id: first label: "one" }
		Card { id: second label: "two" }
	}
}`,
		"Card.goi": `props { label string }
Column {
	Text { id: title text: props.label }
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"first.title": "one", "second.title": "two"} {
		n, ok := tr.ByID(id)
		if !ok {
			t.Fatalf("%q is not registered:\n%s", id, dump(tr))
		}
		if got := n.(*tree.Text).Text.Get(); got != want {
			t.Fatalf("%s text = %q, want %q", id, got, want)
		}
	}
}

// TestScopedReferencesFollowTheInstance keeps `side.width` pointing at the
// instance's own `side`, not at the other copy's.
func TestScopedReferencesFollowTheInstance(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Bar "./Bar.goi"
Window {
	Column {
		Bar { id: left }
		Bar { id: right }
	}
}`,
		"Bar.goi": `Row {
	Rectangle { id: side width: 200 height: 40 }
	Rectangle { width: side.width / 2 height: 40 }
}`,
	})
	b, diag := Load(os.DirFS(dir), "Main.goi")
	if !diag.Empty() {
		t.Fatal(diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources))
	}
	if _, diag := schema.Validate(b.File); !diag.Empty() {
		t.Fatal(diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources))
	}
	var seen []string
	var collect func(syntax.Value)
	collect = func(v syntax.Value) {
		switch x := v.(type) {
		case *syntax.Ref:
			seen = append(seen, x.ID)
		case *syntax.Binary:
			collect(x.Left)
			collect(x.Right)
		case *syntax.Unary:
			collect(x.X)
		case *syntax.Ternary:
			collect(x.Cond)
			collect(x.Then)
			collect(x.Else)
		case *syntax.Map:
			for _, e := range x.Entries {
				collect(e.Value)
			}
		}
	}
	syntax.WalkElements(b.File.Root, func(e *syntax.Element) {
		for _, p := range e.Props() {
			collect(p.Value)
		}
	})
	if len(seen) != 2 || seen[0] != "left.side" || seen[1] != "right.side" {
		t.Fatalf("references = %v, want [left.side right.side]", seen)
	}
}

// TestCallerChildIDsAreNotScoped: an id written in the caller's file belongs to
// the caller, so passing children does not move it into the instance.
func TestCallerChildIDsAreNotScoped(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import Card "./Card.goi"
Window {
	Card {
		id: first
		Text { id: hello text: "x" }
	}
}`,
		"Card.goi": `Column {
	$children
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.ByID("hello"); !ok {
		t.Fatalf("the caller's id was scoped:\n%s", dump(tr))
	}
}
