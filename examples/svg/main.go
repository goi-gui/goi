// SVG: one icon file recoloured by the design, by a theme role, and from Go.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/svg
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
	win, err := app.LoadWindowFile("examples/svg/svg.goi", ui.WithHotReload(true))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// The roles the demo buttons pick, as literal colours so the choice is
	// visible without looking the scheme up.
	colors := []uint32{0x6750A4FF, 0x7D5260FF, 0xB3261EFF}
	names := []string{"primary", "tertiary", "error"}

	if err := win.Bind(func(w *ui.Window) error {
		icon := ui.MustByID[*ui.SVG](w, "live")
		label := ui.MustByID[*ui.Text](w, "liveColor")
		return ui.SetHandler(w, "Pick", func(index int) {
			icon.Color.Set(colors[index])
			label.Text.Set(names[index])
		})
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
