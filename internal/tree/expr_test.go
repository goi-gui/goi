package tree

import (
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/goi-gui/goi/internal/i18n"
)

// TestOSExpression covers the platform global: a design can hide a title bar
// it did not write for this OS.
func TestOSExpression(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Rectangle { id: box visible: app.os == "linux" width: 40 height: 20 }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()
	box := tr.ids["box"].Base()
	if got, want := box.Visible(), runtime.GOOS == "linux"; got != want {
		t.Fatalf("visible = %v on %s", got, runtime.GOOS)
	}
}

// TestThemeExpressions covers the point of the expression layer: a view can ask
// about the scheme and pick a value, and it must follow a later switch.
func TestThemeExpressions(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		theme: light
		Column {
			Text { id: label text: app.dark ? "Dark" : "Light" color: app.dark ? onSurface : onPrimary }
			Rectangle { id: box visible: app.dark width: 40 height: 20 }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()
	label := tr.ids["label"].Base()
	if got, _ := label.PropValue("text"); got != "Light" {
		t.Fatalf("light text = %v", got)
	}
	if got := label.colorRoleFor("color"); got != "onPrimary" {
		t.Fatalf("light role = %q", got)
	}
	if box := tr.ids["box"].Base(); box.Visible() {
		t.Fatal("box is visible in light mode")
	}

	tr.Root.Theme.Set("dark")
	tr.ApplyExpressions()
	if got, _ := label.PropValue("text"); got != "Dark" {
		t.Fatalf("dark text = %v", got)
	}
	if got := label.colorRoleFor("color"); got != "onSurface" {
		t.Fatalf("dark role = %q", got)
	}
	if box := tr.ids["box"].Base(); !box.Visible() {
		t.Fatal("box is hidden in dark mode")
	}

	// Re-evaluating an unchanged expression must not keep the tree dirty, or
	// the frame loop would never go idle.
	tr.ids["label"].Base().ClearDirty()
	tr.ids["box"].Base().ClearDirty()
	tr.ApplyExpressions()
	if _, dirty := tr.ids["box"].Base().Dirty(); dirty {
		t.Fatal("unchanged expression marked the node dirty")
	}
}

// TestWindowExpressions checks the size globals and that a comparison result can
// feed a numeric property.
func TestWindowExpressions(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Rectangle { id: wide visible: window.width >= 640 width: window.width / 2 height: 20 }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()
	wide := tr.ids["wide"].Base()
	if !wide.Visible() {
		t.Fatal("wide layout should show the rectangle")
	}
	if wide.Width() != 400 {
		t.Fatalf("width = %v, want 400", wide.Width())
	}
	tr.SetViewport(400, 600)
	tr.ApplyExpressions()
	if wide.Visible() {
		t.Fatal("narrow layout should hide the rectangle")
	}
}

// TestStringConcat keeps `text: a.text + "!"` working.
func TestStringConcat(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Text { id: a text: "Hi" }
			Text { id: b text: a.text + "!" }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.ApplyExpressions()
	if got, _ := tr.ids["b"].Base().PropValue("text"); got != "Hi!" {
		t.Fatalf("text = %v", got)
	}
}

// TestGoSetTakesThePropertyFromItsExpression is the theme-switch case: a design
// that starts on `app.systemMode` must still let Go pick a scheme, and the
// frame loop must not put the expression's value back.
func TestGoSetTakesThePropertyFromItsExpression(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		theme: app.systemMode
		Text { id: label text: app.mode }
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetSystemMode(ModeLight)
	tr.ApplyExpressions()
	label := tr.ids["label"].(*Text)
	if got := tr.Root.Theme.Get(); got != "light" {
		t.Fatalf("theme = %q, want the desktop's light", got)
	}
	if got := label.Text.Get(); got != "light" {
		t.Fatalf("text = %q", got)
	}

	// Go takes both properties over, the way the gallery's switch does.
	tr.Root.Theme.Set("dark")
	tr.SetMode(ModeDark)
	tr.ApplyExpressions()
	label.Text.Set("manual")
	tr.SetSystemMode(ModeLight)
	tr.ApplyExpressions()

	if got := tr.Root.Theme.Get(); got != "dark" {
		t.Fatalf("theme = %q, the expression overwrote a Go value", got)
	}
	if got := label.Text.Get(); got != "manual" {
		t.Fatalf("text = %q, the expression overwrote a Go value", got)
	}

	// The expression still owns a property Go never wrote.
	tr, diag = BuildSource([]byte(`Window {
		theme: app.systemMode
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetSystemMode(ModeDark)
	tr.ApplyExpressions()
	if got := tr.Root.Theme.Get(); got != "dark" {
		t.Fatalf("theme = %q, want it to follow the desktop", got)
	}
	tr.SetSystemMode(ModeLight)
	tr.ApplyExpressions()
	if got := tr.Root.Theme.Get(); got != "light" {
		t.Fatalf("theme = %q after the desktop switched", got)
	}
}

// TestTranslateExpression covers t(...) end to end: a plain key, a nested key,
// format arguments, the key itself for a miss, and a language switch that is
// read again on the next expression pass.
func TestTranslateExpression(t *testing.T) {
	cat, err := i18n.Load(fstest.MapFS{
		"i18n/en.json": &fstest.MapFile{Data: []byte(`{
			"welcome": "Welcome",
			"settings": {"theme": "Theme"},
			"hello": "Hello %s",
			"count": "%d items"
		}`)},
		"i18n/tr.json": &fstest.MapFile{Data: []byte(`{
			"welcome": "Hoşgeldiniz",
			"settings": {"theme": "Tema"},
			"hello": "Merhaba %s"
		}`)},
	}, "i18n")
	if err != nil {
		t.Fatal(err)
	}
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Text { id: plain text: t("welcome") }
			Text { id: nested text: t("settings.theme") }
			Text { id: filled text: t("hello", "Kaan") }
			Text { id: miss text: t("nope.key") }
			Text { id: lang text: app.language }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetI18n(cat)
	tr.SetSystemLanguage("tr")
	tr.ApplyExpressions()
	text := func(id string) string { return tr.ids[id].(*Text).Text.Get() }
	if got := text("plain"); got != "Hoşgeldiniz" {
		t.Fatalf("plain = %q", got)
	}
	if got := text("nested"); got != "Tema" {
		t.Fatalf("nested = %q", got)
	}
	if got := text("filled"); got != "Merhaba Kaan" {
		t.Fatalf("filled = %q", got)
	}
	if got := text("miss"); got != "nope.key" {
		t.Fatalf("miss = %q, want the key", got)
	}
	// No SetLanguage yet, so the design follows the desktop.
	if got := text("lang"); got != "tr" {
		t.Fatalf("app.language = %q, want the system language", got)
	}

	// A language the design does not ship uses the fallback entry by entry.
	tr.SetFallbackLanguage("en")
	tr.SetLanguage("de")
	tr.ApplyExpressions()
	if got := text("plain"); got != "Welcome" {
		t.Fatalf("fallback = %q", got)
	}
	if got := text("filled"); got != "Hello Kaan" {
		t.Fatalf("fallback format = %q", got)
	}
	// A key the fallback does not have is still the key, not blank.
	if got := text("miss"); got != "nope.key" {
		t.Fatalf("unpaired key = %q, want the key", got)
	}

	tr.SetLanguage("en")
	tr.ApplyExpressions()
	if got := text("lang"); got != "en" {
		t.Fatalf("explicit app.language = %q", got)
	}
}

// TestTranslateIntegerVerb keeps %d working: a design number is a float64 and
// has to reach fmt as an integer, the same rule formatf follows.
func TestTranslateIntegerVerb(t *testing.T) {
	cat, err := i18n.Load(fstest.MapFS{
		"i18n/en.json": &fstest.MapFile{Data: []byte(`{"count": "%d items"}`)},
	}, "i18n")
	if err != nil {
		t.Fatal(err)
	}
	tr, diag := BuildSource([]byte(`Window {
		Column { Text { id: label text: t("count", 1 + 2) } }
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetI18n(cat)
	tr.SetLanguage("en")
	tr.ApplyExpressions()
	if got := tr.ids["label"].(*Text).Text.Get(); got != "3 items" {
		t.Fatalf("text = %q", got)
	}
}
