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
		fmt.Fprintf(&b, "import %q as %s\n", imp.Path, imp.Alias)
	}
	for _, pr := range f.Params {
		fmt.Fprintf(&b, "param %s = %s\n", pr.Name, dumpValue(pr.Default))
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

func dumpBlock(b *strings.Builder, blk *Block, depth int) {
	indent := strings.Repeat("  ", depth)
	if blk.Kind == BlockEach {
		fmt.Fprintf(b, "%seach data.%s as %s\n", indent, blk.Source, blk.Var)
		for i, br := range blk.Branches {
			if i == 1 {
				fmt.Fprintf(b, "%s  else\n", indent)
			}
			dumpMembers(b, br.Members, depth+2)
		}
		return
	}
	for _, br := range blk.Branches {
		switch {
		case br.Cond == nil:
			fmt.Fprintf(b, "%selse\n", indent)
		default:
			fmt.Fprintf(b, "%sif %s\n", indent, dumpValue(br.Cond))
		}
		dumpMembers(b, br.Members, depth+1)
	}
}

func dumpMembers(b *strings.Builder, members []Member, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, m := range members {
		switch v := m.(type) {
		case *Property:
			fmt.Fprintf(b, "%s  %s = %s\n", indent, v.Name, dumpValue(v.Value))
		case *Element:
			dumpElement(b, v, depth)
		case *Block:
			dumpBlock(b, v, depth)
		}
	}
}

func dumpValue(v Value) string {
	switch v := v.(type) {
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
		return v.Callee.ID + "." + v.Callee.Prop + "(" + strings.Join(parts, ", ") + ")"
	case *Binary:
		return "(" + dumpValue(v.Left) + " " + v.Op + " " + dumpValue(v.Right) + ")"
	case *Unary:
		return v.Op + dumpValue(v.X)
	case *Ternary:
		return "(" + dumpValue(v.Cond) + " ? " + dumpValue(v.Then) + " : " + dumpValue(v.Else) + ")"
	case *ParamRef:
		return "$" + v.Name
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
