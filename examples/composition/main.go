// Command composition shows one design split across several .goi files.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/composition
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
	// The directory is the asset container: imports resolve inside it, and hot
	// reload watches every file the design reads.
	win, err := app.LoadWindow(ui.DirFS("examples/composition"), "Main.goi",
		ui.WithHotReload(true), ui.WithSize(1024, 720))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// An instance's id scopes the ids its fragment declares, so this is exactly
	// StatCard.goi's `value` text inside the Inbox card. The Starred card keeps
	// its own `starred.value` and its 3.
	win.Bind(func(w *ui.Window) error {
		ui.MustByID[*ui.Text](w, "inbox.value").Text.Set("14")
		return nil
	})
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
