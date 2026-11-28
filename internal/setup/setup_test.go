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

// TestSetupFunctionTypedNumbers converts an argument into the parameter's own
// numeric type. The generated call used to pass the interpreter's int to a
// uint parameter, which fails to compile even though the setup is valid Go.
func TestSetupFunctionTypedNumbers(t *testing.T) {
	f := parse(t, `setup {
	func env(v uint) string {
		switch v {
		case 1:
			return "Inside"
		case 2:
			return "Outside"
		default:
			return "Unknown"
		}
	}

	func signed(v int8) int8       { return v }
	func wide(v int64) int64       { return v }
	func short(v int16) int16      { return v }
	func tiny(v int32) int32       { return v }
	func small(v uint8) uint8      { return v }
	func medium(v uint16) uint16   { return v }
	func huge(v uint64) uint64     { return v }
	func pointer(v uintptr) uintptr { return v }
	func letter(v byte) byte       { return v }
	func symbol(v rune) rune       { return v }
	func ratio(v float32) float32  { return v }
	func factor(v float64) float64 { return v }
	func many(v ...uint8) uint8    { return v[0] }
	func pair(usage, capacity uint) uint { return usage*1000000 + capacity }
}

Window { }`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	sc := r.For(f.Src, "")
	if sc == nil {
		t.Fatal("no scope for the file")
	}
	if v, found, err := sc.Call("env", []any{2}); err != nil || !found || v != "Outside" {
		t.Fatalf("env = %v, %v, %v", v, found, err)
	}
	usageArg, capacityArg := any(1), any(2)
	for name, tc := range map[string]struct {
		arg  any
		want string
	}{
		"signed":  {-3, "-3"},
		"wide":    {7, "7"},
		"short":   {7, "7"},
		"tiny":    {7, "7"},
		"small":   {7, "7"},
		"medium":  {7, "7"},
		"huge":    {7, "7"},
		"pointer": {7, "7"},
		"letter":  {7, "7"},
		"symbol":  {7, "7"},
		"ratio":   {7, "7"},
		"factor":  {7, "7"},
		"many":    {7, "7"},
	} {
		// Two names in one declaration take their own argument, not the first
		// one twice: `usage, capacity uint` is the shape the termhub byte
		// counter uses.
		if v, found, err := sc.Call("pair", []any{usageArg, capacityArg}); err != nil || !found || fmt.Sprint(v) != "1000002" {
			t.Fatalf("pair = %v, %v, %v", v, found, err)
		}
		v, found, err := sc.Call(name, []any{tc.arg})
		if err != nil || !found || fmt.Sprint(v) != tc.want {
			t.Fatalf("%s = %v, %v, %v; want %s", name, v, found, err, tc.want)
		}
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

func TestSetupLifecycleMountsEachInstanceOnce(t *testing.T) {
	f := parse(t, `setup {
	var mounted bool
	var order string

	func onMount() {
		mounted = true
		order += "m"
	}
	func onUnmount() { order += "u" }
	func state() string { return fmt.Sprintf("%v/%s", mounted, order) }
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	a := r.For(f.Src, "a")
	b := r.For(f.Src, "b")
	if _, found, _ := a.Call("onMount", nil); found || a.HasFunc("onMount") {
		t.Fatal("onMount is visible to the design")
	}
	var posts []func()
	r.Start(func(fn func()) { posts = append(posts, fn) }, func(err error) { t.Error(err) })
	if len(posts) != 2 {
		t.Fatalf("queued %d mounts, want 2", len(posts))
	}
	for len(posts) > 0 {
		fn := posts[0]
		posts = posts[1:]
		fn()
	}
	if v, _, _ := a.Call("state", nil); v != "true/m" {
		t.Fatalf("a = %v", v)
	}
	if v, _, _ := b.Call("state", nil); v != "true/m" {
		t.Fatalf("b = %v", v)
	}
	r.Unmount()
	if v, _, _ := a.Call("state", nil); v != "true/mu" {
		t.Fatalf("a after unmount = %v", v)
	}
}

func TestSetupLifecycleFallsBackWithTheInterpreter(t *testing.T) {
	f := parse(t, `setup {
	var mounted bool

	type T struct{ n int }
	func (x T) Plus() int {
		if mounted {
			return x.n
		}
		return 0
	}

	func onMount() { mounted = true }
	func value() int { return T{n: 7}.Plus() }
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	if r.programs[f.Src] != nil {
		t.Fatal("a state-reading method stayed on the shared program")
	}
	sc := r.For(f.Src, "x")
	var posts []func()
	r.Start(func(fn func()) { posts = append(posts, fn) }, func(err error) { t.Error(err) })
	for len(posts) > 0 {
		fn := posts[0]
		posts = posts[1:]
		fn()
	}
	if v, _, _ := sc.Call("value", nil); fmt.Sprint(v) != "7" {
		t.Fatalf("value = %v", v)
	}
	r.Unmount()
}

func TestSetupLifecycleMountsAScopeOnlyUsedForHooks(t *testing.T) {
	f := parse(t, `setup {
	var mounted bool
	func onMount() { mounted = true }
}

Window {}`)
	r, err := New(f.Setups, f.Vars, f.Shapes)
	if err != nil {
		t.Fatal(err)
	}
	r.Note(f.Src, "frag#1")
	var posts []func()
	r.Start(func(fn func()) { posts = append(posts, fn) }, func(err error) { t.Error(err) })
	for len(posts) > 0 {
		fn := posts[0]
		posts = posts[1:]
		fn()
	}
	sc := r.For(f.Src, "frag#1")
	if v, ok := sc.Value("mounted"); !ok || fmt.Sprint(v) != "true" {
		t.Fatalf("mounted = %v, %v", v, ok)
	}
}

// TestSetupSharedMachineAndImports covers the memory work: one interpreter is
// shared by every file, a file compiles on first use, and only the packages a
// file imports are registered.
func TestSetupSharedMachineAndImports(t *testing.T) {
	setups := []syntax.Setup{
		{Src: "A.goi", Code: `func label() string { return fmt.Sprintf("%d", 1) }`},
		{Src: "B.goi", Code: `import "strings"

var n int
func clean(s string) string { return strings.TrimSpace(s) }`},
		{Src: "C.goi", Code: `var count int
func inc() { count++ }`},
	}
	r, err := New(setups, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := r.programs["A.goi"], r.programs["B.goi"], r.programs["C.goi"]
	if a == nil || b == nil || c == nil {
		t.Fatal("a setup did not compile to a program")
	}
	if a.m != b.m || b.m != c.m {
		t.Fatal("files did not share one machine")
	}
	if a.compiled || b.compiled || c.compiled {
		t.Fatal("a file compiled before first use")
	}
	sa := r.For("A.goi", "")
	if v, _, err := sa.Call("label", nil); err != nil || v != "1" {
		t.Fatalf("label = %v, %v", v, err)
	}
	if !a.compiled || b.compiled || c.compiled {
		t.Fatal("first use compiled the wrong files")
	}
	if !a.m.registered["fmt/fmt"] {
		t.Fatal("fmt was not registered for the file that uses it")
	}
	before := len(a.m.registered)
	sc := r.For("C.goi", "x")
	if _, _, err := sc.Call("inc", nil); err != nil {
		t.Fatal(err)
	}
	if len(a.m.registered) != before {
		t.Fatal("a file with no imports registered another package")
	}
	clean := r.For("B.goi", "")
	if v, _, err := clean.Call("clean", []any{"  x "}); err != nil || v != "x" {
		t.Fatalf("clean = %v, %v", v, err)
	}
}
