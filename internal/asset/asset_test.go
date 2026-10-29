package asset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ksckaan1/goui/internal/schema"
	"github.com/ksckaan1/goui/internal/tree"
)

// design writes a set of files into a temp dir and returns an FS over it.
func design(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// load runs the whole pipeline the UI uses: expand, validate, build.
func load(t *testing.T, dir, entry string) (*tree.Tree, error) {
	t.Helper()
	b, diag := Load(os.DirFS(dir), entry)
	if !diag.Empty() {
		return nil, &errText{diag.FormatFiles(entry, b.Sources[entry], b.Sources)}
	}
	if _, diag := schema.Validate(b.File); !diag.Empty() {
		return nil, &errText{diag.FormatFiles(entry, b.Sources[entry], b.Sources)}
	}
	return tree.Build(b.File, nil)
}

type errText struct{ s string }

func (e *errText) Error() string { return e.s }

func TestImportExpandsToFragmentRoot(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Sidebar.ui" as Sidebar
Window {
	Row {
		Sidebar { }
	}
}`,
		"Sidebar.ui": `Column {
	width: 200
	Text { text: "Mail" }
}`,
	})
	tr, err := load(t, dir, "Main.ui")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range tr.All() {
		if n.Kind() == "Column" && n.Base().Width() == 200 {
			found = true
		}
	}
	if !found {
		t.Fatalf("fragment root did not replace the instance:\n%s", dump(tr))
	}
}

// TestImportPassesParams is the core feature: values flow from the instance into
// the fragment.
func TestImportPassesParams(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Sidebar.ui" as Sidebar
Window {
	Sidebar { title: "Inbox" }
}`,
		"Sidebar.ui": `param title: "none"
Column {
	Text { text: $title }
}`,
	})
	tr, err := load(t, dir, "Main.ui")
	if err != nil {
		t.Fatal(err)
	}
	got := textOf(tr)
	if got != "Inbox" {
		t.Fatalf("text = %q, want the param value", got)
	}
}

func TestParamDefaultApplies(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Sidebar.ui" as Sidebar
Window {
	Sidebar { }
}`,
		"Sidebar.ui": `param title: "default"
Column { Text { text: $title } }`,
	})
	tr, err := load(t, dir, "Main.ui")
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(tr); got != "default" {
		t.Fatalf("text = %q, want the default", got)
	}
}

// TestInstancePropsOverrideFragmentProps checks `Sidebar { width: 300 }` wins
// over the fragment's own width without producing a duplicate property.
func TestInstancePropsOverrideFragmentProps(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Sidebar.ui" as Sidebar
Window {
	Sidebar { width: 300 }
}`,
		"Sidebar.ui": `Column { width: 100 }
`,
	})
	tr, err := load(t, dir, "Main.ui")
	if err != nil {
		t.Fatal(err)
	}
	if got := tr.Root.Base().Kids()[0].Width(); got != 300 {
		t.Fatalf("width = %v, want the instance's 300", got)
	}
}

func TestParamInsideResponsiveMap(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Sidebar.ui" as Sidebar
Window {
	Sidebar { accent: #FF0000 }
}`,
		"Sidebar.ui": `param accent: #00FF00
Column {
	Text { text: "x" color: { base: $accent, md: $accent } }
}`,
	})
	tr, err := load(t, dir, "Main.ui")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			if got := n.(*tree.Text).Color.Get(); got != 0xFF0000FF {
				t.Fatalf("color = %08x, want the param value", got)
			}
			return
		}
	}
	t.Fatal("no text")
}

func TestTwoInstancesDoNotShareNodes(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Part.ui" as Part
Window {
	Column {
		Part { label: "one" }
		Part { label: "two" }
	}
}`,
		"Part.ui": `param label: ""
Text { text: $label }`,
	})
	tr, err := load(t, dir, "Main.ui")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			got = append(got, n.(*tree.Text).Text.Get())
		}
	}
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("texts = %v, want one and two", got)
	}
}

func TestNestedImport(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./parts/Page.ui" as Page
Window { Page { } }`,
		"parts/Page.ui": `import "./Card.ui" as Card
Column { Card { } }`,
		"parts/Card.ui": `Rectangle { width: 10 height: 10 }`,
	})
	tr, err := load(t, dir, "Main.ui")
	if err != nil {
		t.Fatal(err)
	}
	var rects int
	for _, n := range tr.All() {
		if n.Kind() == "Rectangle" {
			rects++
		}
	}
	if rects != 1 {
		t.Fatalf("rectangles = %d, want 1 (import paths are relative to the importing file)", rects)
	}
}

func TestImportCycleIsReported(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./A.ui" as A
Window { A { } }`,
		"A.ui": `import "./B.ui" as B
Column { B { } }`,
		"B.ui": `import "./A.ui" as A
Row { A { } }`,
	})
	_, err := load(t, dir, "Main.ui")
	if err == nil || !strings.Contains(err.Error(), "import cycle") {
		t.Fatalf("err = %v, want an import cycle", err)
	}
}

func TestMissingImportPointsAtTheImport(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Nope.ui" as Nope
Window { Nope { } }`,
	})
	_, err := load(t, dir, "Main.ui")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message names the missing file and points into Main.ui.
	if !strings.Contains(err.Error(), "Nope.ui") || !strings.Contains(err.Error(), "Main.ui:1:") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportOutsideRootIsRejected(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "../evil.ui" as Evil
Window { Evil { } }`,
	})
	_, err := load(t, dir, "Main.ui")
	if err == nil || !strings.Contains(err.Error(), "outside the asset root") {
		t.Fatalf("err = %v, want a rejection", err)
	}
}

func TestUnknownParamIsReported(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Part.ui" as Part
Window { Part { } }`,
		"Part.ui": `Column { Text { text: $nope } }`,
	})
	_, err := load(t, dir, "Main.ui")
	if err == nil || !strings.Contains(err.Error(), "has no param") {
		t.Fatalf("err = %v, want an unknown param", err)
	}
}

// TestUnknownInstancePropertyIsReported is the typo path: `Sidebar { widht: 1 }`
// must fail against the fragment root's schema.
func TestUnknownInstancePropertyIsReported(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Part.ui" as Part
Window { Part { widht: 10 } }`,
		"Part.ui": `Column { }`,
	})
	_, err := load(t, dir, "Main.ui")
	if err == nil || !strings.Contains(err.Error(), `unknown property "widht"`) {
		t.Fatalf("err = %v, want an unknown property", err)
	}
	// The position belongs to the instance, not to the fragment.
	if !strings.Contains(err.Error(), "Main.ui:") {
		t.Fatalf("err = %v, want a Main.ui position", err)
	}
}

func TestDuplicateIDAcrossInstancesIsReported(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Part.ui" as Part
Window {
	Column {
		Part { }
		Part { }
	}
}`,
		"Part.ui": `Text { id: same text: "x" }`,
	})
	_, err := load(t, dir, "Main.ui")
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("err = %v, want a duplicate id", err)
	}
}

func TestDataOnlyValuesInFragment(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.ui": `import "./Part.ui" as Part
Window { Part { label: "x" } }`,
		"Part.ui": `param label: ""
Column { Text { text: $label } }`,
	})
	// A bare word param value must work as a string, like every other string
	// property.
	tr, err := load(t, dir, "Main.ui")
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(tr); got != "x" {
		t.Fatalf("text = %q", got)
	}
}

func textOf(tr *tree.Tree) string {
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			return n.(*tree.Text).Text.Get()
		}
	}
	return ""
}

func dump(tr *tree.Tree) string {
	var b strings.Builder
	for _, n := range tr.All() {
		b.WriteString(n.Kind())
		b.WriteByte('\n')
	}
	return b.String()
}
