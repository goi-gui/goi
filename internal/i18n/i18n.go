// Package i18n loads the translation catalogs a design ships next to its .goi
// files: a folder named i18n holding one JSON file per language, named after
// its code (`en.json`, `tr.json`). A nested object flattens to a dotted key, so
//
//	{"settings": {"theme": "Tema"}}
//
// is read as `t("settings.theme")`. The folder is optional: without it every
// `t(...)` falls back to its own key.
package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Dir is the folder name a design uses for its catalogs, relative to the asset
// container's root.
const Dir = "i18n"

// Bundle is every catalog of one design, keyed by language code.
type Bundle struct {
	langs map[string]map[string]string
}

// Load reads every `.json` file in dir. A missing folder is not an error: i18n
// is optional, and the caller gets a nil bundle.
func Load(fsys fs.FS, dir string) (*Bundle, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("i18n: %s: %w", dir, err)
	}
	b := &Bundle{langs: map[string]map[string]string{}}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		lang := strings.TrimSuffix(name, ".json")
		if lang == "" {
			continue
		}
		src, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", name, err)
		}
		keys, err := parse(src, name)
		if err != nil {
			return nil, err
		}
		b.langs[lang] = keys
	}
	if len(b.langs) == 0 {
		return nil, nil
	}
	return b, nil
}

// parse reads one catalog and flattens nested objects into dotted keys.
func parse(src []byte, name string) (map[string]string, error) {
	var raw any
	if err := json.Unmarshal(src, &raw); err != nil {
		return nil, fmt.Errorf("i18n: %s: %w", name, err)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("i18n: %s: the catalog must be a JSON object", name)
	}
	out := map[string]string{}
	flatten("", obj, out)
	return out, nil
}

func flatten(prefix string, obj map[string]any, out map[string]string) {
	for k, v := range obj {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch x := v.(type) {
		case map[string]any:
			flatten(key, x, out)
		case string:
			out[key] = x
		case float64, bool:
			out[key] = fmt.Sprint(x)
		case nil:
			out[key] = ""
		}
	}
}

// Languages lists the catalog codes in order, for a settings menu or a test.
func (b *Bundle) Languages() []string {
	if b == nil {
		return nil
	}
	out := make([]string, 0, len(b.langs))
	for code := range b.langs {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// Has reports whether the bundle carries a catalog for code.
func (b *Bundle) Has(code string) bool {
	_, ok := b.lang(code)
	return ok
}

// Translate looks key up in lang, then in fallback, and returns the key itself
// when neither has it. args fill the format verbs when any are given, so
// "welcome_with_name": "Hoşgeldin %s" works like formatf.
func (b *Bundle) Translate(lang, fallback, key string, args ...any) string {
	if s, ok := b.Lookup(lang, fallback, key); ok {
		return apply(s, args)
	}
	return key
}

// Lookup returns the raw pattern, before any format verbs are filled: the
// caller needs it to know which verbs want an integer. It checks lang, then
// fallback, and reports false when neither has the key.
func (b *Bundle) Lookup(lang, fallback, key string) (string, bool) {
	if s, ok := b.lookup(lang, key); ok {
		return s, true
	}
	return b.lookup(fallback, key)
}

func (b *Bundle) lookup(lang, key string) (string, bool) {
	keys, ok := b.lang(lang)
	if !ok {
		return "", false
	}
	s, ok := keys[key]
	return s, ok
}

// lang finds a catalog by exact code, then by its base code: the desktop may
// report "en-US" while the design ships "en.json", and that is the file meant.
func (b *Bundle) lang(code string) (map[string]string, bool) {
	if b == nil || code == "" {
		return nil, false
	}
	if keys, ok := b.langs[code]; ok {
		return keys, true
	}
	lower := strings.ToLower(code)
	for name, keys := range b.langs {
		if strings.ToLower(name) == lower {
			return keys, true
		}
	}
	base := lower
	if i := strings.IndexAny(base, "-_"); i > 0 {
		base = base[:i]
	}
	if base != lower {
		if keys, ok := b.langs[base]; ok {
			return keys, true
		}
		for name, keys := range b.langs {
			if strings.ToLower(name) == base {
				return keys, true
			}
		}
	}
	return nil, false
}

// apply fills the format verbs, leaving a catalog entry without arguments
// untouched so a stray `%` in a translation prints as written.
func apply(s string, args []any) string {
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}
