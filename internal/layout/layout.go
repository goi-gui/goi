// Package layout computes a rectangle for every node. It is pure: no GPU, no
// windowing, no globals, so it runs in headless tests.
//
// The model is a small subset of flexbox: Column stacks vertically, Row lays out
// horizontally, Stack and Window overlay. Children may declare a fixed size, a
// flex grow factor, or nothing (auto). Material 3 components report their own
// intrinsic size through m3.MetricsFor.
package layout

import (
	"github.com/ksckaan1/goui/internal/m3"
	"github.com/ksckaan1/goui/internal/tree"
)

// Measurer supplies text sizes. Layout never shapes glyphs itself.
type Measurer interface {
	// Measure returns the size of text at the given font size, wrapping at
	// maxWidth (use a negative value for no wrapping).
	Measure(text string, fontSize, maxWidth float64) (w, h float64)
	// MeasureWeight is the weight-aware form used by Material components.
	MeasureWeight(text string, fontSize float64, weight int) (w, h float64)
}

// FakeMeasurer approximates text as half the font size per character and 1.2
// font sizes per line, so layout tests need no font files.
type FakeMeasurer struct{}

// Measure implements Measurer.
func (FakeMeasurer) Measure(text string, fontSize, maxWidth float64) (float64, float64) {
	return fakeMeasure(text, fontSize, maxWidth)
}

// MeasureWeight implements Measurer.
func (FakeMeasurer) MeasureWeight(text string, fontSize float64, weight int) (float64, float64) {
	return fakeMeasure(text, fontSize, -1)
}

func fakeMeasure(text string, fontSize, maxWidth float64) (float64, float64) {
	if text == "" {
		return 0, fontSize * 1.2
	}
	w := float64(len([]rune(text))) * fontSize * 0.55
	h := fontSize * 1.2
	// shortcut: FakeMeasurer is line-agnostic, so wrapped text gets a single-line
	// height. Real measuring (internal/text) handles wrapping.
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

// Layout arranges root inside a client area of width w and height h.
func (e *Engine) Layout(root *tree.Window, w, h float64) tree.Rect {
	r := tree.Rect{W: w, H: h}
	root.Base().SetBounds(r)
	// A window overlays its children, so a dialog can sit on top of content.
	e.stackChildren(root.Base(), w, h, 0, 0)
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

	// Material components declare their own content inset.
	if cv, ok := componentOf(b); ok {
		mt := m3.MetricsFor(b.Node().Kind(), cv, e.measure)
		insetL, insetT := inset+mt.Left, inset+mt.Top
		cw = maxf(w-insetL-inset-mt.Right, 0)
		ch = maxf(h-insetT-inset-mt.Bottom, 0)
		switch b.Node().Kind() {
		case "NavigationBar", "Tabs":
			e.equalRow(b, kids, cw, ch, insetL, insetT)
		case "NavigationRail", "NavigationDrawer":
			e.linear(b, kids, cw, ch, insetL, false, insetT)
		case "ButtonGroup", "Row", "Toolbar", "Carousel":
			e.linear(b, kids, cw, ch, insetL, true, insetT)
		default:
			e.linear(b, kids, cw, ch, insetL, false, insetT)
		}
		return
	}

	switch b.Node().Kind() {
	case "Row":
		e.linear(b, kids, cw, ch, inset, true, inset)
	case "Stack":
		e.stackChildren(b, w, h, inset, inset)
	default: // Window, Column
		e.linear(b, kids, cw, ch, inset, false, inset)
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
func (e *Engine) linear(b *tree.NodeBase, kids []*tree.NodeBase, availW, availH, offsetX float64, horizontal bool, offsetY float64) {
	c := containerOf(b)
	spacing := c.Spacing.Get()
	align := c.Align.Get()

	main, cross := availH, availW
	if horizontal {
		main, cross = availW, availH
	}

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
		}
		s = clampSize(s, minSize(k, horizontal), maxSize(k, horizontal))
		sizes[i] = s
		used += s
		totalFlex += k.Flex()
	}
	used += spacing * float64(len(kids)-1)

	// Grow flex children from their base size.
	if totalFlex > 0 && main > used {
		extra := (main - used) / totalFlex
		for i, k := range kids {
			if f := k.Flex(); f > 0 {
				sizes[i] = clampSize(sizes[i]+extra*f, minSize(k, horizontal), maxSize(k, horizontal))
			}
		}
	}

	pos := offsetY
	if horizontal {
		pos = offsetX
	}
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
		fill := false
		if cv, ok := componentOf(k); ok {
			mt := m3.MetricsFor(k.Node().Kind(), cv, e.measure)
			if horizontal && mt.FillH || !horizontal && mt.FillW {
				fill = true
			}
			if horizontal && mt.FillW {
				sz = availW
			}
			if !horizontal && mt.FillH {
				sz = availH
			}
		}
		if (a == "stretch" || isContainer(k) || fill) && declaredCross(k, horizontal) < 0 {
			cs = cross
		}
		cs = clampSize(cs, minSize(k, !horizontal), maxSize(k, !horizontal))
		crossPos := 0.0
		if horizontal {
			crossPos = offsetY + crossAlign(a, availH, cs)
		} else {
			crossPos = offsetX + crossAlign(a, availW, cs)
		}

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

// equalRow gives each child an equal share of the row, which is what
// navigation bars and tab bars do.
func (e *Engine) equalRow(b *tree.NodeBase, kids []*tree.NodeBase, availW, availH, offsetX, offsetY float64) {
	n := float64(len(kids))
	if n == 0 {
		return
	}
	w := availW / n
	for _, k := range kids {
		if v := k.Width(); v >= 0 {
			w = v
		}
		h := availH
		if v := k.Height(); v >= 0 {
			h = v
		}
		k.SetBounds(tree.Rect{X: offsetX, Y: offsetY, W: w, H: h})
		e.children(k, w, h)
		offsetX += w
	}
}

// stackChildren overlays every visible child, defaulting each to fill the
// parent. Window uses it so a dialog can cover the content.
func (e *Engine) stackChildren(b *tree.NodeBase, w, h, insetX, insetY float64) {
	align := "start"
	if c, ok := containerKind(b); ok {
		align = c.Align.Get()
	}
	for _, k := range b.Kids() {
		if !k.Visible() {
			continue
		}
		mw, mh := e.natural(k, w-2*insetX, h-2*insetY)
		cw, ch := mw, mh
		if v := k.Width(); v >= 0 {
			cw = v
		}
		if v := k.Height(); v >= 0 {
			ch = v
		}
		// Stack and Window overlay: an undeclared child fills the area, which
		// is what a background or a dialog expects.
		if k.Width() < 0 {
			cw = w - 2*insetX
		}
		if k.Height() < 0 {
			ch = h - 2*insetY
		}
		a := k.AlignSelf()
		if a == "" {
			a = align
		}
		m := k.Margin()
		r := tree.Rect{
			X: insetX + crossAlign(a, w-2*insetX, cw) + m,
			Y: insetY + crossAlign(a, h-2*insetY, ch) + m,
			W: cw,
			H: ch,
		}
		k.SetBounds(r)
		e.children(k, cw, ch)
	}
}

// natural returns the intrinsic size of a node.
func (e *Engine) natural(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	if cv, ok := componentOf(k); ok {
		mt := m3.MetricsFor(k.Node().Kind(), cv, e.measure)
		w, h := mt.W, mt.H
		if mt.FillW {
			w = availW
		}
		if mt.FillH {
			h = availH
		}
		// Components with children and no declared height hug their content.
		if h == 0 && len(k.Kids()) > 0 {
			h = e.contentHeight(k, availW, mt)
		}
		if w == 0 && len(k.Kids()) > 0 {
			w = e.contentWidth(k, availW, h, mt)
		}
		return w, h
	}
	switch n := k.Node().(type) {
	case *tree.Text:
		return e.textSize(n.Text.Get(), n)
	}
	if k.Node().Kind() == "Window" {
		return availW, availH
	}
	return e.containerNatural(k, availW, availH)
}

// flowHorizontal reports containers whose children run left to right.
func flowHorizontal(kind string) bool {
	switch kind {
	case "Row", "ButtonGroup", "Toolbar", "Carousel", "Tabs", "NavigationBar":
		return true
	}
	return false
}

// contentHeight is the intrinsic height: the sum for a column, the tallest
// child for a row.
func (e *Engine) contentHeight(k *tree.NodeBase, availW float64, mt m3.Metrics) float64 {
	spacing := containerSpacing(k)
	total := mt.Top + mt.Bottom
	horizontal := flowHorizontal(k.Node().Kind())
	first := true
	for _, c := range visibleChildren(k) {
		_, h := e.natural(c, availW-mt.Left-mt.Right, 0)
		if v := c.Height(); v >= 0 {
			h = v
		}
		if horizontal {
			total = maxf(total, h+mt.Top+mt.Bottom)
			continue
		}
		if !first {
			total += spacing
		}
		total += h
		first = false
	}
	return total
}

// contentWidth is the intrinsic width: the widest child for a column, the sum
// for a row.
func (e *Engine) contentWidth(k *tree.NodeBase, availW, h float64, mt m3.Metrics) float64 {
	spacing := containerSpacing(k)
	total := mt.Left + mt.Right
	horizontal := flowHorizontal(k.Node().Kind())
	first := true
	for _, c := range visibleChildren(k) {
		w, _ := e.natural(c, availW-mt.Left-mt.Right, h)
		if v := c.Width(); v >= 0 {
			w = v
		}
		if !horizontal {
			total = maxf(total, w+mt.Left+mt.Right)
			continue
		}
		if !first {
			total += spacing
		}
		total += w
		first = false
	}
	return total
}

// containerSpacing reads spacing from a component or a Container.
func containerSpacing(k *tree.NodeBase) float64 {
	if c, ok := componentOf(k); ok {
		return c.Spacing
	}
	return containerOf(k).Spacing.Get()
}

// textSize measures a Text node, honouring fontSize, role and wrap.
func (e *Engine) textSize(s string, t *tree.Text) (float64, float64) {
	size := t.FontSize.Get()
	weight := int(t.Weight.Get())
	if size <= 0 {
		// Role-based size: the metrics helper knows the type scale.
		role := t.Role.Get()
		size, weight = roleSize(role, weight)
	}
	if t.Wrap.Get() {
		return e.Measurer.Measure(s, size, -1)
	}
	return e.Measurer.MeasureWeight(s, size, weight)
}

func (e *Engine) measure(s string, size float64, weight int) (float64, float64) {
	return e.Measurer.MeasureWeight(s, size, weight)
}

// containerOf returns the Container embedded in a Column/Row/Stack, or an empty
// one so callers never need a nil check.
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

// containerKind reports whether the node has a Container with alignment.
func containerKind(b *tree.NodeBase) (*tree.Container, bool) {
	switch n := b.Node().(type) {
	case *tree.Column:
		return &n.Container, true
	case *tree.Row:
		return &n.Container, true
	case *tree.Stack:
		return &n.Container, true
	}
	return nil, false
}

// componentOf returns the Material component state behind a node.
func componentOf(b *tree.NodeBase) (*m3.ComponentView, bool) {
	type m3er interface{ M3() *tree.Component }
	m, ok := b.Node().(m3er)
	if !ok {
		return nil, false
	}
	c := m.M3()
	return &m3.ComponentView{
		Label:       c.Label.Get(),
		Supporting:  c.Supporting.Get(),
		Text:        c.Text.Get(),
		Placeholder: c.Placeholder.Get(),
		Icon:        c.Icon.Get(),
		Trailing:    c.Trailing.Get(),
		Variant:     c.Variant.Get(),
		Size:        c.Size.Get(),
		Disabled:    c.Disabled.Get(),
		Checked:     c.Checked.Get(),
		Selected:    c.Selected.Get(),
		Expanded:    c.Expanded.Get(),
		Value:       c.Value.Get(),
		Min:         c.Min.Get(),
		Max:         c.Max.Get(),
		Step:        c.Step.Get(),
	}, true
}

// containerNatural is the auto size of Column, Row and Stack.
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
	case "Window", "Column", "Row", "Stack",
		"Card", "Dialog", "BottomSheet", "SideSheet", "List", "Menu",
		"NavigationDrawer", "NavigationBar", "NavigationRail", "Tabs",
		"Toolbar", "ButtonGroup", "Carousel":
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

// roleSize returns the pixel size and weight of an M3 type role.
func roleSize(role string, weight int) (float64, int) {
	t := m3.Light().Type
	var st m3.TextStyle
	switch role {
	case "displayLarge":
		st = t.DisplayLarge
	case "displayMedium":
		st = t.DisplayMedium
	case "displaySmall":
		st = t.DisplaySmall
	case "headlineLarge":
		st = t.HeadlineLarge
	case "headlineMedium":
		st = t.HeadlineMedium
	case "headlineSmall":
		st = t.HeadlineSmall
	case "titleLarge":
		st = t.TitleLarge
	case "titleMedium":
		st = t.TitleMedium
	case "titleSmall":
		st = t.TitleSmall
	case "bodyLarge":
		st = t.BodyLarge
	case "bodySmall":
		st = t.BodySmall
	case "labelLarge":
		st = t.LabelLarge
	case "labelMedium":
		st = t.LabelMedium
	case "labelSmall":
		st = t.LabelSmall
	default:
		st = t.BodyMedium
	}
	w := st.Weight
	if weight > 0 {
		w = weight
	}
	return st.Size, w
}
