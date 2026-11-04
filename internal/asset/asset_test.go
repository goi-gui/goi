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
func TestImportPassesProps(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Sidebar.goi" as Sidebar
Window {
	Sidebar { title: "Inbox" }
}`,
		"Sidebar.goi": `props { title string }
Column {
	Text { text: props.title }
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	got := textOf(tr)
	if got != "Inbox" {
		t.Fatalf("text = %q, want the prop value", got)
	}
}

func TestPropIsZeroWhenNotPassed(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Sidebar.goi" as Sidebar
Window {
	Sidebar { }
}`,
		"Sidebar.goi": `props { title string }
Column { Text { text: props.title } }`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(tr); got != "" {
		t.Fatalf("text = %q, want the string zero value", got)
	}
}

// TestPropWithoutDefaultGetsZeroValue is the contract for a prop the instance
// leaves out: Go's zero value for the declared type, so the fragment never has
// to guard for a missing value.
func TestPropWithoutDefaultGetsZeroValue(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Part.goi" as Part
Window {
	Column {
		Part { id: one label: "set" show: true }
		Part { id: none }
	}
}`,
		"Part.goi": `props {
	label string
	show bool
}
Column {
	Text { id: t text: props.label }
	Rectangle { id: box visible: props.show }
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"one.t": "set", "none.t": ""} {
		n, ok := tr.ByID(id)
		if !ok {
			t.Fatalf("%q is not registered:\n%s", id, dump(tr))
		}
		if got := n.(*tree.Text).Text.Get(); got != want {
			t.Fatalf("%s text = %q, want %q", id, got, want)
		}
	}
	one, _ := tr.ByID("one.box")
	none, _ := tr.ByID("none.box")
	if !shown(one) {
		t.Fatal("the passed bool was not used")
	}
	if shown(none) {
		t.Fatal("the missing bool prop is not the zero value")
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
		"Sidebar.goi": `props { accent color}
Column {
	Text { text: "x" color: { base: props.accent, md: props.accent } }
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			if got := n.(*tree.Text).Color.Get(); got != 0xFF0000FF {
				t.Fatalf("color = %08x, want the prop value", got)
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
		"Part.goi": `props { label string }
Text { text: props.label }`,
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

func TestUnknownPropIsReported(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Part.goi" as Part
Window { Part { } }`,
		"Part.goi": `Column { Text { text: props.nope } }`,
	})
	_, err := load(t, dir, "Main.goi")
	if err == nil || !strings.Contains(err.Error(), "has no prop") {
		t.Fatalf("err = %v, want an unknown prop", err)
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
		"Part.goi": `props { label string }
Column { Text { text: props.label } }`,
	})
	// A bare word prop value must work as a string, like every other string
	// property.
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(tr); got != "x" {
		t.Fatalf("text = %q", got)
	}
}

// TestFragmentDataMergesIntoTheEntry lets a fragment own the `data` block and
// the shape its {#each} walks; the app still sets it by name from the entry.
func TestFragmentDataMergesIntoTheEntry(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./List.goi" as List
Window {
	List { }
}`,
		"List.goi": `bridge {
	rows []row
}

type row {
	title string
}

Column {
	for _, row := range bridge.rows {
		Text { text: row.title }
	}
}`})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	tr.SetData("rows", []map[string]any{{"title": "one"}, {"title": "two"}})
	tr.ApplyExpressions()
	var texts []string
	for _, n := range tr.All() {
		if tx, ok := n.(*tree.Text); ok {
			texts = append(texts, tx.Text.Get())
		}
	}
	if strings.Join(texts, ",") != "one,two" {
		t.Fatalf("texts = %v, want [one two]", texts)
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

// TestFragmentInstanceCanBeATemplateRow lets one .goi file hold the row design
// and the list use it: the instance's `template: true` lands on the fragment
// root, so the row is hidden and ui.Fill can copy it.
func TestFragmentInstanceCanBeATemplateRow(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Row.goi" as Row
Window {
	List {
		id: list
		Row { template: true supporting: "x" }
	}
}`,
		"Row.goi": `props { supporting string }
ListItem {
	label: "Row"
	supporting: props.supporting
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := tr.ByID("list")
	if !ok {
		t.Fatal("no list")
	}
	var row tree.Node
	for _, c := range list.Base().Children() {
		row = c
	}
	if row == nil || !row.Base().Template() || row.Base().Visible() {
		t.Fatalf("the fragment instance is not a hidden template row:\n%s", dump(tr))
	}
}

// TestBlockSurvivesImportExpansion guards the copy path: a control block inside
// a fragment must reach the tree, with its arms, props and slots intact.
func TestBlockSurvivesImportExpansion(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Card.goi" as Card
Window {
	Card {
		mode: "fancy"
		Text { id: passed text: "passed" }
	}
}`,
		"Card.goi": `props { mode string}
Column {
	if props.mode == "fancy" {
		Text { id: fancy text: "fancy" }
	} else {
		Column {
			$children
		}
	}
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	// Both arms reach the tree; the prop decides which one is shown.
	fancy, ok := tr.ByID("fancy")
	if !ok {
		t.Fatalf("the if arm was dropped:\n%s", dump(tr))
	}
	passed, ok := tr.ByID("passed")
	if !ok {
		t.Fatalf("the slot inside the else arm was not filled:\n%s", dump(tr))
	}
	if !shown(fancy) || shown(passed) {
		t.Fatal("the prop did not pick the if arm")
	}
}

// shown reports whether n is drawn: a hidden ancestor hides it too.
func shown(n tree.Node) bool {
	for n != nil {
		if !n.Base().Visible() {
			return false
		}
		n = n.Base().Parent()
	}
	return true
}

// TestPropNamedIDOwnsTheInstanceProperty pins the one name that two features
// share: a fragment that declares an `id` prop takes the instance's `id:` as
// that value, so instance-id scoping stays off for it.
func TestPropNamedIDOwnsTheInstanceProperty(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Card.goi" as Card
Window {
	Column {
		Card { id: "one" }
	}
}`,
		"Card.goi": `props { id string }
Column {
	Text { id: title text: props.id }
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	n, ok := tr.ByID("title")
	if !ok {
		t.Fatalf("the inner id was scoped by a prop:\n%s", dump(tr))
	}
	if got := n.(*tree.Text).Text.Get(); got != "one" {
		t.Fatalf("text = %q, want the prop value", got)
	}
}

// TestPropCanCarryAList makes a fragment declare `props { rows []ticket }` and
// the instance pass a bridge list, which its {#each} then walks.
func TestPropCanCarryAList(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./List.goi" as List
bridge { rows []ticket }
type ticket { title string }
Window {
	Column {
		List { rows: bridge.rows }
	}
}`,
		"List.goi": `props { rows []ticket }
Column {
	for _, row := range props.rows {
		Text { text: row.title }
	}
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	tr.SetData("rows", []map[string]any{{"title": "one"}, {"title": "two"}})
	tr.ApplyExpressions()
	var got []string
	for _, n := range tr.All() {
		if n.Kind() == "Text" {
			got = append(got, n.(*tree.Text).Text.Get())
		}
	}
	if strings.Join(got, ",") != "one,two" {
		t.Fatalf("texts = %v, want the two rows", got)
	}
}

// TestPropCanCarryAHandler lets the instance decide which handler a fragment's
// event calls.
func TestPropCanCarryAHandler(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Row.goi" as Row
bridge { onOpen func(id string) }
Window {
	Column {
		Row { onOpen: bridge.onOpen }
	}
}`,
		"Row.goi": `props { onOpen func(id string) }
Text { id: row text: "row" onClick: props.onOpen("7") }`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := tr.SetHandler("onOpen", func(id string) { got = append(got, id) }); err != nil {
		t.Fatal(err)
	}
	n, ok := tr.ByID("row")
	if !ok {
		t.Fatalf("no row:\n%s", dump(tr))
	}
	n.Base().Click.Emit(tree.ClickEvent{})
	if len(got) != 1 || got[0] != "7" {
		t.Fatalf("handler calls = %v, want one with 7", got)
	}
}

// TestFragmentTakesAValueAndAHandler is the shape examples/bridge uses: one
// fragment receives a bridge value and a bridge handler as props, shows the
// value and calls the handler, and never names `bridge` itself.
func TestFragmentTakesAValueAndAHandler(t *testing.T) {
	dir := design(t, map[string]string{
		"Main.goi": `import "./Counter.goi" as Counter
bridge {
	Step   int
	onStep func(delta int)
}
Window {
	Column {
		Counter { Label: "Clicks" Count: bridge.Step OnStep: bridge.onStep }
	}
}`,
		"Counter.goi": `props {
	Label  string
	Count  int
	OnStep func(delta int)
}
Card {
	Column {
		Text { id: label text: formatf("%s: %d", props.Label, props.Count) }
		Button { id: plus label: "+1" onClick: props.OnStep(1) }
		Button { id: minus label: "-1" onClick: props.OnStep(-1) }
	}
}`,
	})
	tr, err := load(t, dir, "Main.goi")
	if err != nil {
		t.Fatal(err)
	}
	tr.SetData("Step", 3)
	tr.ApplyExpressions()
	label, ok := tr.ByID("label")
	if !ok {
		t.Fatalf("no label:\n%s", dump(tr))
	}
	if got := label.(*tree.Text).Text.Get(); got != "Clicks: 3" {
		t.Fatalf("label = %q, want the prop value", got)
	}

	var got []int
	if err := tr.SetHandler("onStep", func(d int) { got = append(got, d) }); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"plus", "minus"} {
		n, ok := tr.ByID(id)
		if !ok {
			t.Fatalf("no %s:\n%s", id, dump(tr))
		}
		n.Base().Click.Emit(tree.ClickEvent{})
	}
	if len(got) != 2 || got[0] != 1 || got[1] != -1 {
		t.Fatalf("calls = %v, want [1 -1]", got)
	}

	// Go writes the value the fragment shows; the design follows.
	tr.SetData("Step", 10)
	tr.ApplyExpressions()
	if got := label.(*tree.Text).Text.Get(); got != "Clicks: 10" {
		t.Fatalf("label after SetData = %q", got)
	}
}
