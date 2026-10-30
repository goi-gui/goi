// Theme: the built-in light and dark schemes plus a brand scheme the app
// registers by name, which a .goi file can then select with `theme: brand`.
//
// Run with: go run -ldflags=-linkmode=internal ./examples/theme
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/ksckaan1/goi/ui"
)

var schemes = []string{"light", "dark", "brand"}

// brand is the light scheme with its own teal primary, so the difference is
// visible without inventing a whole palette.
func brand() *ui.Theme {
	t := *ui.LightTheme()
	t.Name = "brand"
	t.Colors.Primary = 0x00695CFF
	t.Colors.OnPrimary = 0xFFFFFFFF
	t.Colors.PrimaryContainer = 0xB2DFDBFF
	t.Colors.OnPrimaryContainer = 0x00251FFF
	t.Colors.SecondaryContainer = 0xCCE8E3FF
	t.Colors.OnSecondaryContainer = 0x00201CFF
	return &t
}

func schemeIndex(name string) int {
	for i, s := range schemes {
		if s == name {
			return i
		}
	}
	return 0
}

func main() {
	app, err := ui.NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	win, err := app.LoadWindowFile("examples/theme/theme.goi",
		ui.WithHotReload(true),
		ui.WithSize(560, 620),
		ui.WithTheme("brand", brand()),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// The scheme name is app state: a reload rebuilds the nodes, not the choice.
	name := "light"
	count := 0

	if err := win.Bind(func(w *ui.Window) error {
		apply := func(n string) {
			name = n
			w.Root().Theme.Set(n)
			for i, id := range []string{"segLight", "segDark", "segBrand"} {
				if seg, err := ui.ByID[*ui.SegmentedButton](w, id); err == nil {
					seg.Selected.Set(i == schemeIndex(n))
				}
			}
			if out, err := ui.ByID[*ui.Text](w, "current"); err == nil {
				out.Text.Set(n)
			}
			if sw, err := ui.ByID[*ui.Switch](w, "darkSwitch"); err == nil {
				sw.Checked.Set(n == "dark")
			}
		}
		for i, id := range []string{"segLight", "segDark", "segBrand"} {
			i := i
			if seg, err := ui.ByID[*ui.SegmentedButton](w, id); err == nil {
				seg.OnClick(func(ui.ClickEvent) { apply(schemes[i]) })
			}
		}
		if sw, err := ui.ByID[*ui.Switch](w, "darkSwitch"); err == nil {
			sw.OnToggle(func(on bool) {
				if on {
					apply("dark")
					return
				}
				apply("light")
			})
		}
		if add, err := ui.ByID[*ui.IconButton](w, "add"); err == nil {
			add.OnClick(func(ui.ClickEvent) {
				count++
				if n, err := ui.ByID[*ui.Text](w, "count"); err == nil {
					n.Text.Set(strconv.Itoa(count))
				}
			})
		}
		apply(name)
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
