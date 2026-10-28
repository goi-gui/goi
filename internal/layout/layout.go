// Package layout computes a rectangle for every node. It is pure: no GPU, no
// windowing, no globals, so it runs in headless tests.
//
// The model is a small subset of flexbox: Column stacks vertically, Row lays out
// horizontally, Stack overlays. Children may declare a fixed size, a flex grow
// factor, or nothing (auto). Text is measured through a Measurer.
package layout

import "github.com/ksckaan1/goui/internal/tree"

// Measurer supplies text sizes. Layout never shapes glyphs itself.
type Measurer interface {
	// Measure returns the size of text at the given font size, wrapping at
	// maxWidth (use a negative value for no wrapping).
	Measure(text string, fontSize, maxWidth float64) (w, h float64)
}

// FakeMeasurer approximates text as half the font size per byte and 1.2 font
// sizes per line, so layout tests need no font files.
type FakeMeasurer struct{}

// Measure implements Measurer.
func (FakeMeasurer) Measure(text string, fontSize, maxWidth float64) (float64, float64) {
	if text == "" {
		return 0, fontSize * 1.2
	}
	w := float64(len(text)) * fontSize * 0.5
	h := fontSize * 1.2
	// shortcut: no real line breaking; wrapping only clamps the width, so the
	// height is right for a single line. The text package replaces this.
	if maxWidth > 0 && w > maxWidth {
		w = maxWidth
	}
	return w, h
}

// Engine runs layout for one tree with one measurer.
type Engine struct {
	Measurer Measurer
}

// New returns an engine; a nil measurer falls back to FakeMeasurer.
func New(m Measurer) *Engine {
	if m == nil {
		m = FakeMeasurer{}
	}
	return &Engine{Measurer: m}
}

// Layout arranges root inside a client area of width w and height h (logical
// pixels).
func (e *Engine) Layout(root *tree.Window, w, h float64) tree.Rect {
	r := tree.Rect{W: w, H: h}
	root.Base().SetBounds(r)
	e.children(root.Base(), w, h)
	return r
}

// children sizes and positions the visible children of b. w and h are b's outer
// size; b's own padding and border are applied here, in one place.
func (e *Engine) children(b *tree.NodeBase, w, h float64) {
	kids := visibleChildren(b)
	if len(kids) == 0 {
		return
	}
	inset := b.Padding() + b.BorderWidth()
	cw, ch := maxf(w-2*inset, 0), maxf(h-2*inset, 0)
	switch b.Node().Kind() {
	case "Row":
		e.linear(b, kids, cw, ch, inset, true)
	case "Stack":
		e.stack(b, kids, cw, ch, inset)
	default: // Window, Column
		e.linear(b, kids, cw, ch, inset, false)
	}
}

func visibleChildren(b *tree.NodeBase) []*tree.NodeBase {
	all := b.Kids()
	out := make([]*tree.NodeBase, 0, len(all))
	for _, k := range all {
		if k.Visible() {
			out = append(out, k)
		}
	}
	return out
}

// linear is the Column (vertical) and Row (horizontal) algorithm.
func (e *Engine) linear(b *tree.NodeBase, kids []*tree.NodeBase, availW, availH, inset float64, horizontal bool) {
	c := containerOf(b)
	spacing := c.Spacing.Get()
	align := c.Align.Get()

	main, cross := availH, availW
	if horizontal {
		main, cross = availW, availH
	}

	// Pass 1: base size along the main axis and the total flex factor.
	sizes := make([]float64, len(kids))
	var totalFlex, used float64
	for i, k := range kids {
		mw, mh := e.natural(k, availW, availH)
		s := mw
		if !horizontal {
			s = mh
		}
		if declared := declaredMain(k, horizontal); declared >= 0 {
			s = declared
		} else if b.Node().Kind() == "Window" {
			// The root's direct children fill the client area, so flex: 1 has
			// space to distribute.
			s = main
		}
		s = clampSize(s, minSize(k, horizontal), maxSize(k, horizontal))
		sizes[i] = s
		used += s
		totalFlex += k.Flex()
	}
	used += spacing * float64(len(kids)-1)

	// Pass 2: share the leftover main-axis space between flex children,
	// growing each from its base size.
	if totalFlex > 0 && main > used {
		extra := (main - used) / totalFlex
		for i, k := range kids {
			if f := k.Flex(); f > 0 {
				sizes[i] = clampSize(sizes[i]+extra*f, minSize(k, horizontal), maxSize(k, horizontal))
			}
		}
	}

	pos := inset
	for i, k := range kids {
		sz := sizes[i]

		mw, mh := e.natural(k, availW, availH)
		cs := mw
		if horizontal {
			cs = mh
		}
		if declared := declaredCross(k, horizontal); declared >= 0 {
			cs = declared
		}
		a := k.AlignSelf()
		if a == "" {
			a = align
		}
		// Containers and stretch fill the cross axis; boxes take their natural
		// size unless the parent says otherwise.
		if (a == "stretch" || isContainer(k)) && declaredCross(k, horizontal) < 0 {
			cs = cross
		}
		cs = clampSize(cs, minSize(k, !horizontal), maxSize(k, !horizontal))
		crossPos := inset + crossAlign(a, cross, cs)

		m := k.Margin()
		var r tree.Rect
		if horizontal {
			r = tree.Rect{X: pos + m, Y: crossPos + m, W: sz, H: cs}
			pos += sz + spacing
		} else {
			r = tree.Rect{X: crossPos + m, Y: pos + m, W: cs, H: sz}
			pos += sz + spacing
		}
		k.SetBounds(r)
		e.children(k, r.W, r.H)
	}
}

// stack overlays every visible child, defaulting each to fill the parent.
func (e *Engine) stack(b *tree.NodeBase, kids []*tree.NodeBase, availW, availH, inset float64) {
	align := containerOf(b).Align.Get()
	for _, k := range kids {
		mw, mh := e.natural(k, availW, availH)
		w, h := mw, mh
		if v := k.Width(); v >= 0 {
			w = v
		}
		if v := k.Height(); v >= 0 {
			h = v
		}
		if k.Width() < 0 && k.Height() < 0 {
			w, h = availW, availH // auto children of a stack fill it
		}
		a := k.AlignSelf()
		if a == "" {
			a = align
		}
		m := k.Margin()
		r := tree.Rect{
			X: inset + crossAlign(a, availW, w) + m,
			Y: inset + crossAlign(a, availH, h) + m,
			W: w,
			H: h,
		}
		k.SetBounds(r)
		e.children(k, w, h)
	}
}

// natural returns the size a node takes when neither dimension is declared.
func (e *Engine) natural(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	switch n := k.Node().(type) {
	case *tree.Text:
		return e.Measurer.Measure(n.Text.Get(), n.FontSize.Get(), wrapWidth(n, availW))
	case *tree.Button:
		w, h := e.Measurer.Measure(n.Text.Get(), n.FontSize.Get(), -1)
		return w + 24 + k.Padding()*2, h + 12 + k.Padding()*2
	case *tree.TextInput:
		w, h := e.Measurer.Measure(n.Placeholder.Get(), n.FontSize.Get(), -1)
		return maxf(w+24+k.Padding()*2, 120), h + 12 + k.Padding()*2
	}
	if k.Node().Kind() == "Window" {
		return availW, availH
	}
	return e.containerNatural(k, availW, availH)
}

// containerNatural is the auto size of Column, Row and Stack: the extent of
// their children plus spacing, padding and borders.
func (e *Engine) containerNatural(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	kids := visibleChildren(k)
	spacing := containerOf(k).Spacing.Get()
	inset := (k.Padding() + k.BorderWidth()) * 2
	kind := k.Node().Kind()

	var mainSum, maxW, maxH float64
	for i, c := range kids {
		cw, ch := e.natural(c, availW, availH)
		if v := c.Width(); v >= 0 {
			cw = v
		}
		if v := c.Height(); v >= 0 {
			ch = v
		}
		switch kind {
		case "Row":
			mainSum += cw
		case "Stack":
			mainSum = maxf(mainSum, cw)
		default:
			mainSum += ch
		}
		maxW = maxf(maxW, cw)
		maxH = maxf(maxH, ch)
		if i > 0 && kind != "Stack" {
			mainSum += spacing
		}
	}
	if kind == "Row" {
		return mainSum + inset, maxH + inset
	}
	if kind == "Stack" {
		return maxf(maxW, mainSum) + inset, maxH + inset
	}
	return maxW + inset, mainSum + inset
}

func isContainer(k *tree.NodeBase) bool {
	switch k.Node().Kind() {
	case "Window", "Column", "Row", "Stack":
		return true
	}
	return false
}

func declaredMain(k *tree.NodeBase, horizontal bool) float64 {
	if horizontal {
		return k.Width()
	}
	return k.Height()
}

func declaredCross(k *tree.NodeBase, horizontal bool) float64 {
	if horizontal {
		return k.Height()
	}
	return k.Width()
}

// wrapWidth is the width a text may occupy.
func wrapWidth(t *tree.Text, availW float64) float64 {
	if !t.Wrap.Get() {
		return -1
	}
	return availW - t.Padding()*2
}

func containerOf(b *tree.NodeBase) *tree.Container {
	switch n := b.Node().(type) {
	case *tree.Column:
		return &n.Container
	case *tree.Row:
		return &n.Container
	case *tree.Stack:
		return &n.Container
	}
	return &tree.Container{}
}

func crossAlign(a string, avail, size float64) float64 {
	switch a {
	case "center":
		return (avail - size) / 2
	case "end":
		return avail - size
	default: // start, stretch
		return 0
	}
}

func minSize(b *tree.NodeBase, horizontal bool) float64 {
	if horizontal {
		return b.MinWidth()
	}
	return 0
}

func maxSize(b *tree.NodeBase, horizontal bool) float64 {
	if horizontal {
		if m := b.MaxWidth(); m >= 0 {
			return m
		}
	}
	return 1e9
}

func clampSize(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
