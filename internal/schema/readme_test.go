package schema

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/goi-gui/goi/internal/asset"
	"github.com/goi-gui/goi/internal/goifmt"
)

// TestReadmeExamplesValidate keeps the samples in README.md honest: they are
// what a reader copies, so each one is formatted, and every file that is not a
// fragment validates. The blocks name their file in a leading comment; an
// import then resolves against the other blocks the same way a reader's files
// would.
func TestReadmeExamplesValidate(t *testing.T) {
	body, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	blocks := regexp.MustCompile("(?s)```goi\n(.*?)```").FindAllStringSubmatch(page, -1)
	if len(blocks) == 0 {
		t.Fatal("README.md has no goi example")
	}
	// A labelled block may name a path (`// ui/Main.goi`), because the design's
	// imports are relative to that directory.
	name := regexp.MustCompile(`(?m)^// ([\w./-]+\.goi)$`)
	dir := t.TempDir()
	var names []string
	for _, block := range blocks {
		src := block[1]
		if got := string(goifmt.Source([]byte(src))); got != src {
			t.Errorf("README.md example is not formatted:\n%s\n--- goifmt ---\n%s", src, got)
		}
		m := name.FindStringSubmatch(src)
		if m == nil {
			t.Errorf("README.md example has no `// name.goi` line:\n%s", src)
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(m[1]))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, m[1])
	}
	for _, file := range names {
		b, diag := asset.Load(os.DirFS(dir), file)
		if !diag.Empty() {
			t.Errorf("README.md %s: %v", file, diag)
			continue
		}
		if b.File.Fragment {
			continue // loaded through the entry point that imports it
		}
		if _, diag := Validate(b.File); !diag.Empty() {
			t.Errorf("README.md %s: %v", file, diag)
		}
	}
}
