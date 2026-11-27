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
}`, f.Vars, f.Shapes)
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
