package tree

import (
	"testing"

	"github.com/ksckaan1/goui/internal/syntax"
)

func TestLiteralEqualExpressions(t *testing.T) {
	num := func(v float64) syntax.Value { return &syntax.Number{Val: v} }
	same := []struct{ a, b syntax.Value }{
		{&syntax.Percent{Val: 50}, &syntax.Percent{Val: 50}},
		{&syntax.Ref{ID: "a", Prop: "width"}, &syntax.Ref{ID: "a", Prop: "width"}},
		{&syntax.Binary{Op: "/", Left: num(1), Right: num(2)}, &syntax.Binary{Op: "/", Left: num(1), Right: num(2)}},
		{
			&syntax.Map{Entries: []syntax.MapEntry{{Key: "base", Value: num(1)}, {Key: "md", Value: num(2)}}},
			&syntax.Map{Entries: []syntax.MapEntry{{Key: "md", Value: num(2)}, {Key: "base", Value: num(1)}}},
		},
	}
	for _, c := range same {
		if !LiteralEqual(c.a, c.b) {
			t.Errorf("%v should equal %v", c.a, c.b)
		}
	}
	if LiteralEqual(&syntax.Ref{ID: "a", Prop: "width"}, &syntax.Ref{ID: "a", Prop: "height"}) {
		t.Error("different refs are equal")
	}
	if LiteralEqual(&syntax.Map{}, &syntax.Binary{Op: "+", Left: num(1), Right: num(1)}) {
		t.Error("different kinds are equal")
	}
}
