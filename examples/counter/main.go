// Command counter is the framework's smoke test: a window loaded from a .goi
// file, buttons whose handlers update a label through the typed property API.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/counter
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/ksckaan1/goi/ui"
)

// counter lives outside Bind on purpose: Bind runs again after every hot
// reload, so state declared inside it would reset on each save.
type counter struct {
	n int
}

func main() {
	app, err := ui.NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	win, err := app.LoadWindowFile("examples/counter/counter.goi", ui.WithHotReload(true))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	c := &counter{}
	err = win.Bind(func(w *ui.Window) error {
		label := ui.MustByID[*ui.Text](w, "label")
		inc := ui.MustByID[*ui.IconButton](w, "inc")
		dec := ui.MustByID[*ui.IconButton](w, "dec")
		reset := ui.MustByID[*ui.Button](w, "reset")

		// render writes the count and keeps the minus disabled at the floor,
		// so the control never offers an action the state refuses.
		render := func() {
			label.Text.Set(strconv.Itoa(c.n))
			dec.Disabled.Set(c.n <= 0)
			reset.Disabled.Set(c.n == 0)
		}
		render()

		inc.OnClick(func(ui.ClickEvent) {
			c.n++
			render()
		})
		dec.OnClick(func(ui.ClickEvent) {
			if c.n > 0 {
				c.n--
			}
			render()
		})
		reset.OnClick(func(ui.ClickEvent) {
			c.n = 0
			render()
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
