package syntax

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenAST pins the parse of representative files. Update with
// `go test ./internal/syntax -update`.
func TestGoldenAST(t *testing.T) {
	entries, err := filepath.Glob("testdata/*.ui")
	if err != nil || len(entries) == 0 {
		t.Fatalf("no golden inputs: %v", err)
	}
	for _, in := range entries {
		name := filepath.Base(in)
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			f, diag := Parse(src)
			if !diag.Empty() {
				t.Fatalf("diagnostics: %v", diag)
			}
			got := Dump(f)
			golden := in + ".ast"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden (run with -update): %v", err)
			}
			if got != string(want) {
				t.Fatalf("AST mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}
