//go:build !js && (all || mermaid)

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/goi-gui/goi/internal/layout"
)

// TestMermaidRendersOffTheUIThread: a visible diagram starts a render that
// produces a bitmap, and the UI goroutine never waits for the rasterizer.
func TestMermaidRendersOffTheUIThread(t *testing.T) {
	a, win := reloadApp(t, `Window {
		Mermaid {
			id:     chart
			width:  400
			data:   "flowchart LR\n A[Start] --> B{Ready?}\n B -->|yes| C([Ship])\n B -->|no| D[Wait]"
		}
		Mermaid {
			id:     chart2
			width:  400
			data:   "flowchart LR\n A[Start] --> B{Ready?}\n B -->|yes| C([Ship])\n B -->|no| D[Wait]"
		}
	}`)
	defer a.closeMermaid()
	win.tree.SetViewport(600, 400)
	layout.New(textMeasurer{}).Layout(win.tree.Root, 600, 400)
	m := MustByID[*Mermaid](win, "chart")
	m2 := MustByID[*Mermaid](win, "chart2")
	if !a.syncMermaid() || !m.Loading || !m2.Loading {
		t.Fatal("a visible diagram did not start rendering")
	}
	deadline := time.Now().Add(60 * time.Second)
	for m.Data == nil && m.LoadErr == nil && time.Now().Before(deadline) {
		a.drainPosted()
		time.Sleep(5 * time.Millisecond)
	}
	if m.LoadErr != nil {
		t.Fatalf("render failed: %v", m.LoadErr)
	}
	if m.Data == nil || m.IntrinsicW <= 0 || m.IntrinsicH <= 0 {
		t.Fatalf("no diagram: data=%v size=%vx%v", m.Data, m.IntrinsicW, m.IntrinsicH)
	}
	if m.Loading || m2.Loading {
		t.Fatal("a node stayed loading after its bitmap arrived")
	}
	if m2.Data != m.Data {
		t.Fatal("two nodes with the same source rendered separate bitmaps")
	}
}

// TestMermaidWaitsForTheViewport: a diagram below a scrolling page does not
// compile the renderer until its box is on screen.
func TestMermaidWaitsForTheViewport(t *testing.T) {
	a, win := reloadApp(t, `Window {
		Column {
			id:     page
			scroll: true
			Rectangle { width: 100 height: 500 }
			Mermaid {
				id:    chart
				width: 200
				data:  "flowchart LR\n A --> B"
			}
		}
	}`)
	defer a.closeMermaid()
	win.tree.SetViewport(300, 120)
	layout.New(textMeasurer{}).Layout(win.tree.Root, 300, 120)
	m := MustByID[*Mermaid](win, "chart")
	if a.syncMermaid() {
		t.Fatal("an off-screen diagram started rendering")
	}
	if m.Loading || m.Data != nil {
		t.Fatal("an off-screen diagram was marked loading")
	}

	page := MustByID[*Column](win, "page")
	page.Base().SetScrollOffset(10_000)
	layout.New(textMeasurer{}).Layout(win.tree.Root, 300, 120)
	if !a.syncMermaid() || !m.Loading {
		t.Fatal("the diagram did not start after scrolling in")
	}
}

// TestMermaidMultilineData keeps the source as written: blank lines and leading
// indentation are part of the Mermaid code, not separate properties.
func TestMermaidMultilineData(t *testing.T) {
	src := "flowchart TD\n\nA --> B\nB --> C"
	_, win := reloadApp(t, `Window {
		Mermaid {
			id:   chart
			data: !"`+src+`"!
		}
	}`)
	m := MustByID[*Mermaid](win, "chart")
	if got := m.Source.Get(); got != src {
		t.Fatalf("data = %q, want %q", got, src)
	}
	if !strings.Contains(m.Source.Get(), "\n\n") {
		t.Fatal("the blank line did not survive")
	}
}

// TestMermaidReleasesAnOffscreenBitmap: scrolling a rendered diagram away keeps
// its intrinsic size for layout but drops the pixels; scrolling back uses the
// cache instead of starting from a blank box.
func TestMermaidReleasesAnOffscreenBitmap(t *testing.T) {
	a, win := reloadApp(t, `Window {
		Column {
			id:     page
			scroll: true
			Mermaid {
				id:     chart
				width:  200
				height: 150
				data:   "flowchart LR\n A --> B"
			}
			Rectangle { width: 100 height: 900 }
		}
	}`)
	defer a.closeMermaid()
	win.tree.SetViewport(300, 200)
	layout.New(textMeasurer{}).Layout(win.tree.Root, 300, 200)
	m := MustByID[*Mermaid](win, "chart")
	a.syncMermaid()
	deadline := time.Now().Add(60 * time.Second)
	for m.Data == nil && m.LoadErr == nil && time.Now().Before(deadline) {
		a.drainPosted()
		time.Sleep(5 * time.Millisecond)
	}
	if m.Data == nil {
		t.Fatalf("first render failed: %v", m.LoadErr)
	}
	first := m.Data
	w, h := m.IntrinsicW, m.IntrinsicH

	page := MustByID[*Column](win, "page")
	page.Base().SetScrollOffset(10_000)
	layout.New(textMeasurer{}).Layout(win.tree.Root, 300, 200)
	a.syncMermaid()
	if m.Data != nil {
		t.Fatal("an off-screen diagram kept its bitmap")
	}
	if m.IntrinsicW != w || m.IntrinsicH != h {
		t.Fatalf("size changed on unload: %vx%v -> %vx%v", w, h, m.IntrinsicW, m.IntrinsicH)
	}

	page.Base().SetScrollOffset(0)
	layout.New(textMeasurer{}).Layout(win.tree.Root, 300, 200)
	if !a.syncMermaid() || m.Data == nil {
		t.Fatal("the cached diagram did not come back")
	}
	if m.Data != first {
		t.Fatal("the cached bitmap was not reused")
	}
}
