// Command bridge shows the design calling Go handlers through `bridge`.
//
// The .goi file declares the handlers and which event calls each one; this file
// supplies the bodies with ui.SetHandler and appends every event to the log.
// Run with: go run -ldflags=-linkmode=internal ./examples/bridge
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ksckaan1/goi/ui"
)

// events lives outside Bind: Bind runs again after a hot reload, so the log is
// app state and Bind only re-registers the handlers.
var events []string

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
		handlers := map[string]any{
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
