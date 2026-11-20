package tree

import (
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/goi-gui/goi/internal/schema"
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
	// A call on an id drives that element instead of a bridge handler:
	// `onClick: spin.pause()`.
	if call.Callee.ID != "bridge" && call.Callee.ID != "data" {
		if !schema.NodeMethod(call.Callee.Prop) {
			return fmt.Errorf("%s.%s: a handler call starts with bridge., or calls play, pause or stop on an id",
				b.node.Kind(), name)
		}
	}
	switch name {
	case "onClick":
		b.Click.On(func(e ClickEvent) { t.fire(b, name, call, e) })
	case "onClickOutside":
		b.Outside.On(func(e ClickEvent) { t.fire(b, name, call, e) })
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
	case "onPlay", "onPause", "onEnd":
		m, ok := MediaNode(b.node)
		if !ok {
			return fmt.Errorf("%s does not have %s", b.node.Kind(), name)
		}
		// The payload is the position in seconds, which is what a design
		// reading `event` expects from a clock.
		secs := func(d time.Duration) float64 { return d.Seconds() }
		switch name {
		case "onPlay":
			m.Playback.On(func(on bool) {
				if on {
					t.fire(b, name, call, secs(m.Position))
				}
			})
		case "onPause":
			m.Playback.On(func(on bool) {
				if !on {
					t.fire(b, name, call, secs(m.Position))
				}
			})
		case "onEnd":
			m.End.On(func(d time.Duration) { t.fire(b, name, call, secs(d)) })
		}
	case "onCancel", "onOK":
		switch p := b.node.(type) {
		case *TimePicker:
			if name == "onCancel" {
				p.Cancel.On(func(v string) { t.fire(b, name, call, v) })
			} else {
				p.OK.On(func(v string) { t.fire(b, name, call, v) })
			}
		case *ColorPicker:
			if name == "onCancel" {
				p.Cancel.On(func(v string) { t.fire(b, name, call, v) })
			} else {
				p.OK.On(func(v string) { t.fire(b, name, call, v) })
			}
		default:
			return fmt.Errorf("%s does not have %s", b.node.Kind(), name)
		}
	case "onDoubleTap", "onLongPress", "onMouseEnter", "onMouseMove", "onMouseLeave",
		"onPanStart", "onPanUpdate", "onPanEnd":
		g, ok := b.node.(*GestureDetector)
		if !ok {
			return fmt.Errorf("%s does not have %s", b.node.Kind(), name)
		}
		switch name {
		case "onDoubleTap":
			g.DoubleTap.On(func(e ClickEvent) { t.fire(b, name, call, nil) })
		case "onLongPress":
			g.LongPress.On(func(e ClickEvent) { t.fire(b, name, call, nil) })
		case "onPanStart":
			g.PanStart.On(func(e PanEvent) { t.fire(b, name, call, nil) })
		case "onPanUpdate":
			g.PanUpdate.On(func(e PanEvent) { t.fire(b, name, call, nil) })
		case "onPanEnd":
			g.PanEnd.On(func(e PanEvent) { t.fire(b, name, call, nil) })
		case "onMouseEnter":
			g.MouseEnter.On(func(e MouseEvent) { t.fire(b, name, call, nil) })
		case "onMouseMove":
			g.MouseMove.On(func(e MouseEvent) { t.fire(b, name, call, nil) })
		case "onMouseLeave":
			g.MouseLeave.On(func(e MouseEvent) { t.fire(b, name, call, nil) })
		}
	case "onHover":
		c := bridgeComponent(b)
		if c == nil {
			return fmt.Errorf("%s does not have onHover", b.node.Kind())
		}
		c.HoverPoint.On(func(i int) { t.fire(b, name, call, i) })
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
	c := evalCtx{tree: t, sc: b.scope}
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
	if call.Callee.ID != "bridge" && call.Callee.ID != "data" {
		t.callNodeMethodX(call.Callee.ID, call.Callee.Prop, args)
		return
	}
	if err := t.bridge.Call(call.Callee.Prop, args); err != nil {
		t.bridgeErr(err)
	}
}

// callNodeMethodX drives an element's transport from an event, with the call's
// arguments: `spin.pause()`, or `clip.seek(12)` on a player.
func (t *Tree) callNodeMethodX(id, method string, args []any) {
	n, ok := t.ByID(id)
	if !ok {
		t.bridgeErr(fmt.Errorf("goi: no element with id %q", id))
		return
	}
	// `id.scrollTo(200)` and its child forms move a scroll area without a Go
	// handler, which is what a "jump to top" button in a design needs.
	if schema.ScrollMethod(method) {
		if err := callScrollMethod(n.Base(), method, args); err != nil {
			t.bridgeErr(err)
		}
		return
	}
	// `id.setObscureText(false)` reveals a password field, the reveal button
	// a design can wire without a Go handler.
	if method == "setObscureText" {
		if len(args) != 1 {
			t.bridgeErr(fmt.Errorf("goi: %s.setObscureText takes one bool", n.Kind()))
			return
		}
		v, ok := args[0].(bool)
		if !ok {
			t.bridgeErr(fmt.Errorf("goi: %s.setObscureText takes a bool", n.Kind()))
			return
		}
		if !n.Base().SetPropBool("obscureText", v) {
			t.bridgeErr(fmt.Errorf("goi: %s has no obscureText to set", n.Kind()))
		}
		return
	}
	// `id.expand()` toggles a component's expanded state and `id.expand(true)`
	// sets it; the prop is what the design reads back as `id.expanded`.
	if method == "expand" {
		b := n.Base()
		cur, has := b.PropValue("expanded")
		if !has {
			t.bridgeErr(fmt.Errorf("goi: %s has no expanded state to set", n.Kind()))
			return
		}
		on, _ := cur.(bool)
		switch len(args) {
		case 0:
			on = !on
		case 1:
			v, ok := args[0].(bool)
			if !ok {
				t.bridgeErr(fmt.Errorf("goi: %s.expand takes a bool", n.Kind()))
				return
			}
			on = v
		default:
			t.bridgeErr(fmt.Errorf("goi: %s.expand takes no argument or one bool", n.Kind()))
			return
		}
		b.SetPropBool("expanded", on)
		return
	}
	if _, isMedia := MediaNode(n); isMedia {
		if h := n.Base().hooks; h != nil && h.Media != nil && h.Media(n, method, args) {
			return
		}
		t.bridgeErr(fmt.Errorf("goi: %s has no %s", n.Kind(), method))
		return
	}
	tr, ok := n.(interface{ Transport(string) bool })
	if !ok || !tr.Transport(method) {
		t.bridgeErr(fmt.Errorf("goi: %s has no method %s", n.Kind(), method))
	}
}

// callScrollMethod runs one of the scroll shortcuts a design calls by id.
func callScrollMethod(b *NodeBase, method string, args []any) error {
	if b.ScrollAxis() == AxisNone {
		return fmt.Errorf("goi: %s does not scroll", b.Node().Kind())
	}
	number := func(i int) (float64, bool) {
		if i >= len(args) {
			return 0, false
		}
		v, ok := args[i].(float64)
		return v, ok
	}
	duration := func(i int) (time.Duration, bool) {
		if i >= len(args) {
			return 0, false
		}
		d, ok := args[i].(time.Duration)
		return d, ok
	}
	childID := func() (string, bool) {
		if len(args) < 1 {
			return "", false
		}
		v, ok := args[0].(string)
		return v, ok
	}
	switch method {
	case "scrollTo":
		v, ok := number(0)
		if !ok || len(args) != 1 {
			return fmt.Errorf("goi: %s.scrollTo takes one number", b.Node().Kind())
		}
		b.ScrollTo(v)
	case "scrollToSmooth":
		v, ok := number(0)
		d, dok := duration(1)
		if !ok || !dok || len(args) != 2 {
			return fmt.Errorf("goi: %s.scrollToSmooth takes a number and a duration", b.Node().Kind())
		}
		b.ScrollToSmooth(v, d)
	case "scrollToNthChild":
		v, ok := number(0)
		if !ok || len(args) != 1 {
			return fmt.Errorf("goi: %s.scrollToNthChild takes one number", b.Node().Kind())
		}
		if !b.ScrollToNthChild(int(v)) {
			return fmt.Errorf("goi: %s has no visible child %d", b.Node().Kind(), int(v))
		}
	case "scrollToNthChildSmooth":
		v, ok := number(0)
		d, dok := duration(1)
		if !ok || !dok || len(args) != 2 {
			return fmt.Errorf("goi: %s.scrollToNthChildSmooth takes a number and a duration", b.Node().Kind())
		}
		if !b.ScrollToNthChildSmooth(int(v), d) {
			return fmt.Errorf("goi: %s has no visible child %d", b.Node().Kind(), int(v))
		}
	case "scrollToChild":
		id, ok := childID()
		if !ok || len(args) != 1 {
			return fmt.Errorf("goi: %s.scrollToChild takes one id", b.Node().Kind())
		}
		if !b.ScrollToChild(id) {
			return fmt.Errorf("goi: %s has no child %q to scroll to", b.Node().Kind(), id)
		}
	case "scrollToChildSmooth":
		id, ok := childID()
		d, dok := duration(1)
		if !ok || !dok || len(args) != 2 {
			return fmt.Errorf("goi: %s.scrollToChildSmooth takes an id and a duration", b.Node().Kind())
		}
		if !b.ScrollToChildSmooth(id, d) {
			return fmt.Errorf("goi: %s has no child %q to scroll to", b.Node().Kind(), id)
		}
	}
	return nil
}

// bridgeErr reports a handler problem to the app instead of killing the frame.
func (t *Tree) bridgeErr(err error) {
	t.setErr(err)
	if t.report != nil {
		t.report(err)
	}
}
