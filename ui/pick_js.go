//go:build js

package ui

import (
	"strings"
	"syscall/js"
)

// chooseFiles opens the browser's file picker. The page's own picker is an
// `<input type="file">` that has to be clicked inside the user's gesture, which
// is why this never blocks: the app keeps drawing while the dialog is up, and
// fn lands when the choice does.
func (a *App) chooseFiles(fn func([]string), opts []DialogOption) {
	o := newDialogOptions(opts)
	doc := js.Global().Get("document")
	input := doc.Call("createElement", "input")
	input.Set("type", "file")
	input.Set("multiple", true)
	if accept := pickAccept(o.filters); accept != "" {
		input.Set("accept", accept)
	}
	input.Get("style").Set("display", "none")
	doc.Get("body").Call("appendChild", input)

	var change, cancel js.Func
	cleanup := func() {
		input.Call("remove")
		if change.Truthy() {
			change.Release()
		}
		if cancel.Truthy() {
			cancel.Release()
		}
	}
	change = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		files := input.Get("files")
		n := files.Length()
		if n == 0 {
			cleanup()
			return nil
		}
		// Reading a File is asynchronous; the srcs are handed over once every
		// one has its bytes.
		srcs := make([]string, n)
		left := n
		for i := 0; i < n; i++ {
			file := files.Index(i)
			i := i
			read := js.FuncOf(func(_ js.Value, args []js.Value) any {
				left--
				if len(args) > 0 {
					if u8 := args[0]; !u8.IsUndefined() && !u8.IsNull() {
						buf := make([]byte, u8.Get("byteLength").Int())
						js.CopyBytesToGo(buf, u8)
						srcs[i] = a.putPicked(i, file.Get("name").String(), buf)
					}
				}
				if left == 0 {
					picked := make([]string, 0, n)
					for _, s := range srcs {
						if s != "" {
							picked = append(picked, s)
						}
					}
					a.Post(func() { fn(picked) })
					cleanup()
				}
				return nil
			})
			file.Call("arrayBuffer").Call("then", read)
		}
		return nil
	})
	// A dismissed picker fires no event in every browser, so the element is left
	// in place; the next pick replaces it. `cancel` exists only to release the
	// handler on browsers that do report it.
	cancel = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		cleanup()
		return nil
	})
	input.Call("addEventListener", "change", change)
	input.Call("addEventListener", "cancel", cancel)
	input.Call("click")
}

// pickAccept turns the design's filters into the input's accept attribute.
func pickAccept(filters []FileFilter) string {
	var out []string
	for _, f := range filters {
		for _, p := range f.Patterns {
			// `*.goi` becomes `.goi`; anything else is passed through.
			if len(p) > 2 && p[0] == '*' && p[1] == '.' {
				out = append(out, p[1:])
				continue
			}
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}
