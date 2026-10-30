package tree

import (
	"fmt"
	"testing"

	"github.com/ksckaan1/goi/internal/componentdef"
	"github.com/ksckaan1/goi/internal/syntax"
)

// TestEveryComponentPropHasItsOwnCell guards the one() fallback: a name that
// componentdef lists but one() does not map would still be accepted by a file
// and would write the Label cell, so setting one property would change another.
func TestEveryComponentPropHasItsOwnCell(t *testing.T) {
	for _, k := range componentdef.Kinds {
		n := newNode(k.Name)
		if n == nil {
			t.Fatalf("%s has no constructor", k.Name)
		}
		holder, ok := n.(interface{ M3() *Component })
		if !ok || holder.M3() == nil {
			continue // plain nodes have no component cells
		}
		props := holder.M3().props(k.Name)
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
			if !setAny(target, want) {
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
func setAny(p NamedProp, name string) bool {
	values := []syntax.Value{
		&syntax.Number{Val: 3.25},
		&syntax.Bool{Val: true},
		&syntax.String{Val: "zz-" + name},
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
