package tree

import (
	"testing"
)

// TestModeFollowsTheResolvedScheme is the difference from the old bool: a
// registered dark theme reports ModeDark even though its name is not "dark".
func TestModeFollowsTheResolvedScheme(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
	Text { id: label text: app.mode }
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	label, _ := tr.ByID("label")

	// Before the driver resolves a scheme the name decides, and the default is
	// light.
	tr.ApplyExpressions()
	if got := tr.Mode(); got != ModeLight || label.(*Text).Text.Get() != "light" {
		t.Fatalf("initial mode = %v, text = %q", got, label.(*Text).Text.Get())
	}
	if tr.Dark() {
		t.Fatal("Dark() is true for a light mode")
	}

	tr.SetMode(ModeDark)
	tr.ApplyExpressions()
	if got := label.(*Text).Text.Get(); got != "dark" {
		t.Fatalf("app.mode = %q, want dark", got)
	}
	if !tr.Dark() {
		t.Fatal("Dark() is false for a dark mode")
	}

	// The bool setter is the same cell, so the two spellings cannot disagree.
	tr.SetDark(false)
	if tr.Mode() != ModeLight || tr.Dark() {
		t.Fatal("SetDark(false) did not reach the mode")
	}
}

// TestModeFallsBackToTheThemeName keeps a headless build honest before the
// driver has had a chance to resolve the scheme.
func TestModeFallsBackToTheThemeName(t *testing.T) {
	dark, diag := BuildSource([]byte(`Window {
	theme: dark
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if got := dark.Mode(); got != ModeDark {
		t.Fatalf("theme: dark gave %v", got)
	}
	brand, diag := BuildSource([]byte(`Window {
	theme: brand
	Text { id: label text: app.mode }
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	if got := brand.Mode(); got != ModeLight {
		t.Fatalf("an unresolved custom theme gave %v, want light", got)
	}
}

// TestSystemModeIsWhatTheDesktopSaid covers the design-side value an app follows
// with `theme: app.systemMode`.
func TestSystemModeIsWhatTheDesktopSaid(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
	theme: app.systemMode
	Text { id: label text: app.systemMode }
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	label, _ := tr.ByID("label")
	tr.SetSystemMode(ModeDark)
	tr.ApplyExpressions()
	if got := tr.SystemMode(); got != ModeDark {
		t.Fatalf("system mode = %v", got)
	}
	if got := label.(*Text).Text.Get(); got != "dark" {
		t.Fatalf("app.systemMode = %q", got)
	}
	if got := tr.Root.Theme.Get(); got != "dark" {
		t.Fatalf("theme = %q, want it to follow the desktop", got)
	}
}

// TestModeString is the design-side spelling, which the docs and the language
// server both promise.
func TestModeString(t *testing.T) {
	cases := map[Mode]string{ModeLight: "light", ModeDark: "dark", ModeUnknown: "unknown"}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("%d = %q, want %q", m, got, want)
		}
	}
}

// TestWindowStateAndLanguage covers the design-side reads for the window state
// and the desktop language. A design sees the platform's report, not the last
// request, for the two state members.
func TestWindowStateAndLanguage(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
	minimized: true
	maximized: false
	Text { id: mini text: window.minimized ? "mini" : "shown" }
	Text { id: maxi text: window.maximized ? "max" : "normal" }
	Text { id: lang text: app.systemLanguage }
	Text { id: resize text: window.resizable ? "yes" : "no" }
}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr.SetSystemLanguage("tr")
	tr.ApplyExpressions()

	mini, _ := tr.ByID("mini")
	lang, _ := tr.ByID("lang")
	resize, _ := tr.ByID("resize")
	// No platform report yet, so the request stands in.
	if got := mini.(*Text).Text.Get(); got != "mini" {
		t.Fatalf("window.minimized = %q before the platform reported", got)
	}
	if got := lang.(*Text).Text.Get(); got != "tr" {
		t.Fatalf("app.systemLanguage = %q", got)
	}
	if got := resize.(*Text).Text.Get(); got != "yes" {
		t.Fatalf("window.resizable = %q", got)
	}

	// The platform says the window is restored; `window.minimized` follows it.
	if !tr.Root.SetPlatformState(false, true, false) {
		t.Fatal("the first platform report did not count as a change")
	}
	tr.ApplyExpressions()
	if got := mini.(*Text).Text.Get(); got != "shown" {
		t.Fatalf("window.minimized = %q after the platform restored it", got)
	}
	maxi, _ := tr.ByID("maxi")
	if got := maxi.(*Text).Text.Get(); got != "max" {
		t.Fatalf("window.maximized = %q", got)
	}
}
