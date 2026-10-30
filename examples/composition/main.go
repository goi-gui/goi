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
	if _, err := app.LoadWindow(ui.DirFS("examples/composition"), "Main.goi",
		ui.WithHotReload(true), ui.WithSize(1024, 720)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
