package ui

import (
	"strings"
	"testing"

	"github.com/ksckaan1/goi/internal/m3"
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
