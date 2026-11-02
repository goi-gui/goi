package asset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ksckaan1/goi/internal/schema"
	"github.com/ksckaan1/goi/internal/tree"
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
		"Main.goi": `import "./Sidebar.goi" as Sidebar
Window {
	Row {
		Sidebar { }
	}
}`,
		"Sidebar.goi": `Column {
	width: 200
	Text { text: "Mail" }
}`,
	})
	tr, err := load(t, dir, "Main.goi")
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
		"Main.goi": `import "./Sidebar.goi" as Sidebar
Window {
	Sidebar { title: "Inbox" }
}`,
		"Sidebar.goi": `param title: "none"
Column {
	Text { text: $title }
}`,
	})
	tr, err := load(t, dir, "Main.goi")
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
		"Main.goi": `import "./Sidebar.goi" as Sidebar
Window {
	Sidebar { }
}`,
		"Sidebar.goi": `param title: "default"
Column { Text { text: $title } }`,
	})
	tr, err := load(t, dir, "Main.goi")
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
		"Main.goi": `import "./Sidebar.goi" as Sidebar
Window {
	Sidebar { width: 300 }
}`,
		"Sidebar.goi": `Column { width: 100 }
`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	if got := tr.Root.Base().Kids()[0].Width(); got != 300 {
		t.Fatalf("width = %v, want the instance's 300", got)
	}
}

func TestParamInsideResponsiveMap(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Sidebar.goi" as Sidebar
Window {
	Sidebar { accent: #FF0000 }
}`,
		"Sidebar.goi": `param accent: #00FF00
Column {
	Text { text: "x" color: { base: $accent, md: $accent } }
}`,
	})
	tr, err := load(t, dir, "Main.goi")
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
		"Main.goi": `import "./Part.goi" as Part
Window {
	Column {
		Part { label: "one" }
		Part { label: "two" }
	}
}`,
		"Part.goi": `param label: ""
Text { text: $label }`,
	})
	tr, err := load(t, dir, "Main.goi")
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
		"Main.goi": `import "./parts/Page.goi" as Page
Window { Page { } }`,
		"parts/Page.goi": `import "./Card.goi" as Card
Column { Card { } }`,
		"parts/Card.goi": `Rectangle { width: 10 height: 10 }`,
	})
	tr, err := load(t, dir, "Main.goi")
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
		"Main.goi": `import "./A.goi" as A
Window { A { } }`,
		"A.goi": `import "./B.goi" as B
Column { B { } }`,
		"B.goi": `import "./A.goi" as A
Row { A { } }`,
	})
	_, err := load(t, dir, "Main.goi")
	if err == nil || !strings.Contains(err.Error(), "import cycle") {
		t.Fatalf("err = %v, want an import cycle", err)
	}
}

func TestMissingImportPointsAtTheImport(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Nope.goi" as Nope
Window { Nope { } }`,
	})
	_, err := load(t, dir, "Main.goi")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message names the missing file and points into Main.goi.
	if !strings.Contains(err.Error(), "Nope.goi") || !strings.Contains(err.Error(), "Main.goi:1:") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportOutsideRootIsRejected(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "../evil.goi" as Evil
Window { Evil { } }`,
	})
	_, err := load(t, dir, "Main.goi")
	if err == nil || !strings.Contains(err.Error(), "outside the asset root") {
		t.Fatalf("err = %v, want a rejection", err)
	}
}

func TestUnknownParamIsReported(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Part.goi" as Part
Window { Part { } }`,
		"Part.goi": `Column { Text { text: $nope } }`,
	})
	_, err := load(t, dir, "Main.goi")
	if err == nil || !strings.Contains(err.Error(), "has no param") {
		t.Fatalf("err = %v, want an unknown param", err)
	}
}

// TestUnknownInstancePropertyIsReported is the typo path: `Sidebar { widht: 1 }`
// must fail against the fragment root's schema.
func TestUnknownInstancePropertyIsReported(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Part.goi" as Part
Window { Part { widht: 10 } }`,
		"Part.goi": `Column { }`,
	})
	_, err := load(t, dir, "Main.goi")
	if err == nil || !strings.Contains(err.Error(), `unknown property "widht"`) {
		t.Fatalf("err = %v, want an unknown property", err)
	}
	// The position belongs to the instance, not to the fragment.
	if !strings.Contains(err.Error(), "Main.goi:") {
		t.Fatalf("err = %v, want a Main.goi position", err)
	}
}

func TestDuplicateIDAcrossInstancesIsReported(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Part.goi" as Part
Window {
	Column {
		Part { }
		Part { }
	}
}`,
		"Part.goi": `Text { id: same text: "x" }`,
	})
	_, err := load(t, dir, "Main.goi")
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("err = %v, want a duplicate id", err)
	}
}

func TestDataOnlyValuesInFragment(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Part.goi" as Part
Window { Part { label: "x" } }`,
		"Part.goi": `param label: ""
Column { Text { text: $label } }`,
	})
	// A bare word param value must work as a string, like every other string
	// property.
	tr, err := load(t, dir, "Main.goi")
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

// TestChildrenLandAtTheSlot is the point of `$children`: the instance's child
// elements replace the marker, and they resolve their own imports in the file
// they were written in, not in the fragment.
func TestChildrenLandAtTheSlot(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Card.goi" as Card
import "./Stripe.goi" as Stripe
Window {
	Card {
		width: 300
		Stripe { }
	}
}`,
		"Card.goi": `Column {
	$children
}`,
		"Stripe.goi": `Row {
	height: 40
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range tr.All() {
		if n.Kind() != "Row" || n.Base().Height() != 40 {
			continue
		}
		p := n.Base().Parent()
		if p == nil || p.Kind() != "Column" || p.Base().Width() != 300 {
			t.Fatalf("child was not placed inside the Card root:\n%s", dump(tr))
		}
		return
	}
	t.Fatalf("the child element did not land at the slot:\n%s", dump(tr))
}

// TestChildrenWithoutASlotIsAnError keeps a wrong call from being silently
// dropped, and points at the children in the file that passed them.
func TestChildrenWithoutASlotIsAnError(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Card.goi" as Card
Window {
	Card { Text { text: "x" } }
}`,
		"Card.goi": `Column {
	width: 100
}`,
	})
	b, diag := Load(os.DirFS(dir), "Main.goi")
	if diag.Empty() {
		t.Fatalf("children were accepted by a fragment with no slot")
	}
	got := diag.FormatFiles("Main.goi", b.Sources["Main.goi"], b.Sources)
	if !strings.Contains(got, "Card does not take children") || !strings.Contains(got, "Main.goi:3") {
		t.Fatalf("diagnostic = %s", got)
	}
}

// TestSlotWithoutChildrenDrawsNothing lets a fragment be previewed on its own:
// the slot is a hole, not an error.
func TestSlotWithoutChildrenDrawsNothing(t *testing.T) {
	dir := design(t, map[string]string{
		"Card.goi": `Column {
	width: 100
	$children
}`,
	})
	b, diag := Load(os.DirFS(dir), "Card.goi")
	if !diag.Empty() {
		t.Fatalf("a fragment with a slot failed to load: %s", diag.FormatFiles("Card.goi", b.Sources["Card.goi"], b.Sources))
	}
	if !b.File.IsFragment() {
		t.Fatal("a file with $children is not treated as a fragment")
	}
	if _, diag := schema.Validate(b.File); !diag.Empty() {
		t.Fatalf("a fragment with a slot is not valid on its own: %v", diag)
	}
}
