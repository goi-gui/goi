// Package paint turns a laid-out tree into a flat display list. It never
// touches the GPU, so its output can be compared in tests.
package paint

import (
	"fmt"
	"strings"

	"github.com/ksckaan1/goui/internal/tree"
)

// Kind identifies a display list operation.
type Kind uint8

// Operation kinds.
const (
	// Fill is a rectangle, optionally rounded, with an optional border.
	Fill Kind = iota
	// Text is a run of glyphs.
	Text
)

// Op is one drawing operation in window coordinates (logical pixels).
type Op struct {
	Kind Kind

	// Rect is x, y, w, h.
	Rect [4]float32
	// Radius is the per-corner radius: top-left, top-right, bottom-right,
	// bottom-left.
	Radius [4]float32
	Color  uint32 // 0xRRGGBBAA

	BorderColor uint32
	BorderWidth float32

	// Clip is the intersection of every ancestor's content box, as x, y, w, h.
	// Ops outside it are still emitted; the renderer discards those fragments.
	Clip [4]float32

	// Text fields.
	Text     string
	FontSize float32
	Align    Align
	TextX    float32 // left edge of the alignment box
	TextY    float32 // baseline
}

// Align is the horizontal alignment of a text run inside its box.
type Align uint8

// Text alignments.
const (
	AlignStart Align = iota
	AlignCenter
	AlignEnd
)

// List is a display list. Reusing the same List across frames keeps the
// allocation count flat.
type List struct {
	Ops []Op
}

// Reset empties the list but keeps its capacity.
func (l *List) Reset() { l.Ops = l.Ops[:0] }

// Build fills l from tr. size is the client area in logical pixels.
func (l *List) Build(tr *tree.Tree, w, h float64) {
	l.Reset()
	clip := tree.Rect{W: w, H: h}
	l.walk(tr.Root.Base(), 0, 0, clip)
}

// walk emits the node and its children. x,y is the node's absolute origin.
func (l *List) walk(b *tree.NodeBase, x, y float64, clip tree.Rect) {
	if !b.Visible() {
		return
	}
	br := b.Bounds()
	x += br.X
	y += br.Y
	r := tree.Rect{X: x, Y: y, W: br.W, H: br.H}

	radius := float32(b.Radius())
	if c := b.Background(); c != 0 || b.BorderWidth() > 0 {
		l.Ops = append(l.Ops, Op{
			Kind:        Fill,
			Rect:        [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)},
			Radius:      [4]float32{radius, radius, radius, radius},
			Color:       c,
			BorderColor: b.BorderColor(),
			BorderWidth: float32(b.BorderWidth()),
			Clip:        clipArr(clip),
		})
	}

	switch n := b.Node().(type) {
	case *tree.Text:
		l.text(n, r, clip)
	case *tree.Button:
		l.textRun(n.Text.Get(), n.FontSize.Get(), n.Color.Get(), r, b.Padding(), true, clip)
	case *tree.TextInput:
		s := n.Text.Get()
		color := n.Color.Get()
		if s == "" {
			// Placeholder: dim the same color instead of introducing a second
			// color property for v1.
			s = n.Placeholder.Get()
			color = (color & 0xFFFFFF00) | 0x80
		}
		l.textRun(s, n.FontSize.Get(), color, r, b.Padding(), false, clip)
	}

	// Children clip to this node's box so nothing spills outside its parent.
	inner := tree.Rect{
		X: r.X + b.Padding() + b.BorderWidth(),
		Y: r.Y + b.Padding() + b.BorderWidth(),
		W: r.W - 2*(b.Padding()+b.BorderWidth()),
		H: r.H - 2*(b.Padding()+b.BorderWidth()),
	}
	if inner.W < 0 {
		inner.W = 0
	}
	if inner.H < 0 {
		inner.H = 0
	}
	childClip := intersect(clip, inner)
	for _, c := range b.Kids() {
		l.walk(c, x, y, childClip)
	}
}

func (l *List) text(n *tree.Text, r tree.Rect, clip tree.Rect) {
	align := AlignStart
	switch n.Align.Get() {
	case "center":
		align = AlignCenter
	case "end":
		align = AlignEnd
	}
	fs := n.FontSize.Get()
	// A text box taller than one line centers the line, which is what an
	// explicit height or a stretched parent looks like.
	baseline := r.Y + fs*0.8
	if lineH := fs * 1.2; r.H > lineH {
		baseline = r.Y + (r.H-lineH)/2 + fs*0.95
	}
	l.Ops = append(l.Ops, Op{
		Kind:     Text,
		Rect:     [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)},
		Color:    n.Color.Get(),
		Clip:     clipArr(clip),
		Text:     n.Text.Get(),
		FontSize: float32(fs),
		Align:    align,
		TextX:    float32(r.X),
		TextY:    float32(baseline),
	})
}

// textRun emits a label centered in r when centered, else left-aligned.
func (l *List) textRun(s string, fontSize float64, color uint32, r tree.Rect, pad float64, centered bool, clip tree.Rect) {
	if s == "" {
		return
	}
	align := AlignStart
	x := r.X + pad + 6
	if centered {
		align = AlignCenter
		x = r.X
	}
	l.Ops = append(l.Ops, Op{
		Kind:     Text,
		Rect:     [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)},
		Color:    color,
		Clip:     clipArr(clip),
		Text:     s,
		FontSize: float32(fontSize),
		Align:    align,
		TextX:    float32(x),
		TextY:    float32(r.Y + (r.H+fontSize*0.7)/2),
	})
}

// Dump renders the list as text, for golden tests.
func (l *List) Dump() string {
	var b strings.Builder
	for _, op := range l.Ops {
		switch op.Kind {
		case Fill:
			fmt.Fprintf(&b, "fill %g,%g %gx%g radius=%g color=#%08x border=#%08x/%g clip=%g,%g %gx%g\n",
				op.Rect[0], op.Rect[1], op.Rect[2], op.Rect[3], op.Radius[0], op.Color, op.BorderColor, op.BorderWidth,
				op.Clip[0], op.Clip[1], op.Clip[2], op.Clip[3])
		case Text:
			fmt.Fprintf(&b, "text %q at %g,%g size=%g align=%d color=#%08x clip=%g,%g %gx%g\n",
				op.Text, op.TextX, op.TextY, op.FontSize, op.Align, op.Color,
				op.Clip[0], op.Clip[1], op.Clip[2], op.Clip[3])
		}
	}
	return b.String()
}

func clipArr(r tree.Rect) [4]float32 {
	return [4]float32{float32(r.X), float32(r.Y), float32(r.W), float32(r.H)}
}

func intersect(a, b tree.Rect) tree.Rect {
	x0 := maxf(a.X, b.X)
	y0 := maxf(a.Y, b.Y)
	x1 := minf(a.Right(), b.Right())
	y1 := minf(a.Bottom(), b.Bottom())
	if x1 < x0 {
		x1 = x0
	}
	if y1 < y0 {
		y1 = y0
	}
	return tree.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
