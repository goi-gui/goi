// Command bridge shows the design calling Go handlers through `bridge`, and a
// fragment receiving one of those values and one of those handlers as props.
//
// The .goi files declare the handlers and which event calls each one; this file
// supplies the bodies with ui.SetHandler and appends every event to the log.
// Run with: go run -ldflags=-linkmode=internal ./examples/bridge
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ksckaan1/goi/ui"
)

// events and step live outside Bind: Bind runs again after a hot reload, so the
// log and the counter are app state and Bind only re-registers the handlers.
var events []string

// step is the value Counter.goi shows. The design never owns it: main.goi passes
// bridge.Step into the fragment as a prop.
var step = 3

func main() {
	app, err := ui.NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	win, err := app.LoadWindow(ui.DirFS("examples/bridge"), "main.goi",
		ui.WithHotReload(true), ui.WithSize(560, 720))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	err = win.Bind(func(w *ui.Window) error {
		log := ui.MustByID[*ui.Text](w, "log")
		add := func(format string, args ...any) {
			events = append(events, fmt.Sprintf(format, args...))
			if len(events) > 6 {
				events = events[len(events)-6:]
			}
			log.Text.Set(strings.Join(events, "\n"))
		}
		// The fragment's prop, fed from the bridge: a value and a handler.
		ui.SetData(w, "Step", step)

		handlers := map[string]any{
			"onStep": func(delta int) {
				step += delta
				ui.SetData(w, "Step", step)
				add("onStep: %+d → %d", delta, step)
			},
			"onClick":  func(name string) { add("onClick: %s", name) },
			"onToggle": func(on bool) { add("onToggle: %v", on) },
			"onVolume": func(v float64) { add("onChange: %.2f", v) },
			"onEdit":   func(text string) { add("onEdit: %q", text) },
			"onSubmit": func(text string) { add("onSubmit: %q", text) },
			"onSelect": func(index int) { add("onSelect: %d", index) },
		}
		for name, fn := range handlers {
			if err := ui.SetHandler(w, name, fn); err != nil {
				return err
			}
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
