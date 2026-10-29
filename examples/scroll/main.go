// Command scroll shows the `scroll` property and the Go offset API.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/scroll
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
	win, err := app.LoadWindowFile("examples/scroll/scroll.ui", ui.WithHotReload(true))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	err = win.Bind(func(w *ui.Window) error {
		page := ui.MustByID[*ui.Column](w, "page")

		ui.MustByID[*ui.Button](w, "top").OnClick(func(ui.ClickEvent) {
			page.SetScrollOffset(0)
		})
		ui.MustByID[*ui.Button](w, "end").OnClick(func(ui.ClickEvent) {
			// Any value past the end reads back as the clamped end, so the
			// handler needs no knowledge of the content height.
			page.SetScrollOffset(1e9)
			fmt.Println("offset:", page.ScrollOffset())
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
