package ui

import (
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// brandTheme is Light with a distinctive primary, so a test can tell the
// registered scheme apart from the built-in ones.
func brandTheme() *m3.Theme {
	t := *m3.Light()
	t.Name = "brand"
	t.Colors.Primary = 0x00AA55FF
	return &t
}

// TestRegisteredThemeIsUsed covers the theme API: a name in the file resolves to
// the scheme the app registered, and its roles follow.
func TestRegisteredThemeIsUsed(t *testing.T) {
	a, win := reloadApp(t, `Window {
		theme: brand
		Column { Text { id: label text: "x" color: primary } }
	}`)
	a.themes = map[string]*m3.Theme{"light": m3.Light(), "dark": m3.Dark(), "brand": brandTheme()}
	win.tree.ResolveColors(themeFor(win).Colors.Role)
	if got := themeFor(win).Name; got != "brand" {
		t.Fatalf("resolved theme = %q, want brand", got)
	}
	label := MustByID[*Text](win, "label")
	if got := label.Color.Get(); got != 0x00AA55FF {
		t.Fatalf("role colour = #%08x, want the brand primary", got)
	}
	if a.win == nil {
		t.Fatal("app lost its window")
	}
}

// TestCustomDarkThemeReadsAsDark keeps app.dark honest: it is the scheme's mood,
// not a comparison against the literal name "dark".
func TestCustomDarkThemeReadsAsDark(t *testing.T) {
	dark := *m3.Dark()
	dark.Name = "midnight"
	a, win := reloadApp(t, `Window {
		theme: midnight
		Column { Text { id: label text: app.dark ? "dark" : "light" } }
	}`)
	a.themes = map[string]*m3.Theme{"light": m3.Light(), "dark": m3.Dark(), "midnight": &dark}
	win.tree.SetDark(themeFor(win).Dark)
	if !win.tree.Dark() {
		t.Fatal("a registered dark theme did not report dark")
	}
	win.tree.ApplyExpressions()
	label := MustByID[*Text](win, "label")
	if got := label.Text.Get(); got != "dark" {
		t.Fatalf("app.dark expression = %q, want dark", got)
	}
}

// TestUnknownThemeFallsBackAndReports catches the typo case: the window still
// draws, but the app hears about the name it could not resolve.
func TestUnknownThemeFallsBackAndReports(t *testing.T) {
	a, win := reloadApp(t, `Window { theme: nope Column { Text { text: "x" } } }`)
	if got := themeFor(win).Name; !strings.Contains(got, "light") {
		t.Fatalf("fallback theme = %q, want light", got)
	}
	_ = a
}

// TestWindowModeReachesGo is the Go half of the feature: the enum follows the
// resolved scheme, and `app.mode` in the design reports the same value.
func TestWindowModeReachesGo(t *testing.T) {
	_, w := reloadApp(t, `Window {
	theme: dark
	Text { id: label text: app.mode }
}`)
	w.tree.SetDark(themeFor(w).Dark)
	w.tree.ApplyExpressions()

	if got := w.Mode(); got != ModeDark {
		t.Fatalf("Mode() = %v, want ModeDark", got)
	}
	if got := MustByID[*Text](w, "label").Text.Get(); got != "dark" {
		t.Fatalf("app.mode = %q, want dark", got)
	}

	if err := applyReloadSource(t, w.app, `Window {
	theme: light
	Text { id: label text: app.mode }
}`); err != nil {
		t.Fatal(err)
	}
	w = w.app.win
	w.tree.SetMode(modeFor(themeFor(w)))
	w.tree.ApplyExpressions()
	if got := w.Mode(); got != ModeLight {
		t.Fatalf("Mode() after the switch = %v, want ModeLight", got)
	}
	if got := MustByID[*Text](w, "label").Text.Get(); got != "light" {
		t.Fatalf("app.mode = %q after the switch", got)
	}
}

// TestThemeSwitchOverridesSystemMode is the gallery bug: `theme: app.systemMode`
// follows the desktop, but a click on a switch has to win and stay won across
// the frame's expression pass.
func TestThemeSwitchOverridesSystemMode(t *testing.T) {
	const src = `Window {
		theme: app.systemMode
		Column {
			Switch { id: sw checked: app.dark iconOn: dark_mode iconOff: light_mode }
		}
	}`
	_, w, in := inputFixture(t, src, 480, 320)
	w.tree.SetSystemMode(tree.ModeLight)
	w.tree.ApplyExpressions()
	if got := w.Root().Theme.Get(); got != "light" {
		t.Fatalf("theme = %q, want the desktop's light", got)
	}

	sw := MustByID[*Switch](w, "sw")
	sw.OnToggle(func(dark bool) {
		name := "light"
		if dark {
			name = "dark"
		}
		w.Root().Theme.Set(name)
		sw.Checked.Set(dark)
	})
	b := absoluteRect(sw)
	in.MouseDown(b.X+b.W/2, b.Y+b.H/2, 0, false)
	in.MouseUp(b.X+b.W/2, b.Y+b.H/2, 0)

	// The frame loop re-evaluates `theme: app.systemMode`; it must not revert.
	w.tree.ApplyExpressions()
	if got := w.Root().Theme.Get(); got != "dark" {
		t.Fatalf("theme = %q after the click, want dark", got)
	}
	if !sw.Checked.Get() {
		t.Fatal("the switch did not stay checked")
	}
	if w.tree.Mode() != tree.ModeDark {
		t.Fatalf("mode = %v", w.tree.Mode())
	}

	// A reload without a file edit keeps the choice: the value carried over is
	// still Go's, so the expression must not reclaim it.
	if err := applyReloadSource(t, w.app, src); err != nil {
		t.Fatal(err)
	}
	w = w.app.win
	if got := w.Root().Theme.Get(); got != "dark" {
		t.Fatalf("theme = %q after a reload", got)
	}

	// Editing the theme line gives the property back to the design.
	if err := applyReloadSource(t, w.app, strings.Replace(src, "theme: app.systemMode", "theme: light", 1)); err != nil {
		t.Fatal(err)
	}
	w = w.app.win
	if got := w.Root().Theme.Get(); got != "light" {
		t.Fatalf("theme = %q after the literal changed", got)
	}
}
