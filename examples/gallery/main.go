// Command gallery renders every Material 3 Expressive component in one window,
// one category page at a time.
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

// page is one category: the panel it shows and the label used in the app bar and
// the navigation surfaces.
type page struct {
	id    string
	label string
}

var pages = []page{
	{"pageButtons", "Buttons"},
	{"pageInputs", "Inputs"},
	{"pageSurfaces", "Surfaces"},
	{"pageFeedback", "Feedback"},
	{"pageLayout", "Layout"},
}

// navIDs are the containers that select a page: the rail, the bottom bar and the
// drawer. They all mirror the same selection.
var navIDs = []string{"rail", "navbar"}

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
	win, err := app.LoadWindow(ui.DirFS(filepath.Dir(path)), filepath.Base(path),
		ui.WithHotReload(true), ui.WithSize(1100, 900))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Which page is open is app state: a reload rebuilds the nodes, not the state.
	active := 0
	drawerOpen := false

	if err := win.Bind(func(w *ui.Window) error {
		show := func(i int) {
			if i < 0 || i >= len(pages) {
				return
			}
			active = i
			for j, p := range pages {
				if n, err := ui.ByID[*ui.Column](w, p.id); err == nil {
					n.SetVisible(j == i)
				}
			}
			if bar, err := ui.ByID[*ui.TopAppBar](w, "appbar"); err == nil {
				bar.Label.Set(pages[i].label)
			}
			// Every nav surface shows the same selection, whichever one was used.
			for _, id := range navIDs {
				if h, _ := ui.ByID[*ui.NavigationRail](w, id); h != nil {
					selectChildren(h.Base(), i)
				}
				if h, _ := ui.ByID[*ui.NavigationBar](w, id); h != nil {
					selectChildren(h.Base(), i)
				}
			}
			for j, id := range []string{"drawerButtons", "drawerInputs", "drawerSurfaces", "drawerFeedback", "drawerLayout"} {
				if item, err := ui.ByID[*ui.NavigationItem](w, id); err == nil {
					item.M3().Selected.Set(j == i)
				}
			}
		}

		setDrawer := func(open bool) {
			drawerOpen = open
			if scrim, err := ui.ByID[*ui.Rectangle](w, "scrim"); err == nil {
				scrim.SetVisible(open)
			}
			if frame, err := ui.ByID[*ui.Positioned](w, "drawerFrame"); err == nil {
				frame.SetVisible(open)
			}
		}

		// Restore the state a reload must not lose.
		show(active)
		setDrawer(drawerOpen)

		// Navigation: a click on a destination reports its index.
		if rail, err := ui.ByID[*ui.NavigationRail](w, "rail"); err == nil {
			rail.OnSelect(show)
		}
		if bar, err := ui.ByID[*ui.NavigationBar](w, "navbar"); err == nil {
			bar.OnSelect(show)
		}
		for j, id := range []string{"drawerButtons", "drawerInputs", "drawerSurfaces", "drawerFeedback", "drawerLayout"} {
			idx := j
			if item, err := ui.ByID[*ui.NavigationItem](w, id); err == nil {
				item.OnClick(func(ui.ClickEvent) {
					show(idx)
					setDrawer(false)
				})
			}
		}

		// The app-bar icon opens the drawer; the scrim and Escape close it.
		// The hit area is the bar's own leading slot: with a rail beside it the
		// bar does not start at the window's left edge, so the window coordinate
		// must be measured from the bar.
		if bar, err := ui.ByID[*ui.TopAppBar](w, "appbar"); err == nil {
			bar.OnClick(func(e ui.ClickEvent) {
				// Local x inside the bar: subtract every ancestor's offset,
				// the bar's own included.
				local := e.X
				for n := ui.Node(bar); n != nil; n = n.Base().Parent() {
					local -= n.Base().Bounds().X
				}
				if local <= 56 {
					setDrawer(!drawerOpen)
				}
			})
		}
		if scrim, err := ui.ByID[*ui.Rectangle](w, "scrim"); err == nil {
			scrim.OnClick(func(ui.ClickEvent) { setDrawer(false) })
		}
		w.OnKey(func(e ui.KeyEvent) {
			if e.Key == ui.KeyEscape {
				setDrawer(false)
			}
		})

		// Wire the stateful controls so the pages behave, not just draw.
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

// selectChildren marks child index i as the selected destination.
func selectChildren(b *ui.NodeBase, i int) {
	for j, kid := range b.Children() {
		c := kid.(interface{ M3() *ui.Component }).M3()
		if c == nil {
			continue
		}
		c.Selected.Set(j == i)
	}
}
