package tree

import "testing"

// TestCommonBoxSetters checks that the declarations a design writes are also
// reachable from Go, which is what an app needs when a size or a colour is
// decided at run time.
func TestCommonBoxSetters(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window {
		Column {
			id:         page
			width:      100
			height:     50
			background: theme.primary
			borderColor: theme.outline
		}
	} `), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	node, ok := tr.ByID("page")
	if !ok {
		t.Fatal("no page")
	}
	b := node.Base()

	b.SetWidth(320)
	b.SetHeight(180)
	if b.Width() != 320 || b.Height() != 180 {
		t.Fatalf("size = %v x %v, want 320 x 180", b.Width(), b.Height())
	}
	b.SetSize(240, 120)
	if b.Width() != 240 || b.Height() != 120 {
		t.Fatalf("SetSize left %v x %v", b.Width(), b.Height())
	}

	// A solid colour replaces the role the design wrote, or the resolver would
	// put the role back on the next theme pass.
	if role := b.colorRoleFor("background"); role != "theme:primary" {
		t.Fatalf("background role = %q, want primary", role)
	}
	b.SetBackground(0x112233FF)
	if got := b.Background(); got != 0x112233FF {
		t.Fatalf("background = %08x", got)
	}
	if role := b.colorRoleFor("background"); role != "" {
		t.Fatalf("background kept the role %q after a Go colour", role)
	}
	b.SetBorderColor(0x445566FF)
	if got := b.BorderColor(); got != 0x445566FF || b.colorRoleFor("borderColor") != "" {
		t.Fatalf("border colour = %08x role %q", got, b.colorRoleFor("borderColor"))
	}

	b.SetPadding(12)
	b.SetMargin(8)
	if b.Padding() != 12 || b.Margin() != 8 {
		t.Fatalf("padding/margin = %v/%v", b.Padding(), b.Margin())
	}
	b.SetRadius(9)
	if b.Radius() != 9 {
		t.Fatalf("radius = %v", b.Radius())
	}
	b.SetScroll(true)
	if b.ScrollAxis() != AxisY {
		t.Fatalf("a scrolling Column reports axis %v, want Y", b.ScrollAxis())
	}
	b.SetScroll(false)
	if b.ScrollAxis() != AxisNone {
		t.Fatal("scroll did not turn off")
	}
}

// TestScrollSnapSetters checks the snap properties an app may change at run
// time, including the bool spelling of the stop rule.
func TestScrollSnapSetters(t *testing.T) {
	tr, diag := BuildSource([]byte(`Window { Column { id: page scroll: true } }`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, _ := tr.ByID("page")
	b := n.Base()
	b.SetSnapType("mandatory")
	b.SetSnapStop(true)
	b.SetSnapPadding(16)
	b.SetSnapAlign("center")
	if b.SnapType() != "mandatory" || !b.SnapStop() || b.SnapPadding() != 16 || b.SnapAlign() != "center" {
		t.Fatalf("snap = %q stopp=%v pad=%v align=%q", b.SnapType(), b.SnapStop(), b.SnapPadding(), b.SnapAlign())
	}
	b.SetSnapStop(false)
	if b.SnapStop() {
		t.Fatal("SetSnapStop(false) kept the stop")
	}
}
