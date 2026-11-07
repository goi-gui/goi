package ui

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/goi-gui/goi/internal/i18n"
	"github.com/goi-gui/goi/internal/tree"
)

// TestLoadWindowFromFS loads a design from an in-memory container, which is what
// an embed.FS looks like to the framework.
func TestLoadWindowFromFS(t *testing.T) {
	fsys := fstest.MapFS{
		"ui/Main.goi": &fstest.MapFile{Data: []byte(`import Header "./parts/Header.goi"
Window {
	title: "From FS"
	Column {
		Header { heading: "Hello" }
	}
}`)},
		"ui/parts/Header.goi": &fstest.MapFile{Data: []byte(`props { heading string }
Text { text: props.heading role: "headlineSmall" }`)},
	}
	a := newHeadlessApp()
	// LoadWindow needs a platform window, so exercise the loading path the same
	// way it does and check the tree it would install.
	b, err := loadBundle(fsys, "ui/Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Marked: a.markDirty})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Root.Title.Get() != "From FS" {
		t.Fatalf("title = %q", tr.Root.Title.Get())
	}
	var got string
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			got = n.(*Text).Text.Get()
		}
	}
	if got != "Hello" {
		t.Fatalf("fragment text = %q, want the prop value", got)
	}
}

// TestLoadWindowReportsFragmentErrors checks the diagnostic names the fragment
// file and the line inside it.
func TestLoadWindowReportsFragmentErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": &fstest.MapFile{Data: []byte(`import Part "./Part.goi"
Window { Part { } }`)},
		"Part.goi": &fstest.MapFile{Data: []byte("Column {\n  nope: 1\n}\n")},
	}
	_, err := loadBundle(fsys, "Main.goi")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Part.goi:2:3") {
		t.Fatalf("err = %v, want a Part.goi position", err)
	}
}

// TestLoadWindowRequiresAWindowRoot is the one place the rule applies: the file
// a program opens must be a Window. The editor and goicheck stay quiet about it
// because a design split into pages has many roots.
func TestLoadWindowRequiresAWindowRoot(t *testing.T) {
	fsys := fstest.MapFS{
		"Main.goi": &fstest.MapFile{Data: []byte("Column {\n\tid: page\n}\n")},
	}
	_, err := loadBundle(fsys, "Main.goi")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "root element must be Window") {
		t.Fatalf("err = %v, want the Window rule", err)
	}
}

// TestHotReloadRequiresDirectoryContainer checks the failure users will hit:
// os.DirFS hides its path, so it cannot be watched.
func TestHotReloadRequiresDirectoryContainer(t *testing.T) {
	a := newHeadlessApp()
	a.assets = fstest.MapFS{}
	if err := a.enableWatcher(); err == nil {
		t.Fatal("expected a clear error for an unwatchable container")
	} else if !strings.Contains(err.Error(), "ui.DirFS") {
		t.Fatalf("err = %v, want it to name ui.DirFS", err)
	}
}

// TestDirFSImplementsFSWithADir keeps DirFS honest: it is an fs.FS and it exposes
// the directory the watcher needs.
func TestDirFSImplementsFSWithADir(t *testing.T) {
	f := DirFS(t.TempDir())
	if _, ok := any(f).(interface{ Dir() string }); !ok {
		t.Fatal("DirFS must expose its directory")
	}
}

// TestAppLanguageAndCatalogReload covers the Go side of i18n: SetLanguage
// changes what t(...) reads, Language reports it, a missing language falls back,
// and editing a catalog reloads the strings without rebuilding the tree.
func TestAppLanguageAndCatalogReload(t *testing.T) {
	en := `{"hello": "Hello", "only_en": "English only"}`
	tr := `{"hello": "Merhaba"}`
	fsys := fstest.MapFS{
		"Main.goi": &fstest.MapFile{Data: []byte(`Window {
			Column {
				Text { id: hello text: t("hello") }
				Text { id: fallback text: t("only_en") }
				Text { id: lang text: app.language }
			}
		}`)},
		"i18n/en.json": &fstest.MapFile{Data: []byte(en)},
		"i18n/tr.json": &fstest.MapFile{Data: []byte(tr)},
	}
	a := newHeadlessApp()
	b, err := loadBundle(fsys, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := tree.Build(b.File, &tree.Hooks{Marked: a.markDirty})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := i18n.Load(fsys, i18n.Dir)
	if err != nil {
		t.Fatal(err)
	}
	a.assets, a.catalog = fsys, cat
	tree.SetI18n(cat)
	tree.SetSystemLanguage("en")
	tree.ApplyExpressions()
	w := &Window{tree: tree, app: a}
	a.win = w

	if got := MustByID[*Text](w, "hello").Text.Get(); got != "Hello" {
		t.Fatalf("default = %q", got)
	}
	// Before SetLanguage the active language is the desktop's.
	if got := a.Language(); got != "en" {
		t.Fatalf("Language = %q", got)
	}
	if got := w.Language(); got != "en" {
		t.Fatalf("Window.Language = %q", got)
	}

	a.SetLanguage("tr")
	tree.ApplyExpressions()
	if got := MustByID[*Text](w, "hello").Text.Get(); got != "Merhaba" {
		t.Fatalf("tr = %q", got)
	}
	if got := MustByID[*Text](w, "lang").Text.Get(); got != "tr" {
		t.Fatalf("app.language = %q", got)
	}
	// The fallback language defaults to the desktop, so a tr miss reads English.
	if got := MustByID[*Text](w, "fallback").Text.Get(); got != "English only" {
		t.Fatalf("fallback = %q", got)
	}

	// A language the design does not ship falls through to the fallback.
	a.SetFallbackLanguage("en")
	a.SetLanguage("de")
	tree.ApplyExpressions()
	if got := MustByID[*Text](w, "hello").Text.Get(); got != "Hello" {
		t.Fatalf("de = %q, want the en fallback", got)
	}

	// A catalog edit reloads only the strings; the tree instance stays.
	a.SetLanguage("tr")
	fsys["i18n/tr.json"] = &fstest.MapFile{Data: []byte(`{"hello": "Selam"}`)}
	if err := a.reloadCatalog(); err != nil {
		t.Fatal(err)
	}
	tree.ApplyExpressions()
	if got := MustByID[*Text](w, "hello").Text.Get(); got != "Selam" {
		t.Fatalf("after reload = %q", got)
	}
}
