package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goi-gui/goi/internal/sysfont"
	"github.com/goi-gui/goi/internal/text"
)

func TestLoadSystemFonts(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "internal", "text", "fonts", "roboto-mono.ttf"))
	if err != nil {
		t.Skipf("the test font is unavailable: %v", err)
	}
	fontPath := filepath.Join(t.TempDir(), "test.ttf")
	if err := os.WriteFile(fontPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	old := systemFonts
	t.Cleanup(func() { systemFonts = old })
	var asked []string
	systemFonts = func(family string) []sysfont.File {
		asked = append(asked, family)
		switch family {
		case "Test Sans":
			return []sysfont.File{{Path: fontPath, Family: family, Weight: 700}}
		case "Test Var":
			return []sysfont.File{{Path: fontPath, Family: family, Weight: 400, Variable: true}}
		}
		return nil
	}

	a, win := reloadApp(t, `Window {
		Text { text: "one" fontFamily: "Test Sans" }
		Text { text: "two" fontFamily: "Test Var" }
	}`)
	a.useSystemFonts = true
	a.loadSystemFonts(win.tree)

	faces := a.fontFaces["Test Sans"]
	if len(faces) != 1 || faces[0].weight != text.WeightBold || len(faces[0].data) == 0 {
		t.Fatalf("static system family = %+v", faces)
	}
	variables := a.fontVariables["Test Var"]
	if len(variables) != 1 || len(variables[0].data) == 0 {
		t.Fatalf("variable system family = %+v", variables)
	}
	if len(asked) != 2 {
		t.Fatalf("lookups = %v, want one per family", asked)
	}

	// A family the app registered itself wins over the installed one.
	if a.fonts == nil {
		a.fonts = map[string][]byte{}
	}
	a.fonts["Test Sans"] = []byte("app font")
	delete(a.fontFaces, "Test Sans")
	asked = nil
	a.loadSystemFonts(win.tree)
	for _, name := range asked {
		if name == "Test Sans" {
			t.Fatal("the system lookup ran for a family the app loaded")
		}
	}
}

func TestLoadSystemFontsIsOffByDefault(t *testing.T) {
	old := systemFonts
	t.Cleanup(func() { systemFonts = old })
	called := false
	systemFonts = func(string) []sysfont.File {
		called = true
		return nil
	}
	a, win := reloadApp(t, `Window { Text { text: "hi" fontFamily: "Test Sans" } }`)
	a.loadSystemFonts(win.tree)
	if called {
		t.Fatal("a headless app scanned the system fonts")
	}
}
