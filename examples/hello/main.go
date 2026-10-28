// Command hello draws nested boxes with radii and borders, the visual check for
// the renderer.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/hello
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
	if _, err := app.LoadWindow("examples/hello/hello.ui", ui.WithHotReload(true)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
