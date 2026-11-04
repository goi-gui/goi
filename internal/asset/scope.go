package asset

import "github.com/ksckaan1/goi/internal/syntax"

// scopeIDs prefixes the ids a fragment declares with the instance's own id, so
// two instances of one fragment can both carry behaviour:
//
//	import "./Card.goi" as Card
//	Card { id: first }      // its `title` id is now `first.title`
//	Card { id: second }     // and this one's is `second.title`
//
// References inside the fragment follow, so `side.width` still points at the
// instance's own `side`. Caller children are not scoped: an id written in the
// caller's file belongs to the caller.
//
// shortcut: ids in an {#each} row are scoped too, so ui.Descendant needs the
// prefixed name. An instance with no id leaves its ids alone, which is why two
// such instances still collide.
func scopeIDs(body *syntax.Element, prefix string) {
	if prefix == "" {
		return
	}
	inner := map[string]bool{}
	syntax.WalkElements(body, func(e *syntax.Element) {
		if e == body {
			return
		}
		if id, ok := literalID(e); ok {
			inner[id] = true
		}
	})
	if len(inner) == 0 {
		return
	}
	syntax.WalkElements(body, func(e *syntax.Element) {
		if e == body {
			return
		}
		if id, ok := literalID(e); ok {
			setLiteralID(e, prefix+"."+id)
		}
	})
	// Values carry the references, conditions included.
	walkValues(body, func(r *syntax.Ref) {
		if inner[r.ID] {
			r.ID = prefix + "." + r.ID
		}
	})
}

// literalID returns the id an element declares, when it is a literal.
func literalID(e *syntax.Element) (string, bool) {
	p := e.Prop("id")
	if p == nil {
		return "", false
	}
	switch v := p.Value.(type) {
	case *syntax.Ident:
		return v.Val, true
	case *syntax.String:
		return v.Val, true
	}
	return "", false
}

// setLiteralID rewrites an element's id literal in place.
func setLiteralID(e *syntax.Element, id string) {
	switch v := e.Prop("id").Value.(type) {
	case *syntax.Ident:
		v.Val = id
	case *syntax.String:
		v.Val = id
	}
}

// walkValues calls fn for every reference in the body: properties and block
// conditions, however deeply nested in an expression.
func walkValues(body *syntax.Element, fn func(*syntax.Ref)) {
	var val func(syntax.Value)
	val = func(v syntax.Value) {
		switch x := v.(type) {
		case *syntax.Ref:
			fn(x)
		case *syntax.Call:
			for _, a := range x.Args {
				val(a)
			}
		case *syntax.Map:
			for i := range x.Entries {
				val(x.Entries[i].Value)
			}
		case *syntax.Binary:
			val(x.Left)
			val(x.Right)
		case *syntax.Unary:
			val(x.X)
		case *syntax.Ternary:
			val(x.Cond)
			val(x.Then)
			val(x.Else)
		}
	}
	var members func([]syntax.Member)
	members = func(ms []syntax.Member) {
		for _, m := range ms {
			switch x := m.(type) {
			case *syntax.Property:
				val(x.Value)
			case *syntax.Element:
				members(x.Members)
			case *syntax.Block:
				for i := range x.Branches {
					if x.Branches[i].Cond != nil {
						val(x.Branches[i].Cond)
					}
					members(x.Branches[i].Members)
				}
			}
		}
	}
	members(body.Members)
}
