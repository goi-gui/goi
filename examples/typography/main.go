// Type: the M3 type scale, explicit sizes and weights, and text wrapping.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/typography
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
	if _, err := app.LoadWindowFile("examples/typography/typography.ui", ui.WithHotReload(true), ui.WithSize(900, 780)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
