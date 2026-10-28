// Command uicheck parses and validates .ui files, printing diagnostics.
//
//	go run ./cmd/uicheck examples/gallery/gallery.ui
package main

import (
	"fmt"
	"os"

	"github.com/ksckaan1/goui/internal/schema"
	"github.com/ksckaan1/goui/internal/syntax"
	"github.com/ksckaan1/goui/internal/tree"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: uicheck FILE.ui...")
		os.Exit(2)
	}
	fail := false
	for _, path := range os.Args[1:] {
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			fail = true
			continue
		}
		f, diag := syntax.Parse(src)
		if !diag.Empty() {
			fmt.Fprintln(os.Stderr, diag.Format(path, src))
			fail = true
			continue
		}
		if _, diag := schema.Validate(f); !diag.Empty() {
			fmt.Fprintln(os.Stderr, diag.Format(path, src))
			fail = true
			continue
		}
		tr, diag := tree.BuildSource(src, nil)
		if !diag.Empty() {
			fmt.Fprintln(os.Stderr, diag.Format(path, src))
			fail = true
			continue
		}
		fmt.Printf("%s: ok (%d nodes, %d ids)\n", path, len(tr.All()), tr.IDs())
	}
	if fail {
		os.Exit(1)
	}
}
