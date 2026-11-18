package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFontReadsAssetsPath(t *testing.T) {
	font, err := os.ReadFile("../internal/text/fonts/noto-color-emoji.ttf")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets", "fonts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "fonts", "Noto.ttf"), font, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte("assets: assets\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	a := newHeadlessApp()
	if err := a.LoadFont("Emoji", "fonts/Noto.ttf"); err != nil {
		t.Fatal(err)
	}
	if len(a.fonts["Emoji"]) == 0 {
		t.Fatal("the font was not read from the assets directory")
	}
	if err := a.LoadFont("Missing", "fonts/nope.ttf"); err == nil {
		t.Fatal("a missing font path was accepted")
	}
}

func TestUnknownFontFamilyIsRecorded(t *testing.T) {
	_, win := reloadApp(t, `Window {
		fontFamily: "NoSuchFont"
		Text { text: "one" }
		Text { text: "two" fontFamily: "Roboto" }
	}`)
	win.noteMissingFonts(win.tree)
	if !win.missingFonts["NoSuchFont"] {
		t.Fatal("an unknown family was not recorded")
	}
	if win.missingFonts["Roboto"] {
		t.Fatal("built-in Roboto was treated as missing")
	}
}

func TestLoadFontManyKeepsGoing(t *testing.T) {
	font, err := os.ReadFile("../internal/text/fonts/noto-color-emoji.ttf")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets", "fonts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"One.ttf", "Two.ttf"} {
		if err := os.WriteFile(filepath.Join(dir, "assets", "fonts", name), font, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "goi.yaml"), []byte("assets: assets\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	a := newHeadlessApp()
	err = a.LoadFontMany(map[string]string{
		"One":     "fonts/One.ttf",
		"Two":     "fonts/Two.ttf",
		"Missing": "fonts/nope.ttf",
	})
	if err == nil {
		t.Fatal("a missing font path was accepted")
	}
	if len(a.fonts["One"]) == 0 || len(a.fonts["Two"]) == 0 {
		t.Fatalf("valid fonts were not both loaded: %v", a.fonts)
	}
}
