package i18n

import (
	"testing"
	"testing/fstest"
)

const (
	enJSON = `{
		"welcome_message": "Welcome",
		"settings": { "theme": "Theme", "language": "Language" },
		"welcome_with_name": "Welcome %s",
		"count": "There are %d items"
	}`
	trJSON = `{
		"welcome_message": "Hoşgeldiniz",
		"settings": { "theme": "Tema", "language": "Dil" },
		"welcome_with_name": "Hoşgeldin %s"
	}`
)

func catalog(t *testing.T, files map[string]string) *Bundle {
	t.Helper()
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	b, err := Load(m, "i18n")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestLoadFlattensNestedKeys covers the file shape: a nested object is read
// with dotted keys, and every language in the folder is detected.
func TestLoadFlattensNestedKeys(t *testing.T) {
	b := catalog(t, map[string]string{"i18n/en.json": enJSON, "i18n/tr.json": trJSON})
	if got := b.Languages(); len(got) != 2 || got[0] != "en" || got[1] != "tr" {
		t.Fatalf("languages = %v", got)
	}
	if !b.Has("tr") || b.Has("de") {
		t.Fatalf("Has disagrees with the folder: %v", b.Languages())
	}
	if got := b.Translate("tr", "en", "settings.theme"); got != "Tema" {
		t.Fatalf("nested key = %q", got)
	}
	if got := b.Translate("tr", "en", "welcome_with_name", "Kaan"); got != "Hoşgeldin Kaan" {
		t.Fatalf("formatted = %q", got)
	}
	// The integer verb wants an int, which the design side passes as int64.
	if got := b.Translate("en", "en", "count", int64(3)); got != "There are 3 items" {
		t.Fatalf("integer verb = %q", got)
	}
}

// TestFallbackChain pins the lookup order: language, then fallback, then the
// key itself.
func TestFallbackChain(t *testing.T) {
	b := catalog(t, map[string]string{"i18n/en.json": enJSON, "i18n/tr.json": trJSON})
	if got := b.Translate("tr", "en", "count", int64(2)); got != "There are 2 items" {
		t.Fatalf("missing tr entry = %q, want the en one", got)
	}
	if got := b.Translate("de", "en", "welcome_message"); got != "Welcome" {
		t.Fatalf("missing language = %q, want the fallback", got)
	}
	if got := b.Translate("de", "en", "nope.key"); got != "nope.key" {
		t.Fatalf("missing key = %q, want the key itself", got)
	}
	// No arguments leaves a stray percent as written.
	if got := b.Translate("en", "en", "welcome_with_name"); got != "Welcome %s" {
		t.Fatalf("unfilled pattern = %q", got)
	}
}

// TestLanguageRegionMatch lets the desktop say en-US while the design ships
// en.json, which is the file meant.
func TestLanguageRegionMatch(t *testing.T) {
	b := catalog(t, map[string]string{"i18n/en.json": enJSON})
	if got := b.Translate("en-US", "", "welcome_message"); got != "Welcome" {
		t.Fatalf("en-US = %q", got)
	}
}

// TestOptionalFolder covers a design with no i18n: no folder, no bundle, and
// the caller can still translate (to the key).
func TestOptionalFolder(t *testing.T) {
	b, err := Load(fstest.MapFS{"Main.goi": &fstest.MapFile{Data: []byte("Window {}")}}, "i18n")
	if err != nil || b != nil {
		t.Fatalf("missing folder = %v, %v", b, err)
	}
	if got := b.Translate("en", "en", "key"); got != "key" {
		t.Fatalf("nil bundle = %q", got)
	}
}

// TestBrokenCatalogReportsTheFile keeps a typo from silently emptying the
// catalog: the error names the file.
func TestBrokenCatalogReportsTheFile(t *testing.T) {
	_, err := Load(fstest.MapFS{"i18n/tr.json": &fstest.MapFile{Data: []byte("{nope")}}, "i18n")
	if err == nil || !contains(err.Error(), "tr.json") {
		t.Fatalf("err = %v, want it to name the file", err)
	}
	_, err = Load(fstest.MapFS{"i18n/tr.json": &fstest.MapFile{Data: []byte(`["a"]`)}}, "i18n")
	if err == nil || !contains(err.Error(), "JSON object") {
		t.Fatalf("array catalog err = %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
