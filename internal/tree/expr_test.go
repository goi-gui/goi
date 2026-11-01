package tree

import (
	"runtime"
	"testing"
)

// TestOSExpression covers the platform global: a design can hide a title bar
// it did not write for this OS.
func TestOSExpression(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Rectangle { id: box visible: app.os == "linux" width: 40 height: 20 }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()
	box := tr.ids["box"].Base()
	if got, want := box.Visible(), runtime.GOOS == "linux"; got != want {
		t.Fatalf("visible = %v on %s", got, runtime.GOOS)
	}
}

// TestThemeExpressions covers the point of the expression layer: a view can ask
// about the scheme and pick a value, and it must follow a later switch.
func TestThemeExpressions(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		theme: light
		Column {
			Text { id: label text: app.dark ? "Dark" : "Light" color: app.dark ? onSurface : onPrimary }
			Rectangle { id: box visible: app.dark width: 40 height: 20 }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()
	label := tr.ids["label"].Base()
	if got, _ := label.PropValue("text"); got != "Light" {
		t.Fatalf("light text = %v", got)
	}
	if got := label.colorRoleFor("color"); got != "onPrimary" {
		t.Fatalf("light role = %q", got)
	}
	if box := tr.ids["box"].Base(); box.Visible() {
		t.Fatal("box is visible in light mode")
	}

	tr.Root.Theme.Set("dark")
	tr.ApplyExpressions()
	if got, _ := label.PropValue("text"); got != "Dark" {
		t.Fatalf("dark text = %v", got)
	}
	if got := label.colorRoleFor("color"); got != "onSurface" {
		t.Fatalf("dark role = %q", got)
	}
	if box := tr.ids["box"].Base(); !box.Visible() {
		t.Fatal("box is hidden in dark mode")
	}

	// Re-evaluating an unchanged expression must not keep the tree dirty, or
	// the frame loop would never go idle.
	tr.ids["label"].Base().ClearDirty()
	tr.ids["box"].Base().ClearDirty()
	tr.ApplyExpressions()
	if _, dirty := tr.ids["box"].Base().Dirty(); dirty {
		t.Fatal("unchanged expression marked the node dirty")
	}
}

// TestWindowExpressions checks the size globals and that a comparison result can
// feed a numeric property.
func TestWindowExpressions(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Rectangle { id: wide visible: window.width >= 640 width: window.width / 2 height: 20 }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.SetViewport(800, 600)
	tr.ApplyExpressions()
	wide := tr.ids["wide"].Base()
	if !wide.Visible() {
		t.Fatal("wide layout should show the rectangle")
	}
	if wide.Width() != 400 {
		t.Fatalf("width = %v, want 400", wide.Width())
	}
	tr.SetViewport(400, 600)
	tr.ApplyExpressions()
	if wide.Visible() {
		t.Fatal("narrow layout should hide the rectangle")
	}
}

// TestStringConcat keeps `text: a.text + "!"` working.
func TestStringConcat(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			Text { id: a text: "Hi" }
			Text { id: b text: a.text + "!" }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatal(diag)
	}
	tr.ApplyExpressions()
	if got, _ := tr.ids["b"].Base().PropValue("text"); got != "Hi!" {
		t.Fatalf("text = %v", got)
	}
}
