// Command window shows the Window element's own properties: the size limits,
// `disableResize` and `fullscreen`, and how a set from Go reaches the platform.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/window
package main

import (
	"fmt"
	"os"

	"github.com/ksckaan1/goi/ui"
)

func main() {
	app, err := ui.NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	win, err := app.LoadWindowFile("examples/window/window.goi", ui.WithHotReload(true))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	err = win.Bind(func(w *ui.Window) error {
		// The setters write the same cells the design's literals fill, so the
		// native window and the layout always agree. The tree keeps the values,
		// so a hot reload carries them over: no app state needed.
		setMin := func(a, b float64) {
			w.Root().SetMinWidth(a)
			w.Root().SetMaxWidth(b)
			render(w)
		}
		render(w)

		if sw, err := ui.ByID[*ui.Switch](w, "lockResize"); err == nil {
			sw.OnToggle(func(on bool) {
				w.Root().DisableResize.Set(on)
				render(w)
			})
		}
		if sw, err := ui.ByID[*ui.Switch](w, "fs"); err == nil {
			sw.OnToggle(func(on bool) {
				w.Root().Fullscreen.Set(on)
				render(w)
			})
		}
		if err := ui.SetHandler(w, "SetMin", func() { setMin(600, 900) }); err != nil {
			return err
		}
		if err := ui.SetHandler(w, "ClearLimits", func() { setMin(-1, -1) }); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func render(w *ui.Window) {
	ui.MustByID[*ui.Switch](w, "lockResize").Checked.Set(w.Root().DisableResize.Get())
	ui.MustByID[*ui.Switch](w, "fs").Checked.Set(w.Root().Fullscreen.Get())
	ui.MustByID[*ui.Text](w, "status").Text.Set(status(w))
}

func status(w *ui.Window) string {
	resize := "resize on"
	if w.Root().DisableResize.Get() {
		resize = "resize off"
	}
	fs := "windowed"
	if w.Root().Fullscreen.Get() {
		fs = "fullscreen"
	}
	return resize + " · " + fs
}
