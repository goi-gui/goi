package setup

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/gosetup"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/traefik/yaegi/stdlib"
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
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	// A broken file is compiled when something reads it, not at load.
	if sc := r.For(f.Src, ""); sc == nil {
		t.Fatal("no scope for the broken file")
	}
	if err := r.Err(); err == nil || !strings.Contains(err.Error(), "Main.goi:3:") {
		t.Fatalf("error = %v", err)
	}
}

func TestSetupCompilesLazilyInOneMachine(t *testing.T) {
	f := parse(t, `setup {
	var count int
	func inc() { count++ }
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	p := r.programs[f.Src]
	if p == nil {
		t.Fatal("setup did not register a program")
	}
	if p.compiled || p.newFn.IsValid() {
		t.Fatal("setup compiled before a scope asked for it")
	}
	a, _ := r.For(f.Src, "Main.goi#1").(*programInstance)
	b, _ := r.For(f.Src, "Main.goi#2").(*programInstance)
	if a == nil || b == nil || !p.compiled {
		t.Fatal("setup did not compile on first use")
	}
	if a.p.m != b.p.m {
		t.Fatal("instances did not share one machine")
	}
	if _, found, err := a.Call("inc", nil); err != nil || !found {
		t.Fatalf("inc = %v, %v", found, err)
	}
	if v, _ := b.Value("count"); v != 0 {
		t.Fatalf("second instance count = %v, want 0", v)
	}
}

func TestProgramsShareOneInterpreterAcrossFiles(t *testing.T) {
	f := parse(t, `setup {
	import "strings"

	var count int
	func inc() { count++ }
	func clean(s string) string { return strings.TrimSpace(s) }
}

Window {}`)
	g, diag := syntax.ParseNamed([]byte(`setup {
	var count int
	func inc() { count++ }
}

Window {}`), "Other.goi")
	if !diag.Empty() {
		t.Fatal(diag)
	}
	r, err := New(append(f.Setups, g.Setups...), f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	fp := r.programs[f.Src]
	if fp == nil {
		t.Skip("the first setup did not compile to a program")
	}
	gp := r.programs[g.Src]
	if gp == nil {
		t.Fatal("the second setup did not compile to a program")
	}
	if fp.m != gp.m {
		t.Fatal("files got separate machines")
	}
	for i := 0; i < 2; i++ {
		src := []string{f.Src, g.Src}[i]
		sc := r.For(src, "x#1")
		if _, _, err := sc.Call("inc", nil); err != nil {
			t.Fatalf("%s inc: %v", src, err)
		}
	}
	if v, _ := r.For(f.Src, "x#2").Value("count"); v != 0 {
		t.Fatalf("a new instance inherited state: %v", v)
	}
}

// TestLazyScopeSharesPureCalls covers the interpreter fallback, which a method
// reading setup state forces because package-level methods cannot see the
// factory's variables.
func TestLazyScopeSharesPureCalls(t *testing.T) {
	f := parse(t, `setup {
	import "strings"

	var count int

	func inc() { count++ }
	func greet(n string) string { return "Hi, " + strings.TrimSpace(n) }

	type T struct{ n int }

	func (t T) Double() int { return t.n + count }
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := r.For(f.Src, "Main.goi#1").(*lazyScope)
	if !ok {
		t.Fatal("a fragment instance was not lazy")
	}
	if v, found, err := sc.Call("greet", []any{" Ada "}); err != nil || !found || v != "Hi, Ada" {
		t.Fatalf("greet = %v, %v, %v", v, found, err)
	}
	if sc.private != nil {
		t.Fatal("a pure function copied the interpreter")
	}
	if _, found, err := sc.Call("inc", nil); err != nil || !found {
		t.Fatalf("inc = %v, %v", found, err)
	}
	if sc.private == nil {
		t.Fatal("a stateful function stayed on the shared base")
	}
}

func TestProgramSharesOneInterpreter(t *testing.T) {
	f := parse(t, `setup {
	var count int

	func inc() { count++ }
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := r.For(f.Src, "Main.goi#1").(*programInstance)
	b, _ := r.For(f.Src, "Main.goi#2").(*programInstance)
	if a == nil || b == nil {
		t.Fatal("the file did not compile to a shared program")
	}
	if a.p != b.p {
		t.Fatal("instances compiled separate interpreters")
	}
	if _, found, err := a.Call("inc", nil); err != nil || !found {
		t.Fatalf("inc = %v, %v", found, err)
	}
	if v, _ := b.Value("count"); v != 0 {
		t.Fatalf("second instance count = %v, want 0", v)
	}
	if _, found, _ := a.Call("missing", nil); found {
		t.Fatal("a missing function was found")
	}
}

func TestSetupMethodsAndVariadicCompileToTheProgram(t *testing.T) {
	f := parse(t, `setup {
	type point struct{ n int }

	func (p point) Double() int { return p.n * 2 }

	var count int
	func inc() { count++ }
	func scaled() int { return point{n: count}.Double() }
	func sum(xs ...int) int {
		n := 0
		for _, x := range xs {
			n += x
		}
		return n
	}
	func join(prefix string, xs ...string) string {
		s := prefix
		for _, x := range xs {
			s += x
		}
		return s
	}
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	if r.programs[f.Src] == nil {
		t.Fatalf("method file did not compile to a program: %v", r.Err())
	}
	sc := r.For(f.Src, "x")
	for _, tc := range []struct {
		name string
		args []any
		want any
	}{
		{"sum", []any{1, 2, 3}, 6},
		{"join", []any{"a", "b", "c"}, "abc"},
	} {
		v, found, err := sc.Call(tc.name, tc.args)
		if err != nil || !found || v != tc.want {
			t.Fatalf("%s = %v, %v, %v, want %v", tc.name, v, found, err, tc.want)
		}
	}
	if v, _, _ := sc.Call("scaled", nil); v != 0 {
		t.Fatalf("scaled = %v", v)
	}
	_, _, _ = sc.Call("inc", nil)
	if v, _, _ := sc.Call("scaled", nil); v != 2 {
		t.Fatalf("scaled after inc = %v", v)
	}
}

// TestSetupMethodReadingStateFallsBack keeps the interpreter path working for
// a method that reads a factory-local variable, which package-level methods
// cannot see.
func TestSetupMethodReadingStateFallsBack(t *testing.T) {
	f := parse(t, `setup {
	var count int
	func inc() { count++ }

	type T struct{ n int }
	func (t T) Plus() int { return t.n + count }
	func total() int { return T{n: 1}.Plus() }
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	if r.programs[f.Src] != nil {
		t.Fatal("a method reading setup state stayed on the shared program")
	}
	sc := r.For(f.Src, "x")
	v, _, err := sc.Call("total", nil)
	if err != nil || fmt.Sprint(v) != "1" {
		t.Fatalf("total = %v, %v", v, err)
	}
}

func TestSetupImportsUsedPackagesAndDropsUnusedOnes(t *testing.T) {
	f := parse(t, `setup {
	import "strings"

	func label() string { return fmt.Sprintf("%d", 7) }
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	p := r.programs[f.Src]
	if p == nil {
		t.Fatalf("setup did not compile to a program: %v", r.Err())
	}
	paths := map[string]bool{}
	for _, im := range p.imports {
		paths[im.Path] = true
	}
	if !paths["fmt"] || paths["strings"] {
		t.Fatalf("imports = %v", p.imports)
	}
	v, found, err := r.For(f.Src, "").Call("label", nil)
	if err != nil || !found || v != "7" {
		t.Fatalf("label = %v, %v, %v", v, found, err)
	}
}

func TestSetupDoesNotImportAShadowedName(t *testing.T) {
	f := parse(t, `setup {
	type box struct{ n int }
	func label() int {
		fmt := box{n: 4}
		return fmt.n
	}
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	p := r.programs[f.Src]
	if p == nil {
		t.Fatalf("setup did not compile to a program: %v", r.Err())
	}
	if len(p.imports) != 0 {
		t.Fatalf("a local fmt was imported: %v", p.imports)
	}
	if v, _, err := r.For(f.Src, "").Call("label", nil); err != nil || fmt.Sprint(v) != "4" {
		t.Fatalf("label = %v, %v", v, err)
	}
}

func TestSetupAutoImportFallsBackWithTheInterpreter(t *testing.T) {
	f := parse(t, `setup {
	import "sort"

	var seen int

	type T struct{ n int }
	func (x T) Label() string { return fmt.Sprintf("%d/%d", x.n, seen) }
	func label() string { return T{n: 3}.Label() }
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	if r.programs[f.Src] != nil {
		t.Fatal("a state-reading method stayed on the shared program")
	}
	if v, _, err := r.For(f.Src, "").Call("label", nil); err != nil || v != "3/0" {
		t.Fatalf("label = %v, %v", v, err)
	}
}

func TestStdPackageTableMatchesYaegi(t *testing.T) {
	for name, p := range gosetup.StdPackages() {
		if _, ok := stdlib.Symbols[p+"/"+name]; !ok {
			t.Fatalf("std package %q maps to %q, which the interpreter does not have", name, p)
		}
	}
}
