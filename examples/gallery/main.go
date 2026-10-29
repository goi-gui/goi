// Command gallery renders every Material 3 Expressive component in one window.
// It is the visual reference for the component set and doubles as a manual
// test: click, type, drag and scroll.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/gallery
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ksckaan1/goui/ui"
)

func main() {
	app, err := ui.NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	path := "examples/gallery/gallery.ui"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	win, err := app.LoadWindow(ui.DirFS(filepath.Dir(path)), filepath.Base(path), ui.WithHotReload(true), ui.WithSize(1100, 900))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// The modal drawer is app state: a reload rebuilds the nodes, not the state.
	drawerOpen := false
	if err := win.Bind(func(w *ui.Window) error {
		// Wire the stateful controls so the gallery behaves, not just draws.
		// Wire every stateful control that exists on this page.
		if sw, err := ui.ByID[*ui.Switch](w, "switchOn"); err == nil {
			sw.OnToggle(func(v bool) { fmt.Println("switch:", v) })
		}
		if cb, err := ui.ByID[*ui.Checkbox](w, "check"); err == nil {
			cb.OnToggle(func(v bool) { fmt.Println("checkbox:", v) })
		}
		if sl, err := ui.ByID[*ui.Slider](w, "slider"); err == nil {
			sl.OnChangeValue(func(v float64) { fmt.Printf("slider: %.2f\n", v) })
		}
		if f, err := ui.ByID[*ui.TextField](w, "field"); err == nil {
			f.OnEdit(func(s string) { fmt.Println("field:", s) })
		}

		// Modal drawer: the menu icon opens it, the scrim and any destination
		// close it. The top bar itself is the hit area, so only a press in the
		// leading icon's slot counts.
		scrim, err := ui.ByID[*ui.Rectangle](w, "scrim")
		if err != nil {
			return err
		}
		frame, err := ui.ByID[*ui.Positioned](w, "drawerFrame")
		if err != nil {
			return err
		}
		setOpen := func(open bool) {
			drawerOpen = open
			scrim.SetVisible(open)
			frame.SetVisible(open)
		}
		setOpen(drawerOpen)
		scrim.OnClick(func(ui.ClickEvent) { setOpen(false) })
		w.OnKey(func(e ui.KeyEvent) {
			if e.Key == ui.KeyEscape {
				setOpen(false)
			}
		})
		if bar, err := ui.ByID[*ui.TopAppBar](w, "appbar"); err == nil {
			bar.OnClick(func(e ui.ClickEvent) {
				if e.X <= 56 {
					setOpen(!drawerOpen)
				}
			})
		}
		for _, id := range []string{"navInbox", "navStarred", "navSent", "navTrash", "navSettings"} {
			if item, err := ui.ByID[*ui.NavigationItem](w, id); err == nil {
				item.OnClick(func(ui.ClickEvent) { setOpen(false) })
			}
		}
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
