// Command gallery renders every Material 3 Expressive component in one window.
// It is the visual reference for the component set and doubles as a manual
// test: click, type, drag and scroll.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/gallery
package main

import (
	"fmt"
	"os"

	"github.com/ksckaan1/goui/ui"
)

func main() {
	app, err := ui.NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	path := "examples/gallery/gallery.ui"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	win, err := app.LoadWindow(path, ui.WithHotReload(true), ui.WithSize(1100, 900))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := win.Bind(func(w *ui.Window) error {
		// Wire the stateful controls so the gallery behaves, not just draws.
		// Wire every stateful control that exists on this page.
		if sw, err := ui.ByID[*ui.Switch](w, "switchOn"); err == nil {
			sw.OnToggle(func(v bool) { fmt.Println("switch:", v) })
		}
		if cb, err := ui.ByID[*ui.Checkbox](w, "check"); err == nil {
			cb.OnToggle(func(v bool) { fmt.Println("checkbox:", v) })
		}
		if sl, err := ui.ByID[*ui.Slider](w, "slider"); err == nil {
			sl.OnChangeValue(func(v float64) { fmt.Printf("slider: %.2f\n", v) })
		}
		if f, err := ui.ByID[*ui.TextField](w, "field"); err == nil {
			f.OnEdit(func(s string) { fmt.Println("field:", s) })
		}
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
