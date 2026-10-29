// Command uicheck loads .ui designs, expanding imports, and prints diagnostics.
//
//	go run ./cmd/uicheck examples/gallery/gallery.ui
//	go run ./cmd/uicheck examples/composition/Main.ui
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ksckaan1/goui/internal/asset"
	"github.com/ksckaan1/goui/internal/schema"
	"github.com/ksckaan1/goui/internal/tree"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: uicheck FILE.ui...")
		os.Exit(2)
	}
	fail := false
	for _, file := range os.Args[1:] {
		// The file's own directory is the asset container, so imports resolve
		// next to the entry file, exactly as LoadWindowFile does.
		dir := filepath.Dir(file)
		name := filepath.Base(file)
		b, diag := asset.Load(os.DirFS(dir), name)
		if !diag.Empty() {
			fmt.Fprintln(os.Stderr, diag.FormatFiles(name, b.Sources[name], b.Sources))
			fail = true
			continue
		}
		// A file with params is a fragment: it draws part of a design and gets
		// its values from whoever imports it, so it has no window root.
		if len(b.File.Params) > 0 {
			if _, diag := schema.Validate(b.File); !diag.Empty() {
				fmt.Fprintln(os.Stderr, diag.FormatFiles(name, b.Sources[name], b.Sources))
				fail = true
				continue
			}
			fmt.Printf("%s: ok (fragment, %d params, %d files)\n", file, len(b.File.Params), len(b.Files))
			continue
		}
		if _, diag := schema.ValidateEntry(b.File); !diag.Empty() {
			fmt.Fprintln(os.Stderr, diag.FormatFiles(name, b.Sources[name], b.Sources))
			fail = true
			continue
		}
		tr, err := tree.Build(b.File, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
			fail = true
			continue
		}
		fmt.Printf("%s: ok (%d nodes, %d ids, %d files)\n", file, len(tr.All()), tr.IDs(), len(b.Files))
	}
	if fail {
		os.Exit(1)
	}
}
