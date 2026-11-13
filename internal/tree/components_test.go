package tree

import (
	"fmt"
	"testing"

	"github.com/goi-gui/goi/internal/componentdef"
	"github.com/goi-gui/goi/internal/syntax"
)

// TestEveryComponentPropHasItsOwnCell guards the one() fallback: a name that
// componentdef lists but one() does not map would still be accepted by a file
// and would write the Label cell, so setting one property would change another.
// It reads the node's own table, because a component with cells of its own
// (Table) supplies some properties outside the shared Component.
func TestEveryComponentPropHasItsOwnCell(t *testing.T) {
	for _, k := range componentdef.Kinds {
		n := newNode(k.Name)
		if n == nil {
			t.Fatalf("%s has no constructor", k.Name)
		}
		if _, ok := n.(interface{ M3() *Component }); !ok {
			continue // plain nodes have no component cells
		}
		holder, ok := n.(interface{ namedProps() []NamedProp })
		if !ok {
			t.Fatalf("%s has no property table", k.Name)
		}
		props := holder.namedProps()
		byName := make(map[string]NamedProp, len(props))
		for _, p := range props {
			byName[p.Name] = p
		}
		for _, want := range componentdef.Props(k.Name) {
			target, ok := byName[want]
			if !ok {
				t.Errorf("%s: no property %q", k.Name, want)
				continue
			}
			before := snapshot(props)
			if !setAny(target, want, k.Name) {
				t.Errorf("%s: %q rejected every literal", k.Name, want)
				continue
			}
			after := snapshot(props)
			for name, was := range before {
				if name == want || paired(want, name) {
					continue
				}
				if after[name] != was {
					t.Errorf("%s: setting %q also changed %q", k.Name, want, name)
				}
			}
		}
	}
}

// paired reports the one deliberate alias: gap and spacing are the same cell,
// so a file can spell the gap either way.
func paired(a, b string) bool {
	return (a == "gap" && b == "spacing") || (a == "spacing" && b == "gap")
}

// setAny writes a distinctive value of whichever type the property takes.
func setAny(p NamedProp, name, kind string) bool {
	values := []syntax.Value{
		&syntax.Number{Val: 3.25},
		&syntax.Bool{Val: true},
		&syntax.String{Val: "zz-" + name},
		&syntax.Color{RGBA: 0x112233FF},
		// A list property takes one of these, depending on what its items are.
		&syntax.List{Items: []syntax.Value{&syntax.String{Val: "zz-" + name}}},
		&syntax.List{Items: []syntax.Value{&syntax.Number{Val: 3.25}}},
		&syntax.List{Items: []syntax.Value{&syntax.Color{RGBA: 0x112233FF}}},
	}
	// The narrowed list first: a progress bar's `variant` is `normal` or `wavy`,
	// neither of which is in the union the other components share.
	for _, v := range componentdef.EnumValuesFor(kind, name) {
		values = append(values, &syntax.Ident{Val: v})
	}
	for _, v := range componentdef.EnumValues(name) {
		values = append(values, &syntax.Ident{Val: v})
	}
	for _, v := range values {
		if p.SetLiteral(v) {
			return true
		}
	}
	return false
}

func snapshot(props []NamedProp) map[string]string {
	out := make(map[string]string, len(props))
	for _, p := range props {
		out[p.Name] = fmt.Sprint(p.Get())
	}
	return out
}

// TestIfBlockTogglesItsArm keeps the block a pure visibility switch: every arm
// is built, exactly one shows, and hiding a branch takes the whole subtree away
// from layout.
func TestIfBlockTogglesItsArm(t *testing.T) {
	src := `Window {
	Column {
		if app.dark {
			Text { id: darkArm text: "dark" }
		} else if window.width > 900 {
			Text { id: wideArm text: "wide" }
		} else {
			Text { id: smallArm text: "small" }
		}
	}
}`
	tr, diag := BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	dark, _ := tr.ByID("darkArm")
	wide, _ := tr.ByID("wideArm")
	small, _ := tr.ByID("smallArm")
	if dark == nil || wide == nil || small == nil {
		t.Fatal("an arm was not built")
	}

	// Nothing set: light, narrow.
	if dark.Base().Visible() || wide.Base().Visible() || !small.Base().Visible() {
		t.Fatalf("initial arms: dark=%v wide=%v small=%v",
			dark.Base().Visible(), wide.Base().Visible(), small.Base().Visible())
	}
	tr.SetViewport(1000, 800)
	tr.ApplyExpressions()
	if !wide.Base().Visible() || small.Base().Visible() {
		t.Fatal("a wide window did not pick the else-if arm")
	}
	// Only the first matching arm shows, even when both conditions hold.
	tr.SetDark(true)
	tr.ApplyExpressions()
	if !dark.Base().Visible() || wide.Base().Visible() {
		t.Fatal("the first matching arm did not win")
	}
}

// A switch shows the first arm holding a value equal to its subject, and falls
// back to default when none does.
func TestSwitchPicksItsArm(t *testing.T) {
	src := `bridge { job string }
Window {
	Column {
		switch bridge.job {
		case "swe", "eee", "fe":
			Text { id: eng text: "engineer" }
		case "teacher":
			Text { id: teach text: "teacher" }
		default:
			Text { id: other text: "other" }
		}
	}
}`
	tr, diag := BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	eng, _ := tr.ByID("eng")
	teach, _ := tr.ByID("teach")
	other, _ := tr.ByID("other")
	if eng == nil || teach == nil || other == nil {
		t.Fatal("an arm was not built")
	}
	shown := func() string {
		tr.ApplyExpressions()
		switch {
		case eng.Base().Visible():
			return "eng"
		case teach.Base().Visible():
			return "teach"
		case other.Base().Visible():
			return "other"
		}
		return ""
	}

	if got := shown(); got != "other" {
		t.Fatalf("empty subject showed %q, want the default arm", got)
	}
	for job, want := range map[string]string{
		"eee":     "eng",
		"fe":      "eng",
		"teacher": "teach",
		"doctor":  "other",
	} {
		tr.SetData("job", job)
		if got := shown(); got != want {
			t.Fatalf("%q showed %q, want %q", job, got, want)
		}
	}
}

// TestShadowResolvesItsRole keeps a themed shadow honest: `shadow: 2 2 primary`
// follows the scheme, and a Go-set shadow reaches the painter.
func TestShadowResolvesItsRole(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
	Column {
		Rectangle { id: box width: 10 height: 10 shadow: 2 2 primary }
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	box, _ := tr.ByID("box")
	if got := box.Base().Shadow(); len(got) != 1 || got[0].Role != "primary" {
		t.Fatalf("shadow = %+v", got)
	}
	// The role is not a colour yet.
	if box.Base().Shadow()[0].Color != 0 {
		t.Fatal("a role was resolved before the theme ran")
	}
	if !tr.ResolveColors(func(name string) (uint32, bool) {
		if name == "primary" {
			return 0x6750A4FF, true
		}
		return 0, false
	}) {
		t.Fatal("ResolveColors reported no change")
	}
	if got := box.Base().Shadow()[0].Color; got != 0x6750A4FF {
		t.Fatalf("resolved colour = %08x", got)
	}
	if _, dirty := box.Base().Dirty(); !dirty {
		t.Fatal("a resolved shadow did not mark the node")
	}

	box.Base().SetShadow(ShadowLayer{X: 0, Y: 0, Blur: 4, Color: 0xFF0000FF})
	if got := box.Base().Shadow(); len(got) != 1 || got[0].Blur != 4 {
		t.Fatalf("after SetShadow = %+v", got)
	}
	box.Base().ClearShadow()
	if len(box.Base().Shadow()) != 0 {
		t.Fatal("ClearShadow left a layer")
	}
}

// A nested block shows only inside a shown outer arm, and a hidden subtree is
// skipped by layout, so it takes no space.
func TestNestedIfBlockNeedsItsOuterArm(t *testing.T) {
	src := `Window {
	Column {
		if app.dark {
			if window.width > 900 {
				Text { id: inner text: "x" height: 40 }
			}
		}
	}
}`
	tr, diag := BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	inner, _ := tr.ByID("inner")
	tr.SetViewport(1200, 800)
	tr.ApplyExpressions()
	if inner.Base().Visible() {
		t.Fatal("the inner arm showed while the outer one was hidden")
	}
	tr.SetDark(true)
	tr.ApplyExpressions()
	if !inner.Base().Visible() {
		t.Fatal("the inner arm did not show inside a shown outer arm")
	}
}

// A block leaves the node tree flat: the arms are ordinary children of the
// enclosing element, so a Row still lays them out as row items.
func TestIfBlockAddsNoContainerNode(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
	Row {
		if app.dark {
			Text { text: "a" }
		} else {
			Text { text: "b" }
		}
	}
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	row := tr.Root.Children()[0]
	if got := len(row.Base().Children()); got != 2 {
		t.Fatalf("%d children, want the two arm elements", got)
	}
	for _, c := range row.Base().Children() {
		if c.Kind() != "Text" {
			t.Fatalf("child is %s, want Text", c.Kind())
		}
	}
}
