package setup

import (
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/syntax"
)

func parse(t *testing.T, src string) *syntax.File {
	t.Helper()
	f, diag := syntax.ParseNamed([]byte(src), "Main.goi")
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	return f
}

func TestSetupValuesAndFunctions(t *testing.T) {
	f := parse(t, `setup {
	import "strings"

	var name = "Kaan"

	func greet(n string) string {
		return "Hi, " + strings.TrimSpace(n)
	}
}

Window { Text { text: greet(name) } }`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	sc := r.For(f.Src, "")
	if sc == nil {
		t.Fatal("no scope for the file")
	}
	if v, ok := sc.Value("name"); !ok || v != "Kaan" {
		t.Fatalf("name = %v, %v", v, ok)
	}
	v, found, err := sc.Call("greet", []any{" Ada "})
	if err != nil || !found || v != "Hi, Ada" {
		t.Fatalf("greet = %v, %v, %v", v, found, err)
	}
	if _, found, _ := sc.Call("missing", nil); found {
		t.Fatal("a missing function was found")
	}
}

func TestSetupLoadsOnlyItsFile(t *testing.T) {
	f := parse(t, `setup {
	var count = 3
}

Window { Text { text: "x" } }`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	if r.For("Other.goi", "") != nil {
		t.Fatal("a scope leaked to another file")
	}
}

func TestSetupErrorNamesTheFile(t *testing.T) {
	f := parse(t, `setup {
	var name = "Kaan"
	var broken = missingName
}

Window {}`)
	_, err := New(f.Setups, f.Vars, f.Shapes)
	if err == nil {
		t.Fatal("broken setup loaded")
	}
	if !strings.Contains(err.Error(), "Main.goi") {
		t.Fatalf("error = %v", err)
	}
}
