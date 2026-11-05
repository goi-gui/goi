// Command goicheck loads .goi designs, expanding imports, and prints diagnostics.
//
//	go run ./cmd/goicheck examples/gallery/gallery.goi
//	go run ./cmd/goicheck -layout examples/*/*.goi
//
// With -layout it also lays the design out headlessly and reports nodes that
// collapsed to zero size. That is the class of bug a syntax check cannot see: a
// container with a height but no width, or a row of children with no width to
// share, draws nothing and looks like a missing element.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ksckaan1/goi/internal/asset"
	"github.com/ksckaan1/goi/internal/layout"
	"github.com/ksckaan1/goi/internal/m3"
	"github.com/ksckaan1/goi/internal/paint"
	"github.com/ksckaan1/goi/internal/schema"
	"github.com/ksckaan1/goi/internal/syntax"
	"github.com/ksckaan1/goi/internal/text"
	"github.com/ksckaan1/goi/internal/tree"
)

func main() {
	checkLayout := flag.Bool("layout", false, "lay the design out and report collapsed nodes")
	ops := flag.Bool("ops", false, "print the display list op count")
	size := flag.String("size", "1100x900", "client size for -layout and -ops")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: goicheck [-layout] [-ops] FILE.goi...")
	}
	flag.Parse()
	if flag.NArg() < 2 && flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	// One font library for the whole run: measuring real text is what makes the
	// layout report match what the window draws.
	var measurer layout.Measurer = layout.FakeMeasurer{}
	var lib *text.Library
	if *checkLayout || *ops {
		lib = text.NewLibrary()
		if lib.HasFont() {
			measurer = fontMeasurer{lib: lib}
		}
	}

	fail := false
	for _, file := range flag.Args() {
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
		// A file with props or a slot is a fragment: it draws part of a design
		// and gets its values from whoever imports it, so it has no window root.
		fragment := b.File.IsFragment()
		if fragment {
			if _, diag := schema.Validate(b.File); !diag.Empty() {
				fmt.Fprintln(os.Stderr, diag.FormatFiles(name, b.Sources[name], b.Sources))
				fail = true
				continue
			}
			if !*checkLayout && !*ops {
				fmt.Printf("%s: ok (fragment, %d props, %d files)\n", file, len(b.File.Props), len(b.Files))
				continue
			}
			// Preview it on its own: fill the props with their defaults, then
			// wrap the root in a Window so it sizes the way the window would.
			body, pdiag := asset.Preview(b.File)
			if !pdiag.Empty() {
				fmt.Fprintln(os.Stderr, pdiag.FormatFiles(name, b.Sources[name], b.Sources))
				fail = true
				continue
			}
			preview := *body
			preview.Root = &syntax.Element{Name: "Window", Src: body.Src,
				Members: []syntax.Member{body.Root}}
			b.File = &preview
		} else if _, diag := schema.ValidateEntry(b.File); !diag.Empty() {
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
		line := fmt.Sprintf("%s: ok (%d nodes, %d ids, %d files)", file, len(tr.All()), tr.IDs(), len(b.Files))
		if *checkLayout || *ops {
			w, h := parseSize(*size)
			layout.New(measurer).Layout(tr.Root, w, h)
			if bad := collapsed(tr); len(bad) > 0 {
				for _, id := range bad {
					fmt.Fprintf(os.Stderr, "%s: %s has zero size after layout at %.0fx%.0f\n", file, id, w, h)
				}
				fail = true
				continue
			}
			line += fmt.Sprintf(", laid out at %.0fx%.0f", w, h)
		}
		if *ops {
			w, h := parseSize(*size)
			l := &paint.List{Theme: paintTheme(tr)}
			l.Build(tr, w, h)
			line += fmt.Sprintf(", %d ops", len(l.Ops))
		}
		fmt.Println(line)
	}
	if fail {
		os.Exit(1)
	}
}

// parseSize reads the -size flag, falling back to a roomy desktop window so a
// responsive design is checked at its widest tier by default.
func parseSize(s string) (float64, float64) {
	w, h := 1100.0, 900.0
	if _, err := fmt.Sscanf(s, "%fx%f", &w, &h); err != nil {
		return 1100, 900
	}
	return w, h
}

// collapsed lists nodes that are visible but cannot draw. An empty container is
// fine; a container with children that has no height (a column) or no width (a
// row) lays every child out at zero and draws nothing, which is the class of bug
// a syntax check cannot see. A leaf is reported when it has a surface or text but
// no area.
func collapsed(tr *tree.Tree) []string {
	var bad []string
	for _, n := range tr.All() {
		b := n.Base()
		if b.Parent() == nil || !shown(b) {
			continue
		}
		bb := b.Bounds()
		kids := 0
		for _, k := range b.Kids() {
			if k.Visible() {
				kids++
			}
		}
		if kids == 0 {
			// A leaf with something to draw but no room for it. An empty Text
			// draws nothing: a label filled from data is empty until the app
			// sets it.
			draws := b.Background() != 0 || b.BorderWidth() > 0
			if t, ok := n.(*tree.Text); ok {
				draws = draws || t.Text.Get() != ""
			} else if n.Kind() == "Text" {
				draws = true
			}
			if c, ok := n.(interface{ M3() *tree.Component }); ok {
				m := c.M3()
				draws = draws || m.Label.Get() != "" || m.Text.Get() != "" || m.Icon.Get() != ""
			}
			if draws && (bb.W == 0 || bb.H == 0) {
				bad = append(bad, name(n))
			}
			continue
		}
		switch n.Kind() {
		case "Divider", "Positioned", "Image", "SVG":
			continue
		}
		if bb.W == 0 || bb.H == 0 {
			bad = append(bad, name(n))
		}
	}
	return bad
}

func name(n tree.Node) string {
	if id := n.Base().ID(); id != "" {
		return id
	}
	return n.Kind()
}

// shown reports whether a node and every ancestor are visible.
func shown(b *tree.NodeBase) bool {
	if !b.Visible() {
		return false
	}
	for p := b.Parent(); p != nil; p = p.Base().Parent() {
		if !p.Base().Visible() {
			return false
		}
	}
	return true
}

// fontMeasurer measures with the same library the window uses, so a layout
// report matches the pixels.
type fontMeasurer struct{ lib *text.Library }

func (m fontMeasurer) Measure(s string, size, maxWidth float64) (float64, float64) {
	f := m.lib.Face(size, text.WeightRegular)
	if f == nil {
		return layout.FakeMeasurer{}.Measure(s, size, maxWidth)
	}
	return f.Measure(s, maxWidth)
}

func (m fontMeasurer) MeasureWeight(s string, size float64, weight int) (float64, float64) {
	f := m.lib.Face(size, text.Weight(weight))
	if f == nil {
		return layout.FakeMeasurer{}.MeasureWeight(s, size, weight)
	}
	return f.Measure(s, -1)
}

// paintTheme reads the root's theme so op colours match the design.
func paintTheme(tr *tree.Tree) *m3.Theme {
	if tr.Root.Theme.Get() == "dark" {
		return m3.Dark()
	}
	return m3.Light()
}
