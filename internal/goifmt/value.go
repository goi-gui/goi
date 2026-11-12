package goifmt

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
)

// Precedence levels, matching the parser: a child needs parentheses only when
// it binds more loosely than its parent.
const (
	precTernary = iota + 1
	precOr
	precAnd
	precEquality
	precCompare
	precSum
	precTerm
	precUnary
	precPrimary
)

// printValue writes a value back as source.
func printValue(v syntax.Value) string {
	return printPrec(v, precTernary)
}

// printProperty prints a property value, keeping the friendly spellings the
// parser accepts: a fontWeight keyword and a decoration's bare keywords. The
// formatter must not turn `bold` into 700 or add quotes around `underline`,
// because the next read would be a different file. depth is the property's own
// indentation, so a multi-line value block lines up with the file.
func printProperty(name string, v syntax.Value, depth int) string {
	switch m := v.(type) {
	case *syntax.Map:
		return printResponsive(m, depth)
	case *syntax.Modifier:
		return printModifier(m, depth)
	}
	switch name {
	case "fontWeight", "subtitleFontWeight":
		if n, ok := v.(*syntax.Number); ok {
			switch n.Val {
			case 400:
				return "normal"
			case 700:
				return "bold"
			case syntax.FontWeightBolder:
				return "bolder"
			case syntax.FontWeightLighter:
				return "lighter"
			}
		}
	case "decoration":
		if s, ok := v.(*syntax.String); ok && isDecorationText(s.Val) {
			return s.Val
		}
	}
	return printValue(v)
}

// printResponsive writes the old brace map in the modifier spelling, one entry
// per line: `responsive {` reads as a word, and the rows keep a long breakpoint
// list from running past the margin.
func printResponsive(m *syntax.Map, depth int) string {
	if len(m.Entries) == 0 {
		return "responsive {}"
	}
	pad := strings.Repeat("\t", depth+1)
	var b strings.Builder
	b.WriteString("responsive {\n")
	for _, e := range m.Entries {
		b.WriteString(pad + e.Key + ": " + printValue(e.Value) + "\n")
	}
	b.WriteString(strings.Repeat("\t", depth) + "}")
	return b.String()
}

// printModifier writes a responsive or conditional block, one entry per line.
func printModifier(m *syntax.Modifier, depth int) string {
	if len(m.Entries) == 0 {
		return m.Form.String() + " {}"
	}
	pad := strings.Repeat("\t", depth+1)
	var b strings.Builder
	b.WriteString(m.Form.String() + " {\n")
	for _, e := range m.Entries {
		b.WriteString(pad)
		if m.Form == syntax.ModifierConditional {
			b.WriteString(printValue(e.Value) + ": " + printValue(e.Cond))
		} else {
			b.WriteString(e.Key + ": " + printValue(e.Value))
		}
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat("\t", depth) + "}")
	return b.String()
}

// isDecorationText reports whether a string is only the keywords `decoration`
// writes bare, so printing it unquoted round-trips.
func isDecorationText(s string) bool {
	if s == "" {
		return false
	}
	for _, w := range strings.Fields(s) {
		switch w {
		case "none", "underline", "overline", "line-through":
		default:
			return false
		}
	}
	return true
}

func precOf(v syntax.Value) int {
	switch x := v.(type) {
	case *syntax.Ternary:
		return precTernary
	case *syntax.Binary:
		switch x.Op {
		case "||":
			return precOr
		case "&&":
			return precAnd
		case "==", "!=":
			return precEquality
		case "<", "<=", ">", ">=":
			return precCompare
		case "+", "-":
			return precSum
		}
		return precTerm
	case *syntax.Unary:
		return precUnary
	}
	return precPrimary
}

func printPrec(v syntax.Value, parent int) string {
	s := printInner(v)
	if precOf(v) < parent {
		return "(" + s + ")"
	}
	return s
}

func printInner(v syntax.Value) string {
	switch x := v.(type) {
	case nil:
		return ""
	case *syntax.String:
		return quote(x.Val)
	case *syntax.Number:
		return strconv.FormatFloat(x.Val, 'g', -1, 64)
	case *syntax.Bool:
		return strconv.FormatBool(x.Val)
	case *syntax.Color:
		if x.RGBA&0xFF == 0xFF {
			return fmt.Sprintf("#%06x", x.RGBA>>8)
		}
		return fmt.Sprintf("#%08x", x.RGBA)
	case *syntax.Ident:
		return x.Val
	case *syntax.Percent:
		return strconv.FormatFloat(x.Val, 'g', -1, 64) + "%"
	case *syntax.Ref:
		if x.Prop == "" {
			return x.ID
		}
		return x.ID + "." + x.Prop
	case *syntax.Call:
		parts := make([]string, len(x.Args))
		for i, a := range x.Args {
			parts[i] = printPrec(a, precTernary)
		}
		name := x.Name
		if name == "" && x.Callee != nil {
			name = printInner(x.Callee)
		}
		return name + "(" + strings.Join(parts, ", ") + ")"
	case *syntax.Map:
		if len(x.Entries) == 0 {
			return "{}"
		}
		parts := make([]string, len(x.Entries))
		for i, e := range x.Entries {
			parts[i] = e.Key + ": " + printPrec(e.Value, precTernary)
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	case *syntax.Modifier:
		parts := make([]string, len(x.Entries))
		for i, e := range x.Entries {
			if x.Form == syntax.ModifierConditional {
				parts[i] = printValue(e.Value) + ": " + printValue(e.Cond)
			} else {
				parts[i] = e.Key + ": " + printValue(e.Value)
			}
		}
		return x.Form.String() + " { " + strings.Join(parts, ", ") + " }"
	case *syntax.Binary:
		prec := precOf(x)
		// A same-precedence right operand of a left-associative operator needs
		// parentheses; `a - (b - c)` is not `a - b - c`.
		right := printPrec(x.Right, prec+1)
		return printPrec(x.Left, prec) + " " + x.Op + " " + right
	case *syntax.Unary:
		return x.Op + printPrec(x.X, precUnary)
	case *syntax.Ternary:
		return printPrec(x.Cond, precOr) + " ? " + printPrec(x.Then, precTernary) + " : " + printPrec(x.Else, precTernary)
	case *syntax.Rotate:
		// The angle is degrees, so a turn or rad becomes the equivalent degree
		// value. Seven significant digits is as fine as the radians the painter
		// stores can resolve, and it keeps `1.57rad` from becoming
		// `89.95437383553926deg`.
		num := func(v float64) string { return strconv.FormatFloat(v, 'g', 7, 64) }
		if x.Axis == "" && x.Vector == [3]float64{} && x.Angle == 0 {
			return "none"
		}
		parts := make([]string, 0, 5)
		if x.Axis != "" {
			parts = append(parts, x.Axis)
		} else if x.Vector != [3]float64{} {
			for _, v := range x.Vector {
				parts = append(parts, num(v))
			}
		}
		parts = append(parts, num(x.Angle)+"deg")
		return strings.Join(parts, " ")
	case *syntax.Shadow:
		if len(x.Layers) == 0 {
			return "none"
		}
		parts := make([]string, len(x.Layers))
		for i, l := range x.Layers {
			parts[i] = printShadowLayer(l)
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// quote writes a Go double-quoted string literal.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// printShadowLayer writes one shadow the way CSS writes it, omitting the parts
// the design left out.
func printShadowLayer(l syntax.ShadowLayer) string {
	num := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	parts := []string{num(l.X), num(l.Y)}
	if l.Blur != 0 || l.Spread != 0 {
		parts = append(parts, num(l.Blur))
		if l.Spread != 0 {
			parts = append(parts, num(l.Spread))
		}
	}
	if l.Color != nil {
		parts = append(parts, printInner(l.Color))
	}
	if l.Inset {
		parts = append(parts, "inset")
	}
	return strings.Join(parts, " ")
}
