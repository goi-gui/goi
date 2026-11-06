package schema

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestComponentDocsExamplesValidate keeps the reference honest: every page in
// the docs site's components folder has one .goi block, and the language
// accepts it. A property that the schema and the node disagree about showed up
// here first. The docs live in the sibling goi-gui/docs repository, so CI
// without that checkout skips the test.
func TestComponentDocsExamplesValidate(t *testing.T) {
	dir := "../../../docs/docs/components"
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			t.Skip("docs site is not checked out next to the repo")
		}
		t.Fatal(err)
	}
	pages, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 40 {
		t.Fatalf("found %d component pages, expected the whole set", len(pages))
	}
	block := regexp.MustCompile("(?s)```goi\n(.*?)```")
	for _, page := range pages {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		m := block.FindSubmatch(body)
		if m == nil {
			t.Errorf("%s: no .goi example", page)
			continue
		}
		src := string(m[1])
		if !regexp.MustCompile(`(?m)^Window\b`).MatchString(src) {
			src = "Window {\n" + src + "\n}"
		}
		if _, diag := validate(t, src); !diag.Empty() {
			t.Errorf("%s: %v", page, diag)
		}
	}
}
