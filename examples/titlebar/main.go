// Command titlebar shows a design-drawn title bar. The .goi file picks the
// Linux or the macOS layout with `app.os`; this file only wires the buttons to
// the Window control methods.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/titlebar
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
	win, err := app.LoadWindowFile("examples/titlebar/titlebar.goi", ui.WithHotReload(true))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	err = win.Bind(func(w *ui.Window) error {
		// Both platform layouts exist in the tree; only one is visible, and
		// binding every id keeps the handler code free of OS checks.
		for _, id := range []string{"lnxMin", "macMin"} {
			ui.MustByID[*ui.IconButton](w, id).OnClick(func(ui.ClickEvent) { w.Minimize() })
		}
		for _, id := range []string{"lnxMax", "macMax"} {
			ui.MustByID[*ui.IconButton](w, id).OnClick(func(ui.ClickEvent) { w.ToggleMaximize() })
		}
		for _, id := range []string{"lnxClose", "macClose"} {
			ui.MustByID[*ui.IconButton](w, id).OnClick(func(ui.ClickEvent) { w.Close() })
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
