package tree

import "testing"

func buildResponsive(t *testing.T, src string) *Tree {
	t.Helper()
	tr, diag := BuildSource([]byte(src), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	return tr
}

func TestApplyBreakpointsPicksLargestTier(t *testing.T) {
	tr := buildResponsive(t, `Window {
		width: { base: 320, md: 480, lg: 640 }
		Column {
			id: box
			padding: { base: 8, md: 16 }
			align: { md: center }
		}
	}`)
	box, ok := tr.ByID("box")
	if !ok {
		t.Fatal("no box")
	}
	col := box.(*Column)

	cases := []struct {
		width, winW, pad float64
		align            string
	}{
		{100, 320, 8, "start"},
		{639, 320, 8, "start"},
		{767, 320, 8, "start"},
		{768, 480, 16, "center"},
		{1023, 480, 16, "center"},
		{1024, 640, 16, "center"},
		{5000, 640, 16, "center"},
	}
	for _, c := range cases {
		tr.ApplyBreakpoints(c.width)
		if got := tr.Root.Width(); got != c.winW {
			t.Errorf("width %v: window width = %v, want %v", c.width, got, c.winW)
		}
		if got := box.Base().Padding(); got != c.pad {
			t.Errorf("width %v: padding = %v, want %v", c.width, got, c.pad)
		}
		if got := col.Align.Get(); got != c.align {
			t.Errorf("width %v: align = %q, want %q", c.width, got, c.align)
		}
	}
}

func TestApplyBreakpointsRestoresFallback(t *testing.T) {
	tr := buildResponsive(t, `Window {
		Column { id: box padding: { md: 16 } }
	}`)
	box, _ := tr.ByID("box")

	tr.ApplyBreakpoints(800)
	if got := box.Base().Padding(); got != 16 {
		t.Fatalf("padding at 800 = %v, want 16", got)
	}
	tr.ApplyBreakpoints(300)
	if got := box.Base().Padding(); got != 0 {
		t.Fatalf("padding at 300 = %v, want the node default 0", got)
	}
}

func TestApplyBreakpointsMarksOnlyOnChange(t *testing.T) {
	tr := buildResponsive(t, `Window {
		Column { id: box padding: { base: 8, md: 16 } }
	}`)
	tr.clearDirty()

	tr.ApplyBreakpoints(800)
	if _, dirty := tr.Root.Base().Dirty(); !dirty {
		t.Fatal("crossing md should mark the tree dirty")
	}

	tr.clearDirty()
	tr.ApplyBreakpoints(1200)
	if _, dirty := tr.Root.Base().Dirty(); dirty {
		t.Fatal("the same tier must not mark the tree dirty")
	}
}

func TestApplyBreakpointsOverridesGoSet(t *testing.T) {
	tr := buildResponsive(t, `Window {
		Column { id: box padding: { md: 16 } }
	}`)
	box, _ := tr.ByID("box")
	tr.ApplyBreakpoints(800)

	box.Base().padding.Set(99)
	if got := box.Base().Padding(); got != 99 {
		t.Fatalf("Go-set padding = %v", got)
	}
	tr.ApplyBreakpoints(800)
	if got := box.Base().Padding(); got != 16 {
		t.Fatalf("breakpoints own the property: padding = %v, want 16", got)
	}
}

func TestResponsiveBaseAppliesAtBuild(t *testing.T) {
	tr := buildResponsive(t, `Window {
		width: { base: 480, lg: 900 }
	}`)
	if got := tr.Root.Width(); got != 480 {
		t.Fatalf("width at build = %v, want base 480", got)
	}
}
