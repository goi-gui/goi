package asset

import (
	"fmt"
	"sort"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
)

// slotLens rewrites `len($slot)` in a fragment body to the number of elements
// the instance passed to that slot: the direct children of the fill, not what
// they expand to. counts holds each fill's elements; a slot the fragment writes
// bare but nobody filled counts zero, which is what makes
// `if len($children) > 0 { … }` a preview-safe test. A name the fragment does
// not write is an error, reported at the value.
func slotLens(body *syntax.Element, counts map[string]int, from string) syntax.Diagnostics {
	declared := declaredSlots(body)
	var diag syntax.Diagnostics
	walkElementValues(body, func(v syntax.Value) syntax.Value {
		return mapValue(v, func(call *syntax.Call) syntax.Value {
			if call.Name != "len" && call.Name != "cap" {
				return call
			}
			if len(call.Args) != 1 {
				return call
			}
			ref, ok := call.Args[0].(*syntax.SlotRef)
			if !ok {
				return call
			}
			if !declared[ref.Name] {
				diag = append(diag, syntax.Diagnostic{
					Pos:     ref.At,
					End:     ref.At + syntax.Pos(len(ref.Name)),
					Src:     from,
					Message: fmt.Sprintf("%s is not a slot of %s", ref.Name, from),
					Hint:    slotHint(from, declared),
				})
				return call
			}
			return &syntax.Number{At: ref.At, Val: float64(counts[ref.Name])}
		})
	})
	return diag
}

// declaredSlots lists the bare `$name` markers in one fragment body. Braces
// make a slot a call's fill, so they are not declarations and are left to the
// instance above.
func declaredSlots(e *syntax.Element) map[string]bool {
	out := map[string]bool{}
	syntax.WalkElements(e, func(el *syntax.Element) {
		if strings.HasPrefix(el.Name, "$") && el.Open == el.NameAt {
			out[el.Name] = true
		}
	})
	return out
}

// walkElementValues applies fn to every value under one member list: property
// values, a block's condition, range and case labels.
func walkElementValues(e *syntax.Element, fn func(syntax.Value) syntax.Value) {
	var walk func([]syntax.Member)
	walk = func(members []syntax.Member) {
		for _, m := range members {
			switch x := m.(type) {
			case *syntax.Property:
				x.Value = fn(x.Value)
			case *syntax.Element:
				walk(x.Members)
			case *syntax.Block:
				if x.Range != nil {
					x.Range = fn(x.Range)
				}
				if x.Subject != nil {
					x.Subject = fn(x.Subject)
				}
				for i := range x.Branches {
					br := &x.Branches[i]
					if br.Cond != nil {
						br.Cond = fn(br.Cond)
					}
					for j := range br.Vals {
						br.Vals[j] = fn(br.Vals[j])
					}
					walk(br.Members)
				}
			}
		}
	}
	walk(e.Members)
}

// slotHint names the slots a fragment does declare, so a typo has a fix.
func slotHint(from string, declared map[string]bool) string {
	if len(declared) == 0 {
		return from + " declares no slot: only a fragment with a bare $name can count one"
	}
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)
	return "its slots: " + strings.Join(names, ", ")
}

// mapValue applies fn to every call in v and keeps the result in place, so a
// rewritten call stays inside the expression that used it.
func mapValue(v syntax.Value, fn func(*syntax.Call) syntax.Value) syntax.Value {
	switch x := v.(type) {
	case *syntax.Call:
		for i := range x.Args {
			x.Args[i] = mapValue(x.Args[i], fn)
		}
		return fn(x)
	case *syntax.Binary:
		x.Left = mapValue(x.Left, fn)
		x.Right = mapValue(x.Right, fn)
		return x
	case *syntax.Unary:
		x.X = mapValue(x.X, fn)
		return x
	case *syntax.Ternary:
		x.Cond = mapValue(x.Cond, fn)
		x.Then = mapValue(x.Then, fn)
		x.Else = mapValue(x.Else, fn)
		return x
	case *syntax.Map:
		for i := range x.Entries {
			x.Entries[i].Value = mapValue(x.Entries[i].Value, fn)
		}
		return x
	case *syntax.List:
		for i := range x.Items {
			x.Items[i] = mapValue(x.Items[i], fn)
		}
		return x
	case *syntax.Modifier:
		for i := range x.Entries {
			x.Entries[i].Value = mapValue(x.Entries[i].Value, fn)
			if x.Entries[i].Cond != nil {
				x.Entries[i].Cond = mapValue(x.Entries[i].Cond, fn)
			}
		}
		return x
	}
	return v
}
