// Command gallery renders every Material 3 Expressive component in one window,
// one category page at a time.
// It is the visual reference for the component set and doubles as a manual
// test: click, type, drag and scroll.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/gallery
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	{"pageMotion", "Motion"},
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
	fps := flag.Bool("fps", false, "draw the frame rate in the top-right corner")
	flag.Parse()
	path := "examples/gallery/gallery.ui"
	if arg := flag.Arg(0); arg != "" {
		path = arg
	}
	win, err := app.LoadWindow(ui.DirFS(filepath.Dir(path)), filepath.Base(path),
		ui.WithHotReload(true), ui.WithSize(1100, 900))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *fps {
		app.ShowFPS(true)
	}
	// Which page is open is app state: a reload rebuilds the nodes, not the state.
	active := 0
	drawerOpen := false
	dark := false

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
			for j, id := range []string{"drawerButtons", "drawerInputs", "drawerSurfaces", "drawerFeedback", "drawerLayout", "drawerMotion"} {
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

		// Both theme controls drive this one function: the rail switch and the
		// one on the Layout page stay in step because neither owns the state.
		applyTheme := func(d bool) {
			dark = d
			theme := "light"
			if d {
				theme = "dark"
			}
			w.Root().Theme.Set(theme)
			for _, id := range []string{"darkSwitch", "railTheme"} {
				if sw, err := ui.ByID[*ui.Switch](w, id); err == nil {
					sw.Checked.Set(d)
				}
			}
		}

		// Restore the state a reload must not lose.
		show(active)
		setDrawer(drawerOpen)
		applyTheme(dark)

		// Navigation: a click on a destination reports its index.
		if rail, err := ui.ByID[*ui.NavigationRail](w, "rail"); err == nil {
			rail.OnSelect(show)
		}
		if bar, err := ui.ByID[*ui.NavigationBar](w, "navbar"); err == nil {
			bar.OnSelect(show)
		}
		for j, id := range []string{"drawerButtons", "drawerInputs", "drawerSurfaces", "drawerFeedback", "drawerLayout", "drawerMotion"} {
			idx := j
			if item, err := ui.ByID[*ui.NavigationItem](w, id); err == nil {
				item.OnClick(func(ui.ClickEvent) {
					show(idx)
					setDrawer(false)
				})
			}
		}

		// The app-bar leading icon opens the drawer. Its hit area is measured from
		// the bar's own edge, because a rail beside it moves the bar's left edge
		// off the window's.
		if bar, err := ui.ByID[*ui.TopAppBar](w, "appbar"); err == nil {
			bar.OnClick(func(e ui.ClickEvent) {
				local := e.X
				for n := ui.Node(bar); n != nil; n = n.Base().Parent() {
					local -= n.Base().Bounds().X
				}
				if local <= 56 && bar.Icon.Get() != "" {
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

		// The Layout page's repeat demo: one hidden template, three copies. A
		// reload rebuilds the tree, so this runs again from Bind and the copies
		// come back with the page.
		if list, err := ui.ByID[*ui.Column](w, "repeatList"); err == nil {
			for i, name := range []string{"First copy", "Second copy", "Third copy"} {
				item, err := ui.Instantiate[*ui.ListItem](w, "repeatTemplate", list)
				if err != nil {
					continue
				}
				item.Label.Set(name)
				item.Icon.Set([]string{"person", "star", "photo"}[i])
			}
		}

		// The Motion page: the three dots travel the same distance and differ
		// only in easing. Stops are kept so a second Play cancels the first run
		// instead of leaving two tweens pulling the same Left property.
		var easingStops []func()
		playEasing := func() {
			for _, stop := range easingStops {
				stop()
			}
			easingStops = easingStops[:0]
			for _, d := range []struct {
				id   string
				ease ui.Easing
			}{
				{"dotLinear", ui.Linear},
				{"dotStandard", ui.Standard},
				{"dotEmphasized", ui.Emphasized},
			} {
				dot, err := ui.ByID[*ui.Positioned](w, d.id)
				if err != nil {
					continue
				}
				dot.Left.Set(0)
				easingStops = append(easingStops, app.Animate(&dot.Left, 264, 700*time.Millisecond, d.ease))
			}
		}
		if b, err := ui.ByID[*ui.Button](w, "playEasing"); err == nil {
			b.OnClick(func(ui.ClickEvent) { playEasing() })
		}

		// One value drives the slider and the progress ring, so the Motion page
		// shows that any numeric Prop can be animated.
		valueUp := false
		if b, err := ui.ByID[*ui.Button](w, "playValue"); err == nil {
			b.OnClick(func(ui.ClickEvent) {
				valueUp = !valueUp
				to := 0.9
				if !valueUp {
					to = 0.15
				}
				if sl, err := ui.ByID[*ui.Slider](w, "motionSlider"); err == nil {
					app.Animate(&sl.Value, to, 700*time.Millisecond, ui.Standard)
				}
				if pg, err := ui.ByID[*ui.CircularProgress](w, "motionProgress"); err == nil {
					app.Animate(&pg.Value, to, 900*time.Millisecond, ui.Emphasized)
				}
			})
		}

		// The rail switch and the one on the Layout page are both entry points to
		// the theme.
		for _, id := range []string{"railTheme", "darkSwitch"} {
			if sw, err := ui.ByID[*ui.Switch](w, id); err == nil {
				sw.OnToggle(applyTheme)
			}
		}

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

// selectChildren marks destination i as selected. Only NavigationItems count as
// destinations, so the rail's theme switch below them does not shift the index.
func selectChildren(b *ui.NodeBase, i int) {
	n := 0
	for _, kid := range b.Children() {
		if kid.Kind() != "NavigationItem" {
			continue
		}
		c, ok := kid.(interface{ M3() *ui.Component })
		if !ok {
			continue
		}
		if m := c.M3(); m != nil {
			m.Selected.Set(n == i)
		}
		n++
	}
}
