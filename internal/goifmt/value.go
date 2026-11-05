package goifmt

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ksckaan1/goi/internal/syntax"
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
