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

	// App state lives outside Bind, which runs again after every reload.
	minW, maxW := 420.0, 1100.0

	err = win.Bind(func(w *ui.Window) error {
		setMin := func(a, b float64) {
			minW, maxW = a, b
			ui.SetData(w, "MinW", minW)
			ui.SetData(w, "MaxW", maxW)
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
