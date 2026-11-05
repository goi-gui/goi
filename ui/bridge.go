package ui

import "fmt"

// SetHandler registers the Go function a `bridge { ... }` handler calls. The
// design declares the name and the parameter types; Go supplies the body:
//
//	bridge {
//		onOpen func(id string)
//	}
//
//	Button { label: "Open" onClick: bridge.onOpen("42") }
//
//	ui.SetHandler(w, "onOpen", func(id string) { open(id) })
//
// A call's arguments are evaluated when the event fires, so they read the
// current value of a data variable or a row field. Call it from Bind, which runs
// again after every hot reload, so re-registering there is enough.
//
// The event's own payload is the identifier `event` in the call:
// `onChange: bridge.onVolume(event)` with `func(v: number)`.
func SetHandler(w *Window, name string, fn any) error {
	if w == nil || w.tree == nil {
		return fmt.Errorf("goi: no window to register handler %q on", name)
	}
	return w.tree.SetHandler(name, fn)
}
