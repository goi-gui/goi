// Command format shows the formatf builtin and a bridge starting at zero.
//
// The design starts with the values its bridge declares; the button below writes
// a new count from Go, and every interpolated text follows it.
// Run with: go run -ldflags=-linkmode=internal ./examples/format
package main

import (
	"fmt"
	"os"

	"github.com/ksckaan1/goi/ui"
)

// count lives outside Bind: Bind runs again after a hot reload, so the state
// must survive it.
var count = 3

func main() {
	app, err := ui.NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	win, err := app.LoadWindow(ui.DirFS("examples/format"), "main.goi",
		ui.WithHotReload(true), ui.WithSize(420, 320))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	err = win.Bind(func(w *ui.Window) error {
		// The bridge starts at zero; the design already draws.
		ui.SetData(w, "count", count)
		ui.MustByID[*ui.Button](w, "more").OnClick(func(ui.ClickEvent) {
			count++
			ui.SetData(w, "count", count)
			ui.SetData(w, "discount", count >= 5)
			ui.SetData(w, "label", "Items")
		})
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
