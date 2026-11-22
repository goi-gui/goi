package ui

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/goi-gui/goi/internal/i18n"
	"github.com/goi-gui/goi/internal/tree"
)

// liveApp loads a design from an in-memory build and installs it on a headless
// app, the way LoadWindow does once the platform is out of the way.
func liveApp(t *testing.T, fsys fstest.MapFS, entry string) (*App, *Window) {
	t.Helper()
	a := newHeadlessApp()
	b, err := loadBundle(fsys, entry)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Marked: a.markDirty})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := i18n.Load(fsys, i18n.Dir)
	if err != nil {
		t.Fatal(err)
	}
	a.assets, a.path, a.catalog = fsys, entry, cat
	tr.SetI18n(cat)
	tr.SetSystemLanguage("en")
	w := &Window{tree: tr, app: a, title: tr.Root.Title.Get()}
	a.win = w
	return a, w
}

func TestLiveAssetsReloadKeepsGoState(t *testing.T) {
	fsys := fstest.MapFS{
		"ui/Main.goi": &fstest.MapFile{Data: []byte(`import Part "./Part.goi"
Window {
	Column {
		Text { id: label text: "0" }
		Text { id: translated text: t("hello") }
		Part { }
	}
}`)},
		"ui/Part.goi":  &fstest.MapFile{Data: []byte(`Text { id: part text: "before" }`)},
		"i18n/en.json": &fstest.MapFile{Data: []byte(`{"hello": "Hello"}`)},
	}
	a, w := liveApp(t, fsys, "ui/Main.goi")
	MustByID[*Text](w, "label").Text.Set("42")

	a.applyLiveAssets(map[string][]byte{
		"ui/Main.goi":  fsys["ui/Main.goi"].Data,
		"ui/Part.goi":  []byte(`Text { id: part text: "after" }`),
		"i18n/en.json": []byte(`{"hello": "Merhaba"}`),
	})

	if got := MustByID[*Text](w, "part").Text.Get(); got != "after" {
		t.Fatalf("fragment text = %q, want the pushed edit", got)
	}
	if got := MustByID[*Text](w, "label").Text.Get(); got != "42" {
		t.Fatalf("Go-set text was lost: %q", got)
	}
	w.tree.ApplyExpressions()
	if got := MustByID[*Text](w, "translated").Text.Get(); got != "Merhaba" {
		t.Fatalf("translation = %q, want the edited catalog", got)
	}
}

func TestLiveAssetsCatalogOnlyKeepsTheTree(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi":     &fstest.MapFile{Data: []byte(`Window { Text { id: hello text: t("hello") } }`)},
		"i18n/en.json": &fstest.MapFile{Data: []byte(`{"hello": "Hello"}`)},
	}
	a, w := liveApp(t, fsys, "Main.goi")
	before := w.tree

	a.applyLiveAssets(map[string][]byte{
		"Main.goi":     fsys["Main.goi"].Data,
		"i18n/en.json": []byte(`{"hello": "Selam"}`),
	})

	if w.tree != before {
		t.Fatal("a catalog edit rebuilt the tree")
	}
	w.tree.ApplyExpressions()
	if got := MustByID[*Text](w, "hello").Text.Get(); got != "Selam" {
		t.Fatalf("translation = %q, want the edited catalog", got)
	}
}

func TestLiveAssetsHideRemovedFiles(t *testing.T) {
	base := fstest.MapFS{
		"ui/Old.goi":  &fstest.MapFile{Data: []byte(`Text { text: "old" }`)},
		"ui/Keep.goi": &fstest.MapFile{Data: []byte(`Text { text: "keep" }`)},
	}
	live := newLiveAssets(base)
	live.update(map[string][]byte{
		"ui/Old.goi":  base["ui/Old.goi"].Data,
		"ui/Keep.goi": base["ui/Keep.goi"].Data,
	})
	live.update(map[string][]byte{"ui/Keep.goi": base["ui/Keep.goi"].Data})

	if _, err := fs.ReadFile(live, "ui/Old.goi"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a removed file still reads: %v", err)
	}
	entries, err := fs.ReadDir(live, "ui")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "Old.goi" {
			t.Fatal("a removed file is still listed")
		}
	}

	// Adding it back must clear the tombstone.
	live.update(map[string][]byte{
		"ui/Old.goi":  []byte(`Text { text: "again" }`),
		"ui/Keep.goi": base["ui/Keep.goi"].Data,
	})
	if _, err := fs.ReadFile(live, "ui/Old.goi"); err != nil {
		t.Fatalf("a re-added file is still hidden: %v", err)
	}
}

func TestLiveAssetsInitialSnapshotMatchesBuild(t *testing.T) {
	base := fstest.MapFS{
		"Main.goi": &fstest.MapFile{Data: []byte(`Window { Text { text: "same" } }`)},
	}
	live := newLiveAssets(base)
	if changed := live.update(map[string][]byte{
		"Main.goi": base["Main.goi"].Data,
	}); len(changed) != 0 {
		t.Fatalf("connecting reported %v as edits", changed)
	}
}
