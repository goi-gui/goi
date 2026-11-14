package ui

import (
	"testing"
	"testing/fstest"

	"github.com/goi-gui/goi/internal/layout"
)

// TestMermaidRendersOnSync: the ui layer turns the source into a bitmap before
// layout measures it, and a text change asks for another render.
func TestMermaidRendersOnSync(t *testing.T) {
	a, win := reloadApp(t, `Window { Column {
		Mermaid { id: diagram width: 300 text: "flowchart LR\n A-->B" }
	} }`)
	m := MustByID[*Mermaid](win, "diagram")
	if !a.syncMermaid() {
		t.Fatal("first sync did not render")
	}
	if m.Data == nil || m.IntrinsicW <= 0 || m.IntrinsicH <= 0 {
		t.Fatalf("no diagram: data=%v size=%gx%g err=%v", m.Data != nil, m.IntrinsicW, m.IntrinsicH, m.LoadErr)
	}
	if m.LoadErr != nil {
		t.Fatal(m.LoadErr)
	}
	// Layout uses the theme's diagram and the declared width.
	layout.New(nil).Layout(win.tree.Root, 500, 400)
	if b := m.Base().Bounds(); b.W != 300 || b.H <= 0 {
		t.Fatalf("bounds = %gx%g", b.W, b.H)
	}

	m.Text.Set("sequenceDiagram\n Alice->>Bob: Hi")
	if !a.syncMermaid() {
		t.Fatal("text change did not render")
	}
	if m.Data == nil {
		t.Fatalf("no diagram after change: %v", m.LoadErr)
	}
	if a.syncMermaid() {
		t.Fatal("an unchanged source rendered again")
	}
}

// TestMermaidErrorIsVisible: a broken source leaves the node drawable, and the
// error is what paint shows.
func TestMermaidErrorIsVisible(t *testing.T) {
	a, win := reloadApp(t, `Window { Mermaid { id: breakdown text: "not a diagram" } }`)
	m := MustByID[*Mermaid](win, "breakdown")
	a.syncMermaid()
	if m.LoadErr == nil || m.Data != nil {
		t.Fatalf("bad source: err=%v data=%v", m.LoadErr, m.Data != nil)
	}
}

// TestMermaidReadsASourceFile: the same component can point at a .mmd file in
// the design container, which keeps indentation-sensitive sources readable.
func TestMermaidReadsASourceFile(t *testing.T) {
	a, win := reloadApp(t, `Window { Mermaid { id: diagram src: "goi://chart.mmd" } }`)
	a.assets = fstest.MapFS{"chart.mmd": &fstest.MapFile{Data: []byte("flowchart LR\n A-->B\n")}}
	m := MustByID[*Mermaid](win, "diagram")
	if !a.syncMermaid() {
		t.Fatal("src did not render")
	}
	if m.Data == nil || m.LoadErr != nil {
		t.Fatalf("no diagram: %v", m.LoadErr)
	}
	a.assets = fstest.MapFS{}
	m.Src.Set("goi://missing.mmd")
	if !a.syncMermaid() {
		t.Fatal("missing src did not mark a change")
	}
	if m.LoadErr == nil {
		t.Fatal("missing source did not report an error")
	}
}
