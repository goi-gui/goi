package paint

import (
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// richList lays out and paints a RichText with the given runs, returning the
// list and the node so a test can check both the ops and the reserved box.
func richList(t *testing.T, spans []tree.Span, w, h float64) (*List, *tree.RichText) {
	t.Helper()
	tr, diag := tree.BuildSource([]byte(`Window {
		Column {
			Rectangle { width: 10 height: 10 }
			RichText { id: t width: 120 wrap: true }
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	n, ok := tr.ByID("t")
	if !ok {
		t.Fatal("no RichText")
	}
	rt := n.(*tree.RichText)
	rt.SetSpans(spans)
	layout.New(nil).Layout(tr.Root, 200, 300)
	l := &List{}
	l.Build(tr, w, h)
	return l, rt
}

// runs finds the drawn runs by their text, so a test names what it checks.
func runs(l *List, want string) []Op {
	var out []Op
	for _, op := range l.Ops {
		if op.Kind == Text && op.Text == want {
			out = append(out, op)
		}
	}
	return out
}

// TestRichTextStylesRuns checks that each styling reaches the drawing op: a
// bold run is heavier, a slanted one is italic, a link takes the primary
// colour and a code run gets a tinted background.
func TestRichTextStylesRuns(t *testing.T) {
	l, _ := richList(t, []tree.Span{
		{Text: "plain "},
		{Text: "bold", Bold: true},
		{Text: " "},
		{Text: "slanted", Italic: true},
		{Text: " "},
		{Text: "code", Code: true},
		{Text: " "},
		{Text: "link", Link: "https://goi.dev"},
	}, 200, 300)

	plain := runs(l, "plain ")
	loud := runs(l, "bold")
	if len(plain) != 1 || len(loud) != 1 {
		t.Fatalf("plain=%d bold=%d\n%s", len(plain), len(loud), l.Dump())
	}
	if loud[0].Weight <= plain[0].Weight {
		t.Fatalf("bold weight %v, plain %v", loud[0].Weight, plain[0].Weight)
	}
	if got := runs(l, "slanted"); len(got) != 1 || !got[0].Italic {
		t.Fatalf("slanted run not italic: %+v", got)
	}
	link := runs(l, "link")
	if len(link) != 1 || link[0].Color != m3.Light().Colors.Primary {
		t.Fatalf("link run = %+v", link)
	}
	// The code tint is a fill directly under the run: it has to be there, or an
	// inline code span reads as ordinary text.
	code := runs(l, "code")
	if len(code) != 1 {
		t.Fatalf("code run = %+v", code)
	}
	found := false
	for _, op := range l.Ops {
		if op.Kind == Fill && op.Color == m3.Light().Colors.SurfaceContainerHighest {
			found = true
		}
	}
	if !found {
		t.Fatalf("no code background:\n%s", l.Dump())
	}
}

// TestRichTextHeightMatchesWrap is the layout/paint agreement guard: the box the
// layout reserved is exactly as tall as the lines the painter drew, so text can
// never spill into the next block.
func TestRichTextHeightMatchesWrap(t *testing.T) {
	spans := []tree.Span{
		{Text: "a greedy wrap with one "},
		{Text: "bold", Bold: true},
		{Text: " word in the middle so the runs really do break across lines"},
	}
	l, rt := richList(t, spans, 200, 300)
	st := m3.RoleStyle("bodyMedium")
	lines := text.WrapInline(rt.Inlines(), 120, func(style int, s string) float64 {
		// Same estimate the painter falls back to with no measurer.
		return float64(len([]rune(s))) * st.Size * 0.55
	})
	if len(lines) < 2 {
		t.Fatalf("the sample did not wrap: %d line(s)", len(lines))
	}
	want := float64(len(lines)) * st.LineHeight
	if got := rt.Base().Bounds().H; got != want {
		t.Fatalf("bounds height = %v, want %v for %d lines", got, want, len(lines))
	}
	// Every drawn run sits inside the box: a wrap that disagrees would push a
	// baseline past the bottom.
	bottom := rt.Base().Bounds().Y + rt.Base().Bounds().H
	for _, op := range l.Ops {
		if op.Kind == Text && op.TextY > float32(bottom)+1 {
			t.Fatalf("run %q drawn at y=%v past the box bottom %v", op.Text, op.TextY, bottom)
		}
	}
}
