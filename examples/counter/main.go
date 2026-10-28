// Command counter is the framework's smoke test: a window loaded from a .ui
// file, a button whose handler updates a label through the typed property API.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/counter
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/ksckaan1/goui/ui"
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
	win, err := app.LoadWindow("examples/counter/counter.ui", ui.WithHotReload(true))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	c := &counter{}
	err = win.Bind(func(w *ui.Window) error {
		label := ui.MustByID[*ui.Text](w, "label")
		inc := ui.MustByID[*ui.Button](w, "inc")
		reset := ui.MustByID[*ui.Button](w, "reset")
		name := ui.MustByID[*ui.TextField](w, "name")

		inc.OnClick(func(ui.ClickEvent) {
			c.n++
			label.Text.Set(strconv.Itoa(c.n))
		})
		reset.OnClick(func(ui.ClickEvent) {
			c.n = 0
			label.Text.Set("0")
		})
		name.OnEdit(func(s string) {
			fmt.Println("name:", s)
		})
		name.OnSubmit(func(s string) {
			fmt.Println("submitted:", s)
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
