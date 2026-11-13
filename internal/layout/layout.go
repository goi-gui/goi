// Package layout computes a rectangle for every node. It is pure: no GPU, no
// windowing, no globals, so it runs in headless tests.
//
// The model is a small subset of flexbox: Column stacks vertically, Row lays out
// horizontally, Stack and Window overlay. Children may declare a fixed size, a
// flex grow factor, or nothing (auto). Material 3 components report their own
// intrinsic size through m3.MetricsFor.
package layout

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// Measurer supplies text sizes. Layout never shapes glyphs itself.
type Measurer interface {
	// Measure returns the size of text at the given font size, wrapping at
	// maxWidth (use a negative value for no wrapping).
	Measure(text string, fontSize, maxWidth float64) (w, h float64)
	// MeasureWeight is the weight-aware form used by Material components.
	MeasureWeight(text string, fontSize float64, weight int) (w, h float64)
}

// FamilyMeasurer is a Measurer that also knows the app's font families. A
// measurer that does not implement it still works: layout then measures every
// run in the built-in face, which is what `fontFamily` never set means.
type FamilyMeasurer interface {
	MeasureFamily(text string, fontSize float64, weight int, family string) (w, h float64)
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
	// view is scratch for componentOf, so reading a component's properties
	// during a pass does not allocate one view per call.
	view m3.ComponentView
	// metrics caches each component's M3 metrics for the pass. A component is
	// measured once while its parent asks for a natural size, again while it is
	// laid out, and once more for the fill flags; the answer depends only on its
	// properties and the measurer, both constant in a pass.
	metrics map[*tree.NodeBase]m3.Metrics
	// memo caches intrinsic sizes for the current pass. A parent asks a child
	// for its natural size while measuring itself and again while sizing it, and
	// each ask recurses into the whole subtree, so without this a deep tree is
	// measured once per level. Cleared at the start of every Layout.
	memo map[naturalKey]naturalSize
	// wraps caches how many lines a run wraps to, which is all layout needs from
	// a paragraph. A resize lays out every frame, and re-walking every word of
	// every visible paragraph is otherwise the most expensive part of it.
	wraps map[wrapKey]int
}

// wrapKey identifies a wrapped run: text, column, size and weight. The break
// points depend on nothing else, so a hit is exact.
type wrapKey struct {
	s      string
	width  float64
	size   float64
	weight int
	family string
}

// maxWrapCounts bounds the wrap cache; past it the map is dropped and the
// visible runs wrap once more.
const maxWrapCounts = 1024

// naturalKey is one intrinsic-size question: which node, offered how much room.
type naturalKey struct {
	node           *tree.NodeBase
	availW, availH float64
}

type naturalSize struct{ w, h float64 }

// New returns an engine; a nil measurer falls back to FakeMeasurer.
func New(m Measurer) *Engine {
	if m == nil {
		m = FakeMeasurer{}
	}
	return &Engine{Measurer: m}
}

// Layout arranges root inside a client area of width w and height h.
func (e *Engine) Layout(root *tree.Window, w, h float64) tree.Rect {
	// Intrinsic sizes are per pass: a pass is where a property change lands, so
	// nothing from the previous one can still be true.
	clear(e.memo)
	clear(e.metrics)
	root.Base().SetViewport(w, h)
	root.Base().ApplyExpressions()
	r := tree.Rect{W: w, H: h}
	root.Base().SetBounds(r)
	// A window overlays its children, so a dialog can sit on top of content.
	e.stackChildren(root.Base(), w, h, 0, 0, 0, 0)
	return r
}

// children sizes and positions the visible children of b. w and h are b's outer
// size; b's own padding and border are applied here, in one place.
func (e *Engine) children(b *tree.NodeBase, w, h float64) {
	kids := visibleChildren(b)
	// A table places its own header and rows; the generic flow would treat a
	// column as a child box.
	if b.Node().Kind() == "Table" {
		e.tableChildren(b, w, h)
		return
	}
	// A cell's content is one aligned run inside its inset box.
	if b.Node().Kind() == "TableCell" {
		e.cellChildren(b, visibleChildren(b), w, h)
		return
	}
	// A FAB menu stacks its actions above the FAB, and has none while closed;
	// it must still visit an empty child list, so a menu that just closed can
	// clear the bounds its actions had.
	if b.Node().Kind() == "FABMenu" {
		e.fabMenuChildren(b, kids, w, h)
		return
	}
	if len(kids) == 0 {
		return
	}
	pe, be := b.PaddingEdges(), b.BorderEdges()
	cw := maxf(w-pe.Left-pe.Right-be.Left-be.Right, 0)
	ch := maxf(h-pe.Top-pe.Bottom-be.Top-be.Bottom, 0)

	// Material components declare their own content inset.
	if cv, ok := e.componentOf(b); ok {
		mt := e.metricsFor(b, cv)
		insetL, insetT := pe.Left+be.Left+mt.Left, pe.Top+be.Top+mt.Top
		cw = maxf(w-insetL-pe.Right-be.Right-mt.Right, 0)
		ch = maxf(h-insetT-pe.Bottom-be.Bottom-mt.Bottom, 0)
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
	case "Row", "TitleBar":
		e.linear(b, kids, cw, ch, pe.Left+be.Left, true, pe.Top+be.Top)
	case "Stack", "ClipRRect", "Positioned":
		e.stackChildren(b, w, h, pe.Left+be.Left, pe.Top+be.Top, pe.Right+be.Right, pe.Bottom+be.Bottom)
	case "Wrap":
		e.wrap(b, kids, cw, ch, pe.Left+be.Left, pe.Top+be.Top)
	case "Grid":
		e.grid(b, kids, cw, ch, pe.Left+be.Left, pe.Top+be.Top)
	default: // Window, Column
		e.linear(b, kids, cw, ch, pe.Left+be.Left, false, pe.Top+be.Top)
	}
}

func visibleChildren(b *tree.NodeBase) []*tree.NodeBase {
	all := b.Kids()
	for _, k := range all {
		if !k.Visible() {
			out := make([]*tree.NodeBase, 0, len(all))
			for _, k := range all {
				if k.Visible() {
					out = append(out, k)
				}
			}
			return out
		}
	}
	// Nothing hidden is the common case, and the children slice is read-only
	// here, so returning it skips an allocation on every container.
	return all
}

// linear is the Column (vertical) and Row (horizontal) algorithm.
func (e *Engine) linear(b *tree.NodeBase, kids []*tree.NodeBase, availW, availH, offsetX float64, horizontal bool, offsetY float64) {
	spacing := e.containerSpacing(b)
	align := e.containerCrossAlign(b)
	mainAlign := e.containerMainAlign(b)

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
		me := k.MarginEdges()
		if horizontal {
			used += s + me.Left + me.Right
		} else {
			used += s + me.Top + me.Bottom
		}
		totalFlex += k.Flex()
	}
	used += spacing * float64(len(kids)-1)

	// Flex children take the leftover space, and give space back when the
	// content overflows, so a flex scroll container stays bounded by its parent.
	if totalFlex > 0 {
		delta := (main - used) / totalFlex
		for i, k := range kids {
			if f := k.Flex(); f > 0 {
				sizes[i] = clampSize(sizes[i]+delta*f, minSize(k, horizontal), maxSize(k, horizontal))
			}
		}
	}

	// The leftover along the main axis is what mainAxisAlign distributes: a
	// flex child already took it, and an overflowing scroller has none.
	var occupied float64
	for i, k := range kids {
		me := k.MarginEdges()
		if horizontal {
			occupied += sizes[i] + me.Left + me.Right
		} else {
			occupied += sizes[i] + me.Top + me.Bottom
		}
	}
	occupied += spacing * float64(len(kids)-1)
	lead, extra := distribute(mainAlign, main-occupied, len(kids))

	pos := offsetY + lead
	if horizontal {
		pos = offsetX + lead
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
		if cv, ok := e.componentOf(k); ok {
			mt := e.metricsFor(k, cv)
			if horizontal && mt.FillH || !horizontal && mt.FillW {
				fill = true
			}
			// A flex child already took its share of the row, so a component
			// that fills must not be reset to the whole row: that is what a
			// chart or a list with `flex: 1` in a Row asks for.
			if k.Flex() <= 0 && declaredMain(k, horizontal) < 0 {
				if horizontal && mt.FillW {
					sz = availW
				}
				if !horizontal && mt.FillH {
					sz = availH
				}
			}
		}
		if (a == "stretch" || isContainer(k) || fill) && declaredCross(k, horizontal) < 0 {
			cs = cross
		}
		cs = clampSize(cs, minSize(k, !horizontal), maxSize(k, !horizontal))
		// A Wrap or Grid wraps to the width it actually gets, which may be a
		// declared one rather than the parent's, so its height follows cs.
		if !horizontal && k.Height() < 0 && (k.Node().Kind() == "Wrap" || k.Node().Kind() == "Grid") {
			pe, be := k.PaddingEdges(), k.BorderEdges()
			inner := maxf(cs-pe.Left-pe.Right-be.Left-be.Right, 0)
			insetH := pe.Top + pe.Bottom + be.Top + be.Bottom
			if k.Node().Kind() == "Wrap" {
				sz = e.wrapHeight(visibleChildren(k), inner, containerOf(k).Spacing.Get()) + insetH
			} else {
				sz = e.gridHeight(visibleChildren(k), inner, gridColumns(k), containerOf(k).Spacing.Get()) + insetH
			}
		}
		crossPos := 0.0
		if horizontal {
			crossPos = offsetY + crossAlign(a, availH, cs)
		} else {
			crossPos = offsetX + crossAlign(a, availW, cs)
		}

		me := k.MarginEdges()
		var r tree.Rect
		if horizontal {
			r = tree.Rect{X: pos + me.Left, Y: crossPos + me.Top, W: sz, H: cs}
			pos += me.Left + sz + me.Right + spacing + extra
		} else {
			r = tree.Rect{X: crossPos + me.Left, Y: pos + me.Top, W: cs, H: sz}
			pos += me.Top + sz + me.Bottom + spacing + extra
		}
		k.SetBounds(r)
		e.children(k, r.W, r.H)
	}
}

// wrap flows children left to right and starts a new run when the next child
// would cross the right edge. spacing separates children in a run and runs from
// each other; align places shorter children inside their run.
func (e *Engine) wrap(b *tree.NodeBase, kids []*tree.NodeBase, availW, availH, offsetX, offsetY float64) {
	c := containerOf(b)
	spacing := c.Spacing.Get()
	align := e.containerCrossAlign(b)
	mainAlign := e.containerMainAlign(b)

	type item struct {
		k        *tree.NodeBase
		x, w, h  float64
		run, idx int
	}
	items := make([]item, 0, len(kids))
	// runs holds each run's height, runW its width and runN how many children
	// it took, which is what the main-axis distribution needs.
	var runs, runW []float64
	var runN []int
	x := 0.0
	for _, k := range kids {
		mw, mh := e.natural(k, availW, availH)
		if v := k.Width(); v >= 0 {
			mw = v
		}
		if v := k.Height(); v >= 0 {
			mh = v
		}
		if mw > availW {
			mw = availW
		}
		if x > 0 && x+mw > availW {
			x = 0
		}
		if x == 0 {
			runs = append(runs, 0)
			runW = append(runW, 0)
			runN = append(runN, 0)
		}
		run := len(runs) - 1
		if mh > runs[run] {
			runs[run] = mh
		}
		runW[run] = x + mw
		items = append(items, item{k: k, x: x, w: mw, h: mh, run: run, idx: runN[run]})
		runN[run]++
		x += mw + spacing
	}

	y := offsetY
	runY := make([]float64, len(runs))
	for i, h := range runs {
		runY[i] = y
		y += h + spacing
	}
	for _, it := range items {
		a := it.k.AlignSelf()
		if a == "" {
			a = align
		}
		h := it.h
		if a == "stretch" && it.k.Height() < 0 {
			h = runs[it.run]
		}
		// A run is usually narrower than the row, so mainAxisAlign can centre
		// it or spread it out, the same set a Row offers.
		lead, extra := distribute(mainAlign, availW-runW[it.run], runN[it.run])
		me := it.k.MarginEdges()
		yy := runY[it.run] + crossAlign(a, runs[it.run], h)
		xx := offsetX + lead + it.x + float64(it.idx)*extra + me.Left
		it.k.SetBounds(tree.Rect{X: xx, Y: yy + me.Top, W: it.w, H: h})
		e.children(it.k, it.w, h)
	}
}

// grid places children in equal-width columns; each row is as tall as its
// tallest child. A container child fills the cell, a sized one is aligned in it.
func (e *Engine) grid(b *tree.NodeBase, kids []*tree.NodeBase, availW, availH, offsetX, offsetY float64) {
	c := containerOf(b)
	cols := gridColumns(b)
	spacing := c.Spacing.Get()
	align := e.containerCrossAlign(b)
	mainAlign := e.containerMainAlign(b)
	cellW := (availW - spacing*float64(cols-1)) / float64(cols)
	if cellW < 0 {
		cellW = 0
	}

	n := len(kids)
	widths := make([]float64, n)
	heights := make([]float64, n)
	rowH := make([]float64, (n+cols-1)/cols)
	for i, k := range kids {
		_, mh := e.natural(k, cellW, availH)
		// A cell is tight in width, like GridView; a declared width is aligned
		// inside the cell instead.
		mw := cellW
		if v := k.Width(); v >= 0 {
			mw = minf(v, cellW)
		}
		if v := k.Height(); v >= 0 {
			mh = v
		}
		widths[i], heights[i] = mw, mh
		if r := i / cols; mh > rowH[r] {
			rowH[r] = mh
		}
	}
	// A grid's main axis runs down: the leftover vertical space can centre the
	// block of rows or spread the rows apart.
	var gridH float64
	for _, h := range rowH {
		gridH += h + spacing
	}
	if len(rowH) > 0 {
		gridH -= spacing
	}
	lead, extra := distribute(mainAlign, availH-gridH, len(rowH))
	y := offsetY + lead
	for i, k := range kids {
		r, col := i/cols, i%cols
		a := k.AlignSelf()
		if a == "" {
			a = align
		}
		h := heights[i]
		if a == "stretch" && k.Height() < 0 {
			h = rowH[r]
		}
		me := k.MarginEdges()
		x := offsetX + float64(col)*(cellW+spacing) + crossAlign(a, cellW, widths[i])
		k.SetBounds(tree.Rect{X: x + me.Left, Y: y + me.Top, W: widths[i], H: h})
		e.children(k, widths[i], h)
		if col == cols-1 || i == n-1 {
			y += rowH[r] + spacing + extra
		}
	}
}

// gridColumns reads the column count, defaulting to one for a bad value.
func gridColumns(b *tree.NodeBase) int {
	g, ok := b.Node().(*tree.Grid)
	if !ok {
		return 1
	}
	if n := int(g.Columns.Get()); n > 0 {
		return n
	}
	return 1
}

// wrapHeight is the height of a wrapped run of children at the given width.
func (e *Engine) wrapHeight(kids []*tree.NodeBase, availW, spacing float64) float64 {
	x, total, run := 0.0, 0.0, 0.0
	for _, k := range kids {
		mw, mh := e.natural(k, availW, 0)
		if v := k.Width(); v >= 0 {
			mw = v
		}
		if v := k.Height(); v >= 0 {
			mh = v
		}
		mw = minf(mw, availW)
		if x > 0 && x+mw > availW {
			total += run + spacing
			x, run = 0, 0
		}
		run = maxf(run, mh)
		x += mw + spacing
	}
	if len(kids) > 0 {
		total += run
	}
	return total
}

// gridHeight is the total height of the grid rows at the given width.
func (e *Engine) gridHeight(kids []*tree.NodeBase, availW float64, cols int, spacing float64) float64 {
	cellW := (availW - spacing*float64(cols-1)) / float64(cols)
	if cellW < 0 {
		cellW = 0
	}
	total := 0.0
	for i := 0; i < len(kids); i += cols {
		rowH := 0.0
		for j := i; j < i+cols && j < len(kids); j++ {
			_, mh := e.natural(kids[j], cellW, 0)
			if v := kids[j].Height(); v >= 0 {
				mh = v
			}
			rowH = maxf(rowH, mh)
		}
		if i > 0 {
			total += spacing
		}
		total += rowH
	}
	return total
}

// gridNaturalWidth is the sum of the widest child in each column.
func (e *Engine) gridNaturalWidth(kids []*tree.NodeBase, availW float64, cols int, spacing float64) float64 {
	colW := make([]float64, cols)
	for i, k := range kids {
		w, _ := e.natural(k, availW, 0)
		if v := k.Width(); v >= 0 {
			w = v
		}
		colW[i%cols] = maxf(colW[i%cols], w)
	}
	total := spacing * float64(cols-1)
	for _, w := range colW {
		total += w
	}
	return total
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
		k.ResolveSize(availW, availH)
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
func (e *Engine) stackChildren(b *tree.NodeBase, w, h, insetL, insetT, insetR, insetB float64) {
	// An overlay has no flow direction of its own, so it borrows the Column
	// convention: the main axis runs down, the cross axis across.
	cross, main := "start", "start"
	if c, ok := containerKind(b); ok {
		cross, main = c.CrossAlign.Get(), c.MainAlign.Get()
	}
	for _, k := range b.Kids() {
		if !k.Visible() {
			continue
		}
		innerW, innerH := maxf(w-insetL-insetR, 0), maxf(h-insetT-insetB, 0)
		if k.Node().Kind() == "Positioned" {
			r := e.positionedFrame(k, innerW, innerH, insetL, insetT, cross, main)
			k.SetBounds(r)
			e.children(k, r.W, r.H)
			continue
		}
		mw, mh := e.natural(k, innerW, innerH)
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
			cw = innerW
		}
		if k.Height() < 0 {
			ch = innerH
		}
		a, b2 := k.AlignSelf(), k.AlignSelf()
		if a == "" {
			a, b2 = cross, main
		}
		me := k.MarginEdges()
		r := tree.Rect{
			X: insetL + crossAlign(a, innerW, cw) + me.Left,
			Y: insetT + crossAlign(b2, innerH, ch) + me.Top,
			W: cw,
			H: ch,
		}
		k.SetBounds(r)
		e.children(k, cw, ch)
	}
}

// positionedFrame places a Positioned child in a Stack content box. A negative
// offset is auto: the declared or intrinsic size is used and the Stack's align
// decides where the box sits. left+right (or top+bottom) without a declared
// size stretches the box between the two edges.
func (e *Engine) positionedFrame(k *tree.NodeBase, availW, availH, offsetX, offsetY float64, cross, main string) tree.Rect {
	p, ok := k.Node().(*tree.Positioned)
	if !ok {
		return tree.Rect{X: offsetX, Y: offsetY, W: availW, H: availH}
	}
	w, h := e.natural(k, availW, availH)
	if v := k.Width(); v >= 0 {
		w = v
	}
	if v := k.Height(); v >= 0 {
		h = v
	}
	left, top, right, bottom := p.Left.Get(), p.Top.Get(), p.Right.Get(), p.Bottom.Get()

	x := 0.0
	switch {
	case left >= 0 && right >= 0 && k.Width() < 0:
		w = maxf(availW-left-right, 0)
		x = left
	case left >= 0:
		x = left
	case right >= 0:
		x = availW - right - w
	default:
		if cross == "stretch" && k.Width() < 0 {
			w = availW
		} else {
			x = crossAlign(cross, availW, w)
		}
	}

	y := 0.0
	switch {
	case top >= 0 && bottom >= 0 && k.Height() < 0:
		h = maxf(availH-top-bottom, 0)
		y = top
	case top >= 0:
		y = top
	case bottom >= 0:
		y = availH - bottom - h
	default:
		if main == "stretch" && k.Height() < 0 {
			h = availH
		} else {
			y = crossAlign(main, availH, h)
		}
	}

	// A translate is the enter/exit animation offset, applied after anchoring.
	if v := p.TranslateX.Get(); v != 0 {
		x += v
	}
	if v := p.TranslateY.Get(); v != 0 {
		y += v
	}

	// Margin shrinks the box and shifts it inward, like everywhere else.
	me := k.MarginEdges()
	return tree.Rect{
		X: offsetX + x + me.Left,
		Y: offsetY + y + me.Top,
		W: maxf(w-me.Left-me.Right, 0),
		H: maxf(h-me.Top-me.Bottom, 0),
	}
}

// positionedExtent is how much room a Positioned child takes from its Stack's
// auto size: the anchored edges plus its own size.
func (e *Engine) positionedExtent(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	p, ok := k.Node().(*tree.Positioned)
	if !ok {
		return e.natural(k, availW, availH)
	}
	w, h := e.natural(k, availW, availH)
	if v := k.Width(); v >= 0 {
		w = v
	}
	if v := k.Height(); v >= 0 {
		h = v
	}
	left, top, right, bottom := p.Left.Get(), p.Top.Get(), p.Right.Get(), p.Bottom.Get()
	switch {
	case left >= 0 && right >= 0 && k.Width() < 0:
		w = left + right
	case left >= 0:
		w += left
	case right >= 0:
		w += right
	}
	switch {
	case top >= 0 && bottom >= 0 && k.Height() < 0:
		h = top + bottom
	case top >= 0:
		h += top
	case bottom >= 0:
		h += bottom
	}
	me := k.MarginEdges()
	return w + me.Left + me.Right, h + me.Top + me.Bottom
}

// natural returns the intrinsic size of a node, memoized for the current pass.
//
// A leaf is cheap to measure and most trees are wide rather than deep, so only a
// subtree is worth a map slot: the lookup on a flat row of 10k children would
// cost more than the answers it saves.
func (e *Engine) natural(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	if len(k.Kids()) == 0 {
		return e.naturalSize(k, availW, availH)
	}
	key := naturalKey{node: k, availW: availW, availH: availH}
	if got, ok := e.memo[key]; ok {
		return got.w, got.h
	}
	w, h := e.naturalSize(k, availW, availH)
	if e.memo == nil {
		e.memo = make(map[naturalKey]naturalSize, 64)
	}
	e.memo[key] = naturalSize{w, h}
	return w, h
}

// naturalSize is natural without the cache. It only reads properties, padding,
// margins and children's intrinsic sizes, never bounds, so a cached answer stays
// valid for the rest of the pass.
func (e *Engine) naturalSize(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	k.ResolveSize(availW, availH)
	if k.Node().Kind() == "FABMenu" {
		return e.fabMenuNatural(k, availW, availH)
	}
	if k.Node().Kind() == "Table" {
		return e.tableNatural(k, availW, availH)
	}
	if cv, ok := e.componentOf(k); ok {
		mt := e.metricsFor(k, cv)
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
		return e.textSize(n.Text.Get(), n, availW)
	case *tree.RichText:
		return e.richTextSize(n, availW)
	case *tree.SVG:
		// An SVG knows its own viewBox size, so width and height can be left out.
		return n.IntrinsicW, n.IntrinsicH
	}
	if k.Node().Kind() == "Window" {
		return availW, availH
	}
	return e.containerNatural(k, availW, availH)
}

// flowHorizontal reports containers whose children run left to right.
func flowHorizontal(kind string) bool {
	switch kind {
	case "Row", "TitleBar", "ButtonGroup", "Toolbar", "Carousel", "Tabs", "NavigationBar":
		return true
	}
	return false
}

// contentHeight is the intrinsic height: the sum for a column, the tallest
// child for a row.
func (e *Engine) contentHeight(k *tree.NodeBase, availW float64, mt m3.Metrics) float64 {
	spacing := e.containerSpacing(k)
	total := mt.Top + mt.Bottom
	horizontal := flowHorizontal(k.Node().Kind())
	first := true
	for _, c := range visibleChildren(k) {
		_, h := e.natural(c, availW-mt.Left-mt.Right, 0)
		if v := c.Height(); v >= 0 {
			h = v
		}
		me := c.MarginEdges()
		if horizontal {
			total = maxf(total, h+mt.Top+mt.Bottom+me.Top+me.Bottom)
			continue
		}
		if !first {
			total += spacing
		}
		total += h + me.Top + me.Bottom
		first = false
	}
	return total
}

// contentWidth is the intrinsic width: the widest child for a column, the sum
// for a row.
func (e *Engine) contentWidth(k *tree.NodeBase, availW, h float64, mt m3.Metrics) float64 {
	spacing := e.containerSpacing(k)
	total := mt.Left + mt.Right
	horizontal := flowHorizontal(k.Node().Kind())
	first := true
	for _, c := range visibleChildren(k) {
		w, _ := e.natural(c, availW-mt.Left-mt.Right, h)
		if v := c.Width(); v >= 0 {
			w = v
		}
		// Placement lays a child out inside its margins, so the intrinsic width
		// must include them or the last child of a row hangs past its parent.
		me := c.MarginEdges()
		if !horizontal {
			total = maxf(total, w+mt.Left+mt.Right+me.Left+me.Right)
			continue
		}
		if !first {
			total += spacing
		}
		total += w + me.Left + me.Right
		first = false
	}
	return total
}

// containerCrossAlign reads a flow container's cross-axis alignment. A component
// keeps it in its own cell, so reading only the plain Container would always say
// "start".
func (e *Engine) containerCrossAlign(k *tree.NodeBase) string {
	if c, ok := e.componentOf(k); ok && c.CrossAlign != "" {
		return c.CrossAlign
	}
	return containerOf(k).CrossAlign.Get()
}

// containerMainAlign reads the main-axis alignment, which places the whole run
// along the container's own direction.
func (e *Engine) containerMainAlign(k *tree.NodeBase) string {
	if c, ok := e.componentOf(k); ok && c.MainAlign != "" {
		return c.MainAlign
	}
	return containerOf(k).MainAlign.Get()
}

// containerSpacing reads spacing from a component or a Container.
func (e *Engine) containerSpacing(k *tree.NodeBase) float64 {
	if c, ok := e.componentOf(k); ok {
		return c.Spacing
	}
	return containerOf(k).Spacing.Get()
}

// textSize measures a Text node, honouring fontSize, role and wrap. availW is
// the space the parent offers, which is the wrap column when the node declares
// no width of its own.
func (e *Engine) textSize(s string, t *tree.Text, availW float64) (float64, float64) {
	st := m3.RoleStyle(t.Role.Get())
	size := t.FontSize.Get()
	lineHeight := 0.0
	if size <= 0 {
		// Role-based size: the metrics helper knows the type scale.
		size, lineHeight = st.Size, st.LineHeight
	}
	// The role's weight is the base even when fontSize overrides the size, and
	// both layout and paint resolve fontWeight through WeightFor.
	weight := t.WeightFor(st.Weight)
	if lineHeight <= 0 {
		lineHeight = size * 1.2
	}
	family := t.FontFamily()
	if !t.Wrap.Get() {
		return e.measureFamily(s, size, weight, family)
	}
	limit := availW
	if v := t.Width(); v >= 0 {
		limit = v
	}
	if limit <= 0 {
		return e.measureFamily(s, size, weight, family)
	}
	// Layout and paint must break in the same places or the reserved height is
	// wrong, so both use the shared greedy wrap with the same advances.
	key := wrapKey{s: s, width: limit, size: size, weight: weight, family: t.FontFamily()}
	n, ok := e.wraps[key]
	if !ok {
		n = len(text.WrapRanges(s, limit, func(run string) float64 {
			w, _ := e.measureFamily(run, size, weight, key.family)
			return w
		}))
		if e.wraps == nil || len(e.wraps) >= maxWrapCounts {
			e.wraps = make(map[wrapKey]int, maxWrapCounts)
		}
		e.wraps[key] = n
	}
	return limit, float64(n) * lineHeight
}

// measureFamily measures one run in a design's font family, falling back to the
// measured face when the app registered none or the measurer cannot ask.
func (e *Engine) measureFamily(s string, size float64, weight int, family string) (float64, float64) {
	if family != "" {
		if fm, ok := e.Measurer.(FamilyMeasurer); ok {
			return fm.MeasureFamily(s, size, weight, family)
		}
	}
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
	case *tree.TitleBar:
		return &n.Container
	case *tree.Stack:
		return &n.Container
	case *tree.Wrap:
		return &n.Container
	case *tree.Grid:
		return &n.Container
	}
	// A node that is not a container has no spacing or alignment of its own.
	// Its zero value answers both questions; sharing one avoids an allocation
	// per call, which happens for every component in the tree.
	return &zeroContainer
}

// zeroContainer is the read-only answer for nodes without a Container. Nothing
// ever writes through it.
var zeroContainer tree.Container

// containerKind reports whether the node has a Container with alignment.
func containerKind(b *tree.NodeBase) (*tree.Container, bool) {
	switch n := b.Node().(type) {
	case *tree.Column:
		return &n.Container, true
	case *tree.Row:
		return &n.Container, true
	case *tree.TitleBar:
		return &n.Container, true
	case *tree.Stack:
		return &n.Container, true
	case *tree.Wrap:
		return &n.Container, true
	case *tree.Grid:
		return &n.Container, true
	}
	return nil, false
}

// componentOf fills the engine's scratch view with the Material component state
// behind a node and returns it. The result is only valid until the next call and
// must not be held across one: it exists so a layout pass does not allocate a
// view per component per pass, which was most of its garbage.
func (e *Engine) componentOf(b *tree.NodeBase) (*m3.ComponentView, bool) {
	type m3er interface{ M3() *tree.Component }
	m, ok := b.Node().(m3er)
	if !ok {
		return nil, false
	}
	c := m.M3()
	cv := &e.view
	*cv = m3.ComponentView{
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
		Spacing:     c.Spacing.Get(),
		CrossAlign:  c.CrossAlign.Get(),
		MainAlign:   c.MainAlign.Get(),
	}
	return cv, true
}

// metricsFor returns a component's M3 metrics, once per pass.
func (e *Engine) metricsFor(k *tree.NodeBase, cv *m3.ComponentView) m3.Metrics {
	if mt, ok := e.metrics[k]; ok {
		return mt
	}
	family := k.FontFamily()
	mt := m3.MetricsFor(k.Node().Kind(), cv, func(s string, size float64, weight int) (float64, float64) {
		return e.measureFamily(s, size, weight, family)
	})
	if e.metrics == nil {
		e.metrics = make(map[*tree.NodeBase]m3.Metrics, 32)
	}
	e.metrics[k] = mt
	return mt
}

// A FAB menu is a 56dp FAB with its actions stacked above it, 16dp apart.
const (
	fabSize = 56.0
	fabGap  = 16.0
)

// fabMenuNatural is the size of a FAB menu: the FAB alone while closed, and the
// FAB plus every action while open.
func (e *Engine) fabMenuNatural(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	cv, ok := e.componentOf(k)
	if !ok || !cv.Expanded {
		return fabSize, fabSize
	}
	w, h := fabSize, fabSize
	for _, kid := range visibleChildren(k) {
		kw, kh := e.natural(kid, availW, availH)
		if v := kid.Width(); v >= 0 {
			kw = v
		}
		if v := kid.Height(); v >= 0 {
			kh = v
		}
		w = maxf(w, kw)
		h += fabGap + kh
	}
	return w, h
}

// fabMenuChildren stacks the actions above the FAB, right-aligned, with the
// first declared action closest to the FAB. A closed menu gives its actions no
// bounds at all, so they neither draw nor take a click.
func (e *Engine) fabMenuChildren(b *tree.NodeBase, kids []*tree.NodeBase, w, h float64) {
	cv, ok := e.componentOf(b)
	if !ok || !cv.Expanded {
		for _, k := range kids {
			k.SetBounds(tree.Rect{})
			e.children(k, 0, 0)
		}
		return
	}
	pe, be := b.PaddingEdges(), b.BorderEdges()
	innerW := maxf(w-pe.Left-pe.Right-be.Left-be.Right, 0)
	right := pe.Left + be.Left + innerW
	y := h - pe.Bottom - be.Bottom - fabSize
	for _, k := range kids {
		kw, kh := e.natural(k, innerW, y)
		if v := k.Width(); v >= 0 {
			kw = v
		}
		if v := k.Height(); v >= 0 {
			kh = v
		}
		y -= fabGap + kh
		k.SetBounds(tree.Rect{X: right - kw, Y: y, W: kw, H: kh})
		e.children(k, kw, kh)
	}
}

// containerNatural is the auto size of Column, Row and Stack.
func (e *Engine) containerNatural(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	kids := visibleChildren(k)
	spacing := containerOf(k).Spacing.Get()
	pe, be := k.PaddingEdges(), k.BorderEdges()
	insetW := pe.Left + pe.Right + be.Left + be.Right
	insetH := pe.Top + pe.Bottom + be.Top + be.Bottom
	kind := k.Node().Kind()
	innerW := maxf(availW-insetW, 0)

	switch kind {
	case "Positioned":
		var maxW, maxH float64
		innerH := maxf(availH-insetH, 0)
		for _, c := range kids {
			cw, ch := e.natural(c, innerW, innerH)
			if v := c.Width(); v >= 0 {
				cw = v
			}
			if v := c.Height(); v >= 0 {
				ch = v
			}
			maxW, maxH = maxf(maxW, cw), maxf(maxH, ch)
		}
		return maxW + insetW, maxH + insetH
	case "Wrap":
		total := 0.0
		for i, c := range kids {
			w, _ := e.natural(c, innerW, 0)
			if v := c.Width(); v >= 0 {
				w = v
			}
			if i > 0 {
				total += spacing
			}
			total += w
		}
		return minf(total, innerW) + insetW, e.wrapHeight(kids, innerW, spacing) + insetH
	case "Grid":
		cols := gridColumns(k)
		return minf(e.gridNaturalWidth(kids, innerW, cols, spacing), innerW) + insetW,
			e.gridHeight(kids, innerW, cols, spacing) + insetH
	}

	var mainSum, maxW, maxH float64
	for i, c := range kids {
		var cw, ch float64
		if kind == "Stack" && c.Node().Kind() == "Positioned" {
			cw, ch = e.positionedExtent(c, innerW, maxf(availH-insetH, 0))
		} else {
			cw, ch = e.natural(c, availW, availH)
			if v := c.Width(); v >= 0 {
				cw = v
			}
			if v := c.Height(); v >= 0 {
				ch = v
			}
		}
		switch kind {
		case "Row", "TitleBar":
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
	if kind == "Row" || kind == "TitleBar" {
		return mainSum + insetW, maxH + insetH
	}
	if kind == "Stack" {
		return maxf(maxW, mainSum) + insetW, maxH + insetH
	}
	return maxW + insetW, mainSum + insetH
}

// isContainer reports whether a child fills its parent's cross axis. It lists
// the containers that are meant to span the space they are given: a Column or a
// page body, a sheet, a bar. A floating Toolbar is deliberately absent — it is a
// pill that hugs its actions, like a FAB menu.
func isContainer(k *tree.NodeBase) bool {
	switch k.Node().Kind() {
	case "Window", "Column", "Row", "TitleBar", "Stack", "Positioned", "Wrap", "Grid",
		"Card", "Dialog", "BottomSheet", "SideSheet", "List", "Menu",
		"NavigationDrawer", "NavigationBar", "NavigationRail", "Tabs",
		"ButtonGroup", "Carousel":
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

// distribute splits a free main-axis amount into a leading offset and an extra
// gap between children, which is what Flutter's MainAxisAlignment does. It is
// used by the row/column algorithm, by a wrap run and by a grid's rows, so the
// six names mean the same thing everywhere.
func distribute(a string, free float64, n int) (lead, gap float64) {
	if n <= 0 || free <= 0 {
		return 0, 0
	}
	switch a {
	case "center":
		return free / 2, 0
	case "end":
		return free, 0
	case "spaceBetween":
		if n == 1 {
			return 0, 0
		}
		return 0, free / float64(n-1)
	case "spaceAround":
		g := free / float64(n)
		return g / 2, g
	case "spaceEvenly":
		g := free / float64(n+1)
		return g, g
	}
	return 0, 0
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
	return b.MinHeight()
}

func maxSize(b *tree.NodeBase, horizontal bool) float64 {
	m := b.MaxWidth()
	if !horizontal {
		m = b.MaxHeight()
	}
	if m >= 0 {
		return m
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

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
