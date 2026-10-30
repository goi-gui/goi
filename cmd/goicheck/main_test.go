package main

import (
	"testing"

	"github.com/ksckaan1/goi/internal/layout"
	"github.com/ksckaan1/goi/internal/tree"
)

// TestCollapsedFindsEmptyContainers is the check that would have caught a Stack
// with a height but no width: it draws nothing and no syntax error fires.
func TestCollapsedFindsEmptyContainers(t *testing.T) {
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			// A card with a label but no space to draw it.
			Card { id: noWidth label: "invisible" }
			Column { id: empty }
			Text { id: fine text: "ok" }
			Rectangle { id: hidden width: 10 height: 10 visible: false }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	layout.New(layout.FakeMeasurer{}).Layout(tr.Root, 400, 300)
	bad := collapsed(tr)
	if len(bad) != 1 || bad[0] != "noWidth" {
		t.Fatalf("collapsed = %v, want [noWidth]", bad)
	}
}
