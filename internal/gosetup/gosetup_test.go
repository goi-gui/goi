package gosetup

import (
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/syntax"
)

func TestSymbolsAndCompletion(t *testing.T) {
	a := Analyze(`import "fmt"

var name = "Kaan"

func greet(n string) string { return fmt.Sprint("Hi, ", n) }
`)
	names := map[string]string{}
	for _, s := range a.Symbols() {
		names[s.Name] = s.Kind
	}
	if names["name"] != "variable" || names["greet"] != "function" {
		t.Fatalf("symbols = %v", names)
	}
	items := a.Complete(strings.LastIndex(a.code, "gr") + 2)
	found := false
	for _, item := range items {
		if item.Label == "greet" {
			found = true
		}
	}
	if !found {
		t.Fatalf("greet not completed: %v", items)
	}
	at := strings.LastIndex(a.code, "fmt.") + 4
	items = a.Complete(at)
	found = false
	for _, item := range items {
		if item.Label == "Sprint" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fmt.Sprint not completed: %v", items)
	}
}

func TestTypeDiagnostics(t *testing.T) {
	a := Analyze(`var count int = "not a number"`)
	if len(a.Diagnostics()) == 0 {
		t.Fatal("a type mismatch produced no diagnostic")
	}
}

func TestBridgeDeclarationsAreVisible(t *testing.T) {
	f, diag := syntax.Parse([]byte(`bridge {
	onSave(fileName string)
	getFileName() string
	content string
}

Window { }`))
	if !diag.Empty() {
		t.Fatal(diag)
	}
	a := AnalyzeWithBridge(`import "fmt"

var example string

func doSomething() {
	fileName := bridge.getFileName()
	bridge.onSave(fileName)
	fmt.Println(bridge.content)
}`, f.Vars, f.Shapes, f.Props)
	if ds := a.Diagnostics(); len(ds) != 0 {
		t.Fatalf("bridge access did not type-check: %v", ds)
	}
	at := strings.LastIndex(a.code, "bridge.") + len("bridge.")
	items := a.Complete(at)
	labels := map[string]bool{}
	for _, item := range items {
		labels[item.Label] = true
	}
	for _, want := range []string{"getFileName", "onSave", "content"} {
		if !labels[want] {
			t.Fatalf("bridge completion missing %q: %v", want, items)
		}
	}
}

func TestBridgeListKeepsItsDeclaredType(t *testing.T) {
	f, diag := syntax.Parse([]byte(`bridge {
	tickets []ticket
}

type ticket {
	title  string
	tags   []string
	unread bool
}

Window { }`))
	if !diag.Empty() {
		t.Fatal(diag)
	}
	a := AnalyzeWithBridge(`func unread() int {
	n := 0
	for _, t := range bridge.tickets {
		if t.unread {
			n++
		}
		n += len(t.tags)
		_ = t.title
	}
	return n
}`, f.Vars, f.Shapes, f.Props)
	if ds := a.Diagnostics(); len(ds) != 0 {
		t.Fatalf("a bridge list did not type-check as []ticket: %v", ds)
	}
}

func TestAutoImportAndUnusedImportOffsets(t *testing.T) {
	code := "import \"strings\"\n\nfunc label() string { return fmt.Sprintf(\"%d\", 1) }\n\nfunc broken() int { return missing }\n"
	a := Analyze(code)
	ds := a.Diagnostics()
	if len(ds) != 1 || !strings.Contains(ds[0].Msg, "undefined: missing") {
		t.Fatalf("diagnostics = %v", ds)
	}
	if want := strings.Index(code, "missing"); ds[0].Offset != want {
		t.Fatalf("offset = %d, want %d", ds[0].Offset, want)
	}
}

func TestPropsAreVisibleToSetup(t *testing.T) {
	f, diag := syntax.Parse([]byte(`props {
	title string
	count int
}

Window { }`))
	if !diag.Empty() {
		t.Fatal(diag)
	}
	code := `func label() string { return props.title + fmt.Sprint(props.count) }`
	a := AnalyzeWithBridge(code, f.Vars, f.Shapes, f.Props)
	if ds := a.Diagnostics(); len(ds) != 0 {
		t.Fatalf("props did not type-check: %v", ds)
	}
	at := strings.LastIndex(code, "props.") + len("props.")
	labels := map[string]bool{}
	for _, item := range a.Complete(at) {
		labels[item.Label] = true
	}
	if !labels["title"] || !labels["count"] {
		t.Fatalf("props completion = %v", labels)
	}
}
