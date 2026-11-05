package tree

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/goi-gui/goi/internal/syntax"
)

// Bridge is the Go side of the `bridge { ... }` contract: the handlers a design
// declares by name and Go registers with Window.SetHandler. The registry lives
// on the tree, and Bind runs again after a hot reload, so registering a handler
// there is enough to survive one.
type Bridge struct {
	handlers map[string]reflect.Value
}

// Set registers fn for a handler name. A handler is any Go function; its
// parameters are converted from the design's values when the event fires.
func (b *Bridge) Set(name string, fn any) error {
	if b == nil {
		return fmt.Errorf("goi: no bridge to register %q on", name)
	}
	if name == "" {
		return fmt.Errorf("goi: a handler needs a name")
	}
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return fmt.Errorf("goi: handler %q is %T, not a function", name, fn)
	}
	if b.handlers == nil {
		b.handlers = map[string]reflect.Value{}
	}
	b.handlers[name] = v
	return nil
}

// Call invokes the handler registered for name with the event's arguments.
func (b *Bridge) Call(name string, args []any) error {
	if b == nil {
		return fmt.Errorf("goi: no handler registered for %q", name)
	}
	fn, ok := b.handlers[name]
	if !ok {
		return fmt.Errorf("goi: no handler registered for %q: call Window.SetHandler in Bind", name)
	}
	ft := fn.Type()
	if ft.NumIn() != len(args) {
		return fmt.Errorf("goi: handler %q takes %d argument(s), the design passes %d", name, ft.NumIn(), len(args))
	}
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		v, err := bridgeArg(a, ft.In(i))
		if err != nil {
			return fmt.Errorf("goi: handler %q argument %d: %w", name, i+1, err)
		}
		in[i] = v
	}
	fn.Call(in)
	return nil
}

// bridgeArg converts one design value to the Go parameter type. A number may
// feed an int, a float or a string; the declaration promised a type but Go may
// spell it more precisely.
func bridgeArg(v any, want reflect.Type) (reflect.Value, error) {
	if v == nil {
		return reflect.Zero(want), nil
	}
	rv := reflect.ValueOf(v)
	if rv.Type().AssignableTo(want) {
		return rv, nil
	}
	if want.Kind() == reflect.String {
		switch x := v.(type) {
		case float64:
			return reflect.ValueOf(strconv.FormatFloat(x, 'g', -1, 64)).Convert(want), nil
		case uint32:
			return reflect.ValueOf(fmt.Sprintf("#%06x", x>>8)).Convert(want), nil
		case bool:
			return reflect.ValueOf(strconv.FormatBool(x)).Convert(want), nil
		}
	}
	if rv.Type().ConvertibleTo(want) {
		switch want.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if f, ok := v.(float64); ok {
				return reflect.ValueOf(int64(f)).Convert(want), nil
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if f, ok := v.(float64); ok {
				return reflect.ValueOf(uint64(f)).Convert(want), nil
			}
		}
		return rv.Convert(want), nil
	}
	return reflect.Value{}, fmt.Errorf("%T cannot feed %s", v, want)
}

// SetHandler registers the Go function a bridge handler name calls. Call it
// through ui.SetHandler from Window.Bind, which runs again after every hot
// reload:
//
//	w.Bind(func(w *ui.Window) error {
//		return ui.SetHandler(w, "onOpen", func(id string) { open(id) })
//	})
func (t *Tree) SetHandler(name string, fn any) error {
	if t == nil {
		return fmt.Errorf("goi: no tree to register %q on", name)
	}
	return t.bridge.Set(name, fn)
}

// bindEvent wires one event property to its handler call. The arguments are
// kept as syntax and evaluated when the event fires, never every frame.
func (t *Tree) bindEvent(b *NodeBase, name string, v syntax.Value) error {
	call, ok := v.(*syntax.Call)
	if !ok {
		return fmt.Errorf("%s.%s: expected a handler call", b.node.Kind(), name)
	}
	if call.Callee.ID != "bridge" && call.Callee.ID != "data" {
		return fmt.Errorf("%s.%s: a handler call starts with bridge., not %s", b.node.Kind(), name, call.Callee.ID)
	}
	switch name {
	case "onClick":
		b.Click.On(func(e ClickEvent) { t.fire(b, name, call, e) })
	case "onToggle":
		c := bridgeComponent(b)
		if c == nil {
			return fmt.Errorf("%s does not have onToggle", b.node.Kind())
		}
		c.Toggle.On(func(on bool) { t.fire(b, name, call, on) })
	case "onChange":
		c := bridgeComponent(b)
		if c == nil {
			return fmt.Errorf("%s does not have onChange", b.node.Kind())
		}
		c.Change.On(func(v float64) { t.fire(b, name, call, v) })
	case "onEdit":
		c := bridgeComponent(b)
		if c == nil {
			return fmt.Errorf("%s does not have onEdit", b.node.Kind())
		}
		c.Edit.On(func(s string) { t.fire(b, name, call, s) })
	case "onSubmit":
		c := bridgeComponent(b)
		if c == nil {
			return fmt.Errorf("%s does not have onSubmit", b.node.Kind())
		}
		c.Submit.On(func(s string) { t.fire(b, name, call, s) })
	case "onSelect":
		c := bridgeComponent(b)
		if c == nil {
			return fmt.Errorf("%s does not have onSelect", b.node.Kind())
		}
		c.Select.On(func(i int) { t.fire(b, name, call, i) })
	default:
		return fmt.Errorf("%s.%s: unknown event", b.node.Kind(), name)
	}
	return nil
}

// bridgeComponent returns the component state behind a node, or nil.
func bridgeComponent(b *NodeBase) *Component {
	h, ok := b.node.(interface{ M3() *Component })
	if !ok {
		return nil
	}
	return h.M3()
}

// fire evaluates the call's arguments at event time and invokes the handler.
// The identifier `event` is the event's own payload.
func (t *Tree) fire(b *NodeBase, name string, call *syntax.Call, payload any) {
	c := evalCtx{tree: t, sc: b.scope, seen: map[refKey]bool{}}
	args := make([]any, len(call.Args))
	for i, a := range call.Args {
		if id, ok := a.(*syntax.Ident); ok && id.Val == "event" {
			args[i] = payload
			continue
		}
		v, ok := c.eval(a, 0)
		if !ok {
			t.bridgeErr(fmt.Errorf("goi: %s.%s: cannot evaluate argument %d of %s", b.node.Kind(), name, i+1, call.Callee.Prop))
			return
		}
		args[i] = v
	}
	if err := t.bridge.Call(call.Callee.Prop, args); err != nil {
		t.bridgeErr(err)
	}
}

// bridgeErr reports a handler problem to the app instead of killing the frame.
func (t *Tree) bridgeErr(err error) {
	t.setErr(err)
	if t.report != nil {
		t.report(err)
	}
}
