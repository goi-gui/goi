package syntax

import (
	"fmt"
	"strconv"
	"strings"
)

// Dump renders an AST as indented text. Tests use it as a golden format; it is
// also handy when debugging a surprising parse.
func Dump(f *File) string {
	if f == nil || f.Root == nil {
		return "<nil>\n"
	}
	var b strings.Builder
	for _, imp := range f.Imports {
		fmt.Fprintf(&b, "import %s %q\n", imp.Alias, imp.Path)
	}
	for _, pr := range f.Props {
		fmt.Fprintf(&b, "props %s %s\n", pr.Name, pr.Type.String())
	}
	for _, v := range f.Vars {
		fmt.Fprintf(&b, "bridge %s %s\n", v.Name, v.Type.String())
	}
	for _, sh := range f.Shapes {
		fmt.Fprintf(&b, "type %s\n", sh.Name)
		for _, fl := range sh.Fields {
			fmt.Fprintf(&b, "  %s %s\n", fl.Name, fl.Type.String())
		}
	}
	dumpElement(&b, f.Root, 0)
	return b.String()
}

func dumpElement(b *strings.Builder, e *Element, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(b, "%s%s\n", indent, e.Name)
	for _, m := range e.Members {
		switch v := m.(type) {
		case *Property:
			fmt.Fprintf(b, "%s  %s = %s\n", indent, v.Name, dumpValue(v.Value))
		case *Element:
			dumpElement(b, v, depth+1)
		case *Block:
			dumpBlock(b, v, depth+1)
		}
	}
}

// dumpBlock writes a control block in the Go spelling it is written in.
func dumpBlock(b *strings.Builder, blk *Block, depth int) {
	indent := strings.Repeat("  ", depth)
	if blk.Kind == BlockFor {
		names := blk.KeyVar
		if blk.ValVar != "" {
			names += ", " + blk.ValVar
		}
		if names != "" {
			names += " := "
		}
		fmt.Fprintf(b, "%sfor %srange %s\n", indent, names, dumpValue(blk.Range))
		dumpMembers(b, blk.Branches[0].Members, depth+1)
		return
	}
	if blk.Kind == BlockSwitch {
		fmt.Fprintf(b, "%sswitch %s\n", indent, dumpValue(blk.Subject))
		for _, br := range blk.Branches {
			if len(br.Vals) == 0 {
				fmt.Fprintf(b, "%sdefault\n", indent)
			} else {
				vals := make([]string, len(br.Vals))
				for i, v := range br.Vals {
					vals[i] = dumpValue(v)
				}
				fmt.Fprintf(b, "%scase %s\n", indent, strings.Join(vals, ", "))
			}
			dumpMembers(b, br.Members, depth+1)
		}
		return
	}
	for i, br := range blk.Branches {
		switch {
		case i == 0:
			fmt.Fprintf(b, "%sif %s\n", indent, dumpValue(br.Cond))
		case br.Cond != nil:
			fmt.Fprintf(b, "%selse if %s\n", indent, dumpValue(br.Cond))
		default:
			fmt.Fprintf(b, "%selse\n", indent)
		}
		dumpMembers(b, br.Members, depth+1)
	}
}

// dumpMembers writes a block arm's members one level deeper.
func dumpMembers(b *strings.Builder, members []Member, depth int) {
	fake := &Element{Members: members}
	dumpElement(b, fake, depth)
}

func dumpValue(v Value) string {
	switch v := v.(type) {
	case *Shadow:
		if len(v.Layers) == 0 {
			return "none"
		}
		parts := make([]string, len(v.Layers))
		for i, l := range v.Layers {
			parts[i] = dumpShadowLayer(l)
		}
		return strings.Join(parts, ", ")
	case *String:
		return strconv.Quote(v.Val)
	case *Number:
		return strconv.FormatFloat(v.Val, 'g', -1, 64)
	case *Bool:
		return strconv.FormatBool(v.Val)
	case *Color:
		return fmt.Sprintf("#%08x", v.RGBA)
	case *Ident:
		return v.Val
	case *Percent:
		return strconv.FormatFloat(v.Val, 'g', -1, 64) + "%"
	case *Ref:
		return v.ID + "." + v.Prop
	case *Call:
		parts := make([]string, len(v.Args))
		for i, a := range v.Args {
			parts[i] = dumpValue(a)
		}
		name := v.Name
		if name == "" {
			name = v.Callee.ID + "." + v.Callee.Prop
		}
		return name + "(" + strings.Join(parts, ", ") + ")"
	case *Binary:
		return "(" + dumpValue(v.Left) + " " + v.Op + " " + dumpValue(v.Right) + ")"
	case *Unary:
		return v.Op + dumpValue(v.X)
	case *Ternary:
		return "(" + dumpValue(v.Cond) + " ? " + dumpValue(v.Then) + " : " + dumpValue(v.Else) + ")"
	case *Map:
		parts := make([]string, len(v.Entries))
		for i, e := range v.Entries {
			parts[i] = e.Key + ": " + dumpValue(e.Value)
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	return "?"
}

// dumpShadowLayer writes one shadow in its CSS spelling.
func dumpShadowLayer(l ShadowLayer) string {
	num := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	parts := []string{num(l.X), num(l.Y)}
	if l.Blur != 0 || l.Spread != 0 {
		parts = append(parts, num(l.Blur))
		if l.Spread != 0 {
			parts = append(parts, num(l.Spread))
		}
	}
	if l.Color != nil {
		parts = append(parts, dumpValue(l.Color))
	}
	if l.Inset {
		parts = append(parts, "inset")
	}
	return strings.Join(parts, " ")
}
