package schema

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ksckaan1/goui/internal/syntax"
)

var idPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*$`)

// Result is what validation learned that instantiation cannot recompute cheaply.
type Result struct {
	// IDs maps each element to its resolved id, for the elements that have one.
	IDs map[*syntax.Element]string
}

// Validate checks the AST against the schema, reporting every problem rather
// than stopping at the first. The AST itself is never modified.
func Validate(f *syntax.File) (*Result, syntax.Diagnostics) {
	if f == nil || f.Root == nil {
		return nil, syntax.Diagnostics{{Message: "the file has no root element"}}
	}
	v := &validator{ids: map[*syntax.Element]string{}, seen: map[string]syntax.Pos{}}
	if _, ok := Lookup(f.Root.Name); !ok {
		v.elementUnknown(f.Root)
	} else if f.Root.Name != "Window" {
		v.add(f.Root.NameAt, f.Root.Close, "the root element must be Window", syntax.Suggest(f.Root.Name, RootNames()))
	}
	v.walk(f.Root)
	if !v.diag.Empty() {
		return nil, v.diag
	}
	return &Result{IDs: v.ids}, nil
}

type validator struct {
	ids  map[*syntax.Element]string
	seen map[string]syntax.Pos
	diag syntax.Diagnostics
}

func (v *validator) add(at, end syntax.Pos, msg, hint string) {
	v.diag = append(v.diag, syntax.Diagnostic{Pos: at, End: end, Message: msg, Hint: hint})
}

func (v *validator) elementUnknown(e *syntax.Element) {
	v.add(e.NameAt, e.NameAt+2, fmt.Sprintf("unknown element %q", e.Name), syntax.Suggest(e.Name, ElementNames()))
}

// walk validates one element and recurses into its children.
func (v *validator) walk(e *syntax.Element) {
	def, ok := Lookup(e.Name)
	if !ok {
		v.elementUnknown(e)
		return // children of an unknown element cannot be meaningfully checked
	}

	seen := map[string]syntax.Pos{}
	for _, p := range e.Props() {
		at := p.NameAt
		if _, dup := seen[p.Name]; dup {
			v.add(at, at+syntax.Pos(len(p.Name)), fmt.Sprintf("duplicate property %q", p.Name), "")
			continue
		}
		seen[p.Name] = at

		pd := def.Prop(p.Name)
		if pd == nil {
			v.add(at, at+syntax.Pos(len(p.Name)),
				fmt.Sprintf("unknown property %q on %s", p.Name, e.Name), syntax.Suggest(p.Name, propNames(def)))
			continue
		}
		v.checkValue(e, p, pd)
	}

	if len(e.Children()) > 0 && !def.Children {
		first := e.Children()[0]
		v.add(first.NameAt, first.NameAt+2, fmt.Sprintf("%s cannot contain child elements", e.Name), "")
	}

	for _, c := range e.Children() {
		v.walk(c)
	}
}

func propNames(def *Element) []string {
	out := make([]string, len(def.Attrs))
	for i, a := range def.Attrs {
		out[i] = a.Name
	}
	return out
}

// checkValue verifies the literal kind and records the element's id.
func (v *validator) checkValue(e *syntax.Element, p *syntax.Property, pd *Prop) {
	val := p.Value
	at, end := val.Pos(), val.Pos()+1
	fail := func(want string) {
		v.add(at, end, fmt.Sprintf("%s.%s expects %s, got %s", e.Name, pd.Name, want, kindName(val)), "")
	}

	switch pd.Type {
	case TypeID:
		txt, ok := literalText(val)
		if !ok {
			fail("an identifier")
			return
		}
		if !idPattern.MatchString(txt) {
			v.add(at, end, fmt.Sprintf("invalid id %q: must match [a-z][A-Za-z0-9_]*", txt), "")
			return
		}
		if _, dup := v.seen[txt]; dup {
			v.add(at, end, fmt.Sprintf("duplicate id %q", txt), "")
			return
		}
		v.seen[txt] = at
		v.ids[e] = txt

	case TypeString:
		switch val.(type) {
		case *syntax.String, *syntax.Ident:
			// An Ident is allowed so `text: hello` reads naturally; the schema
			// documents string properties as accepting bare words.
		default:
			fail("a string")
		}

	case TypeNumber:
		if _, ok := val.(*syntax.Number); !ok {
			fail("a number")
		}

	case TypeBool:
		if _, ok := val.(*syntax.Bool); !ok {
			fail("true or false")
		}

	case TypeColor:
		switch c := val.(type) {
		case *syntax.Color:
			// Already normalized by the lexer.
		case *syntax.String:
			if _, ok := parseColor(c.Val); !ok {
				v.add(at, end, fmt.Sprintf("%s.%s is not a valid color: %q", e.Name, pd.Name, c.Val), "")
			}
		default:
			fail("a color like #3b82f6")
		}

	case TypeEnum:
		name, ok := literalText(val)
		if !ok {
			fail("an identifier (" + strings.Join(pd.Enum, ", ") + ")")
			return
		}
		for _, allowed := range pd.Enum {
			if name == allowed {
				return
			}
		}
		v.add(at, end, fmt.Sprintf("%s.%s has no value %q", e.Name, pd.Name, name), syntax.Suggest(name, pd.Enum))
	}
}

// literalText returns the text of a String or Ident value.
func literalText(val syntax.Value) (string, bool) {
	switch v := val.(type) {
	case *syntax.String:
		return v.Val, true
	case *syntax.Ident:
		return v.Val, true
	}
	return "", false
}

func kindName(val syntax.Value) string {
	switch val.Kind() {
	case syntax.StringKind:
		return "a string"
	case syntax.NumberKind:
		return "a number"
	case syntax.BoolKind:
		return "a bool"
	case syntax.ColorKind:
		return "a color"
	case syntax.IdentKind:
		return "an identifier"
	}
	return "an unknown value"
}

// parseColor accepts #rgb, #rrggbb and #rrggbbaa.
func parseColor(s string) (uint32, bool) {
	if !strings.HasPrefix(s, "#") {
		return 0, false
	}
	hexs := s[1:]
	if len(hexs) != 3 && len(hexs) != 6 && len(hexs) != 8 {
		return 0, false
	}
	n, err := strconv.ParseUint(hexs, 16, 32)
	if err != nil {
		return 0, false
	}
	switch len(hexs) {
	case 3:
		r := (n >> 8) & 0xF
		g := (n >> 4) & 0xF
		b := n & 0xF
		return uint32(r*17)<<24 | uint32(g*17)<<16 | uint32(b*17)<<8 | 0xFF, true
	case 6:
		return uint32(n)<<8 | 0xFF, true
	default:
		return uint32(n), true
	}
}
