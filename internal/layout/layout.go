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
	// A map's children are overlays on the map, not content in its padding.
	if b.Node().Kind() == "Map" {
		e.mapChildren(b, kids, w, h)
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
		case "CircularProgress":
			// A ring's children are a label or an icon inside it, so each one
			// keeps its own size and sits in the middle.
			e.centeredChildren(b, kids, cw, ch, insetL, insetT)
		case "NavigationRail", "NavigationDrawer":
			e.linear(b, kids, cw, ch, insetL, false, insetT)
		case "Popover":
			e.popoverChildren(b, kids, cw, ch, insetL, insetT)
		case "ListItem":
			e.listItemSlots(b, kids, cw, ch, insetL, insetT)
		case "TextField":
			e.textFieldSlots(b, kids, cw, ch, insetL, insetT)
		case "SearchBar":
			e.searchBarSlots(b, kids, cw, ch, insetL, insetT)
		case "Button":
			e.buttonChildren(b, kids, cw, ch, insetL, insetT)
		case "ButtonGroup", "Row", "Toolbar", "Breadcrumb":
			e.linear(b, kids, cw, ch, insetL, true, insetT)
		case "Carousel":
			e.carouselChildren(b, kids, cw, ch, insetL, insetT)
		case "CarouselItem":
			// A card's image or overlay fills it; a Positioned child still
			// anchors itself.
			e.stackChildren(b, w, h, pe.Left+be.Left, pe.Top+be.Top, pe.Right+be.Right, pe.Bottom+be.Bottom)
		default:
			e.linear(b, kids, cw, ch, insetL, false, insetT)
		}
		return
	}

	switch b.Node().Kind() {
	case "Row", "TitleBar":
		e.linear(b, kids, cw, ch, pe.Left+be.Left, true, pe.Top+be.Top)
	case "Flex":
		e.linear(b, kids, cw, ch, pe.Left+be.Left, flowHorizontal(b), pe.Top+be.Top)
	case "Stack", "ClipRRect", "Positioned", "AspectRatio", "GestureDetector", "Hero":
		e.stackChildren(b, w, h, pe.Left+be.Left, pe.Top+be.Top, pe.Right+be.Right, pe.Bottom+be.Bottom)
	case "Wrap":
		e.wrap(b, kids, cw, ch, pe.Left+be.Left, pe.Top+be.Top)
	case "Grid":
		e.grid(b, kids, cw, ch, pe.Left+be.Left, pe.Top+be.Top)
	default: // Window, Column
		e.linear(b, kids, cw, ch, pe.Left+be.Left, false, pe.Top+be.Top)
	}
}

// carouselChildren lays a carousel out from its scroll offset: every card before
// the leading one keeps the full width, the leading one is full, the next grows
// from shrink to full as the strip scrolls, and the rest stay shrunk. That is
// what makes the incoming card widen while the outgoing card leaves.
func (e *Engine) carouselChildren(b *tree.NodeBase, kids []*tree.NodeBase, w, h, insetL, insetT float64) {
	c, ok := b.Node().(*tree.Carousel)
	if !ok {
		e.linear(b, kids, w, h, insetL, true, insetT)
		return
	}
	shrink := c.Shrink.Get()
	if shrink < 0 {
		shrink = 0
	}
	if shrink > 1 {
		shrink = 1
	}
	spacing := c.Spacing.Get()
	offset := b.ScrollOffsetFor(tree.AxisX)
	align := c.CrossAlign.Get()

	large := func(k *tree.NodeBase) float64 {
		if v := k.Width(); v >= 0 {
			return v
		}
		mw, _ := e.natural(k, w, h)
		if mw < 0 {
			mw = 0
		}
		return mw
	}

	// The items before the leading one are full width, so their starts are
	// independent of the animated cards. Find the last start the leading edge
	// has passed.
	lead, leadStart := 0, 0.0
	x := 0.0
	for i, k := range kids {
		if x <= offset+0.5 {
			lead, leadStart = i, x
		}
		x += large(k) + spacing
	}
	progress := 0.0
	if lead < len(kids) {
		if pitch := large(kids[lead]) + spacing; pitch > 0 {
			progress = (offset - leadStart) / pitch
			progress = maxf(0, minf(1, progress))
		}
	}

	x = 0
	for i, k := range kids {
		width := large(k)
		if i > lead {
			small := width * shrink
			width = small
			if i == lead+1 {
				width = small + (large(k)-small)*progress
			}
		}
		height := k.Height()
		if height < 0 {
			if align == "stretch" {
				height = h
			} else {
				_, height = e.natural(k, width, h)
			}
		}
		me := k.MarginEdges()
		y := insetT + crossAlign(align, h, height)
		r := tree.Rect{
			X: insetL + x + me.Left,
			Y: y + me.Top,
			W: maxf(width, 0),
			H: maxf(height, 0),
		}
		k.SetBounds(r)
		e.children(k, r.W, r.H)
		x += me.Left + r.W + me.Right + spacing
	}
}

// listItemSlots places a row's $leading and $trailing bodies in the icon squares
// the built-in icons occupy, centred. An explicit icon property wins over the
// slot, so while it is set the slot keeps no bounds: nothing of it paints or
// takes a press.
func (e *Engine) listItemSlots(b *tree.NodeBase, kids []*tree.NodeBase, w, h, insetL, insetT float64) {
	li, ok := b.Node().(*tree.ListItem)
	if !ok {
		e.linear(b, kids, w, h, insetL, false, insetT)
		return
	}
	place := func(n tree.Node, x float64) {
		if n == nil {
			return
		}
		size := minf(40, h)
		if size < 0 {
			size = 0
		}
		x += insetL
		if x < insetL {
			x = insetL
		}
		n.Base().SetBounds(tree.Rect{X: x, Y: insetT + (h-size)/2, W: 40, H: size})
		e.children(n.Base(), 40, size)
	}
	if li.Icon.Get() == "" {
		place(li.LeadingNode, 0)
	}
	if li.Trailing.Get() == "" {
		place(li.TrailingNode, w-40)
	}
}

// textFieldSlots puts a field's $leading and $trailing bodies in the 40dp
// columns its icons take, centred in the box the field draws (the supporting
// line is not part of it). An explicit icon wins over the slot, so while it is
// set the slot keeps no bounds: nothing of it paints or takes a press.
func (e *Engine) textFieldSlots(b *tree.NodeBase, kids []*tree.NodeBase, w, h, insetL, insetT float64) {
	tf, ok := b.Node().(*tree.TextField)
	if !ok {
		e.linear(b, kids, w, h, insetL, false, insetT)
		return
	}
	if tf.Supporting.Get() != "" && h > 20 {
		h -= 20
	}
	place := func(n tree.Node, x float64) {
		if n == nil {
			return
		}
		size := minf(40, h)
		if size < 0 {
			size = 0
		}
		x += insetL
		if x < insetL {
			x = insetL
		}
		n.Base().SetBounds(tree.Rect{X: x, Y: insetT + (h-size)/2, W: 40, H: size})
		e.children(n.Base(), 40, size)
	}
	if tf.Icon.Get() == "" {
		place(tf.LeadingNode, 8)
	}
	if tf.Trailing.Get() == "" {
		place(tf.TrailingNode, w-48)
	}
}

// searchBarSlots places the bar's $leading and $trailing bodies in the same
// icon squares the built-in glyphs use. A $leading body replaces the search
// glyph, and a trailingIcon wins over the trailing body.
func (e *Engine) searchBarSlots(b *tree.NodeBase, kids []*tree.NodeBase, w, h, insetL, insetT float64) {
	sb, ok := b.Node().(*tree.SearchBar)
	if !ok {
		e.linear(b, kids, w, h, insetL, false, insetT)
		return
	}
	place := func(n tree.Node, x float64) {
		if n == nil {
			return
		}
		size := minf(40, h)
		if size < 0 {
			size = 0
		}
		x += insetL
		if x < insetL {
			x = insetL
		}
		n.Base().SetBounds(tree.Rect{X: x, Y: insetT + (h-size)/2, W: 40, H: size})
		e.children(n.Base(), 40, size)
	}
	place(sb.LeadingNode, 8)
	if sb.Trailing.Get() == "" {
		place(sb.TrailingNode, w-48)
	}
}

// buttonChildren places a button's end slots and centres the default children
// between them. An explicit leading or trailing icon wins over its slot, which
// keeps no bounds while the icon is set. The children are one horizontal run,
// centred as a group; a design that needs another arrangement wraps them in a
// Row or Column of its own.
func (e *Engine) buttonChildren(b *tree.NodeBase, kids []*tree.NodeBase, w, h, insetL, insetT float64) {
	btn, ok := b.Node().(*tree.Button)
	if !ok || btn.LabelText == nil {
		e.linear(b, kids, w, h, insetL, false, insetT)
		return
	}
	const (
		side = 16.0
		gap  = 8.0
		slot = 24.0
	)
	if btn.LeadingNode != nil {
		btn.LeadingNode.Base().SetVisible(btn.Icon.Get() == "")
	}
	if btn.TrailingNode != nil {
		btn.TrailingNode.Base().SetVisible(btn.Trailing.Get() == "")
	}

	iconSize := func(v float64) float64 {
		if v <= 0 {
			return 18
		}
		return v
	}
	leadSize, trailSize := 0.0, 0.0
	if btn.Icon.Get() != "" {
		leadSize = iconSize(btn.LeadingIconSize.Get())
	} else if btn.LeadingNode != nil {
		leadSize = minf(slot, h)
	}
	if btn.Trailing.Get() != "" {
		trailSize = iconSize(btn.TrailingIconSize.Get())
	} else if btn.TrailingNode != nil {
		trailSize = minf(slot, h)
	}
	leadZone, trailZone := 0.0, 0.0
	if leadSize > 0 {
		leadZone = leadSize + gap
	}
	if trailSize > 0 {
		trailZone = trailSize + gap
	}

	// Slots only take bounds when they are the visible side; the icon is drawn
	// by paint and the slot must not paint over it.
	if btn.Icon.Get() == "" && btn.LeadingNode != nil && leadSize > 0 {
		n := btn.LeadingNode
		n.Base().SetBounds(tree.Rect{X: insetL + side, Y: insetT + (h-leadSize)/2, W: leadSize, H: leadSize})
		e.children(n.Base(), leadSize, leadSize)
	}
	if btn.Trailing.Get() == "" && btn.TrailingNode != nil && trailSize > 0 {
		n := btn.TrailingNode
		n.Base().SetBounds(tree.Rect{X: insetL + w - side - trailSize, Y: insetT + (h-trailSize)/2, W: trailSize, H: trailSize})
		e.children(n.Base(), trailSize, trailSize)
	}

	var content []*tree.NodeBase
	for _, k := range kids {
		if btn.LeadingNode != nil && k == btn.LeadingNode.Base() {
			continue
		}
		if btn.TrailingNode != nil && k == btn.TrailingNode.Base() {
			continue
		}
		content = append(content, k)
	}
	if len(content) == 0 {
		return
	}
	contentX, contentW := insetL, w
	if leadZone > 0 || trailZone > 0 {
		contentX = insetL + side + leadZone
		contentW = w - 2*side - leadZone - trailZone
		contentW = maxf(contentW, 0)
	}

	type item struct {
		k    *tree.NodeBase
		w, h float64
	}
	items := make([]item, 0, len(content))
	total, tallest := 0.0, 0.0
	for _, k := range content {
		cw, ch := e.natural(k, contentW, h)
		if v := k.Width(); v >= 0 {
			cw = v
		}
		if v := k.Height(); v >= 0 {
			ch = v
		}
		cw = minf(cw, contentW)
		if ch > tallest {
			tallest = ch
		}
		total += cw
		items = append(items, item{k: k, w: cw, h: ch})
	}
	if len(items) > 1 {
		total += gap * float64(len(items)-1)
	}
	x := contentX + (contentW-total)/2
	if x < contentX {
		x = contentX
	}
	y := insetT + (h-tallest)/2
	for _, it := range items {
		it.k.SetBounds(tree.Rect{X: x, Y: y + (tallest-it.h)/2, W: it.w, H: it.h})
		e.children(it.k, it.w, it.h)
		x += it.w + gap
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

	// Most rows and columns have a few children; the inline buffer keeps their
	// sizes off the heap without capping a large container.
	var sizeBuf [16]float64
	var sizes []float64
	if len(kids) <= len(sizeBuf) {
		sizes = sizeBuf[:len(kids)]
	} else {
		sizes = make([]float64, len(kids))
	}
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
		// A wrapped Text takes the column's width instead of keeping its
		// unwrapped one: a block that overflows a narrow column would paint
		// outside it and reserve a height for the wrong line count.
		if !horizontal && k.Height() < 0 {
			if tx, ok := k.Node().(*tree.Text); ok && tx.Width() < 0 &&
				(tx.Wrap.Get() || tx.MaxLines.Get() > 0) && cs > cross {
				cs = maxf(cross, 0)
				_, sz = e.textSize(tx.Text.Get(), tx, cs)
				sz = clampSize(sz, minSize(k, horizontal), maxSize(k, horizontal))
			}
		}
		// An AspectRatio takes its width from the parent and derives its height,
		// or its height in a Row and derives its width. A declared dimension
		// still wins for its own axis, and the other follows the ratio.
		if k.Node().Kind() == "AspectRatio" {
			r := aspectRatio(k)
			if horizontal {
				h := cs
				if v := k.Height(); v >= 0 {
					h = v
				}
				w := h * r
				if v := k.Width(); v >= 0 {
					w = v
				}
				sz, cs = w, h
			} else {
				w := cs
				if v := k.Width(); v >= 0 {
					w = v
				}
				h := w / r
				if v := k.Height(); v >= 0 {
					h = v
				}
				cs, sz = w, h
			}
		}
		// A Wrap or Grid wraps to the width it actually gets, which may be a
		// declared one rather than the parent's, so its height follows cs.
		if !horizontal && k.Height() < 0 && (k.Node().Kind() == "Wrap" || k.Node().Kind() == "Grid") {
			pe, be := k.PaddingEdges(), k.BorderEdges()
			inner := maxf(cs-pe.Left-pe.Right-be.Left-be.Right, 0)
			insetH := pe.Top + pe.Bottom + be.Top + be.Bottom
			switch {
			case k.Node().Kind() == "Wrap":
				sz = e.wrapHeight(visibleChildren(k), inner, containerOf(k).Spacing.Get()) + insetH
			case gridVariant(k) == "masonry":
				sz = e.masonryHeight(visibleChildren(k), inner, gridColumns(k), containerOf(k).Spacing.Get()) + insetH
			default:
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

// mapChildren places a map's overlays: a Geom or Path covers the map so its
// painter can project anywhere, while a Point keeps the size of its children
// and is moved to its coordinate at paint time.
func (e *Engine) mapChildren(b *tree.NodeBase, kids []*tree.NodeBase, w, h float64) {
	for _, k := range kids {
		if k.Node().Kind() == "Geom" || k.Node().Kind() == "Path" {
			k.SetBounds(tree.Rect{W: w, H: h})
			e.children(k, w, h)
			continue
		}
		cw, ch := e.natural(k, w, h)
		if v := k.Width(); v >= 0 {
			cw = v
		}
		if v := k.Height(); v >= 0 {
			ch = v
		}
		k.SetBounds(tree.Rect{W: cw, H: ch})
		e.children(k, cw, ch)
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
	// Most Wrap containers hold a handful of children. Keeping the working
	// slices on the stack until they outgrow the inline capacity avoids one
	// heap allocation per wrap per frame.
	const inline = 16
	var itemBuf [inline]item
	items := itemBuf[:0]
	var runBuf, runWBuf [inline]float64
	var runNBuf [inline]int
	runs, runW, runN := runBuf[:0], runWBuf[:0], runNBuf[:0]
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

	y, run := offsetY, 0
	for _, it := range items {
		for run < it.run {
			y += runs[run] + spacing
			run++
		}
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
		yy := y + crossAlign(a, runs[it.run], h)
		xx := offsetX + lead + it.x + float64(it.idx)*extra + me.Left
		it.k.SetBounds(tree.Rect{X: xx, Y: yy + me.Top, W: it.w, H: h})
		e.children(it.k, it.w, h)
	}
}

// grid places children in equal-width columns; each row is as tall as its
// tallest child. A container child fills the cell, a sized one is aligned in it.
func (e *Engine) grid(b *tree.NodeBase, kids []*tree.NodeBase, availW, availH, offsetX, offsetY float64) {
	if gridVariant(b) == "masonry" {
		e.masonry(b, kids, availW, offsetX, offsetY)
		return
	}
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
// aspectRatio is the ratio an AspectRatio node asks for; anything else, or a
// ratio the design left at zero, reads as 1.
func aspectRatio(b *tree.NodeBase) float64 {
	if a, ok := b.Node().(*tree.AspectRatio); ok {
		if r := a.Ratio.Get(); r > 0 {
			return r
		}
	}
	return 1
}

// aspectNatural is the intrinsic size of an AspectRatio: the declared axis wins,
// and the other follows the ratio. With neither declared, the parent's width
// decides; a width-unbounded context falls back to the height.
func (e *Engine) aspectNatural(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	r := aspectRatio(k)
	w, h := k.Width(), k.Height()
	switch {
	case w >= 0 && h < 0:
		h = w / r
	case h >= 0 && w < 0:
		w = h * r
	case w < 0 && h < 0:
		if availW > 0 {
			w, h = availW, availW/r
		} else if availH > 0 {
			w, h = availH*r, availH
		}
	}
	return clampSize(w, minSize(k, true), maxSize(k, true)),
		clampSize(h, minSize(k, false), maxSize(k, false))
}

// gridVariant reads a Grid's layout style: "normal" unless the design asks for
// masonry.
func gridVariant(b *tree.NodeBase) string {
	g, ok := b.Node().(*tree.Grid)
	if !ok || g.Variant.Get() == "" {
		return "normal"
	}
	return g.Variant.Get()
}

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

// masonry places each child in the shortest column top to bottom, so boxes of
// mixed heights pack without the ragged rows a normal grid forces. A child keeps
// its natural height at the column width; the columns share the width and the
// gap between them.
func (e *Engine) masonry(b *tree.NodeBase, kids []*tree.NodeBase, availW, offsetX, offsetY float64) {
	c := containerOf(b)
	cols := gridColumns(b)
	spacing := c.Spacing.Get()
	cellW := (availW - spacing*float64(cols-1)) / float64(cols)
	if cellW < 0 {
		cellW = 0
	}
	colH := make([]float64, cols)
	for _, k := range kids {
		col := 0
		for i := 1; i < cols; i++ {
			if colH[i] < colH[col] {
				col = i
			}
		}
		w := cellW
		if v := k.Width(); v >= 0 {
			w = minf(v, cellW)
		}
		_, h := e.natural(k, w, 0)
		if v := k.Height(); v >= 0 {
			h = v
		}
		me := k.MarginEdges()
		x := offsetX + float64(col)*(cellW+spacing)
		y := offsetY + colH[col]
		k.SetBounds(tree.Rect{X: x + me.Left, Y: y + me.Top, W: w, H: h})
		e.children(k, w, h)
		colH[col] += h + me.Top + me.Bottom + spacing
	}
}

// masonryHeight is the tallest column, which is the height a masonry grid needs
// at the given width.
func (e *Engine) masonryHeight(kids []*tree.NodeBase, availW float64, cols int, spacing float64) float64 {
	cellW := (availW - spacing*float64(cols-1)) / float64(cols)
	if cellW < 0 {
		cellW = 0
	}
	colH := make([]float64, cols)
	for _, k := range kids {
		col := 0
		for i := 1; i < cols; i++ {
			if colH[i] < colH[col] {
				col = i
			}
		}
		w := cellW
		if v := k.Width(); v >= 0 {
			w = minf(v, cellW)
		}
		_, h := e.natural(k, w, 0)
		if v := k.Height(); v >= 0 {
			h = v
		}
		me := k.MarginEdges()
		colH[col] += h + me.Top + me.Bottom + spacing
	}
	tallest := 0.0
	for _, h := range colH {
		tallest = maxf(tallest, h-spacing)
	}
	return maxf(tallest, 0)
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

// centeredChildren places every child at its own size in the middle of the box,
// which is what a progress ring's icon or label wants.
func (e *Engine) centeredChildren(b *tree.NodeBase, kids []*tree.NodeBase, w, h, insetL, insetT float64) {
	for _, k := range kids {
		mw, mh := e.natural(k, w, h)
		if v := k.Width(); v >= 0 {
			mw = v
		}
		if v := k.Height(); v >= 0 {
			mh = v
		}
		x := insetL + (w-mw)/2
		y := insetT + (h-mh)/2
		k.SetBounds(tree.Rect{X: x, Y: y, W: mw, H: mh})
		e.children(k, mw, mh)
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
	if m, ok := k.Node().(*tree.Mermaid); ok {
		w, h := m.IntrinsicW, m.IntrinsicH
		if w <= 0 || h <= 0 {
			// Before the first render (headless layout or an empty source) a
			// 16:9 placeholder keeps the box measurable.
			w, h = 320, 180
		}
		dw, dh := k.Width(), k.Height()
		switch {
		case dw >= 0 && dh >= 0:
			w, h = dw, dh
		case dw >= 0:
			h = dw * h / w
			w = dw
		case dh >= 0:
			w = dh * w / h
			h = dh
		case availW > 0 && w > availW:
			// Without a declared size, a diagram wider than its column shrinks
			// to fit and its height follows.
			h = availW * h / w
			w = availW
		}
		return w, h
	}
	if k.Node().Kind() == "FABMenu" {
		return e.fabMenuNatural(k, availW, availH)
	}
	if k.Node().Kind() == "Table" {
		return e.tableNatural(k, availW, availH)
	}
	if k.Node().Kind() == "AspectRatio" {
		return e.aspectNatural(k, availW, availH)
	}
	if k.Node().Kind() == "Popover" {
		return e.popoverNatural(k, availW, availH)
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
		// The component's own metrics do not know the design's padding, so an
		// auto-sized box grows around its content, the way a padded Text does.
		pe, be := k.PaddingEdges(), k.BorderEdges()
		if !mt.FillW && k.Width() < 0 {
			w += pe.Left + pe.Right + be.Left + be.Right
		}
		if !mt.FillH && k.Height() < 0 {
			h += pe.Top + pe.Bottom + be.Top + be.Bottom
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

// flowHorizontal reports containers whose children run left to right. A Flex
// asks its own direction, since that is a property rather than a kind.
func flowHorizontal(b *tree.NodeBase) bool {
	switch b.Node().Kind() {
	case "Row", "TitleBar", "ButtonGroup", "Toolbar", "Carousel", "Tabs", "NavigationBar", "Breadcrumb":
		return true
	case "Flex":
		return b.Node().(*tree.Flex).Direction.Get() != "column"
	}
	return false
}

// contentHeight is the intrinsic height: the sum for a column, the tallest
// child for a row.
func (e *Engine) contentHeight(k *tree.NodeBase, availW float64, mt m3.Metrics) float64 {
	spacing := e.containerSpacing(k)
	total := mt.Top + mt.Bottom
	horizontal := flowHorizontal(k)
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
	horizontal := flowHorizontal(k)
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
	// A Text's padding and border are part of its box; the glyphs live inside,
	// so the wrap column is the box minus those insets.
	padX, padY := textInsets(t)
	maxLines := int(t.MaxLines.Get())
	if maxLines < 0 {
		maxLines = 0
	}
	if t.Truncate.Get() {
		// One line, cut to the box: the glyphs beyond it are never drawn, so the
		// reserved size stops at the edge.
		w, _ := e.measureFamily(s, size, weight, family)
		boxW := textLimit(t, availW)
		inner := boxW - padX
		if inner > 0 {
			if w > inner {
				w = inner
			}
			if t.Width() >= 0 {
				w = boxW
			} else {
				w += padX
			}
		}
		return w, lineHeight + padY
	}
	if !t.Wrap.Get() && maxLines == 0 {
		w, _ := e.measureFamily(s, size, weight, family)
		return w + padX, lineHeight + padY
	}
	limit := textLimit(t, availW)
	inner := limit - padX
	if limit <= 0 || inner <= 0 {
		// No column yet: reserve one line at the line height paint will draw
		// with, not the measurer's ascent+descent, which is a few pixels less and
		// left a wrapped card short by that much per line.
		w, _ := e.measureFamily(s, size, weight, family)
		return w + padX, lineHeight + padY
	}
	// Layout and paint must break in the same places or the reserved height is
	// wrong, so both use the shared greedy wrap with the same advances.
	key := wrapKey{s: s, width: inner, size: size, weight: weight, family: family}
	n, ok := e.wraps[key]
	if !ok {
		n = len(text.WrapRanges(s, inner, func(run string) float64 {
			w, _ := e.measureFamily(run, size, weight, key.family)
			return w
		}))
		if e.wraps == nil || len(e.wraps) >= maxWrapCounts {
			e.wraps = make(map[wrapKey]int, maxWrapCounts)
		}
		e.wraps[key] = n
	}
	if maxLines > 0 && n > maxLines {
		n = maxLines
	}
	return limit, float64(n)*lineHeight + padY
}

// textInsets is the horizontal and vertical room a Text's padding and border
// take from its box.
func textInsets(t *tree.Text) (x, y float64) {
	pe, be := t.PaddingEdges(), t.BorderEdges()
	return pe.Left + pe.Right + be.Left + be.Right, pe.Top + pe.Bottom + be.Top + be.Bottom
}

// textLimit is the column a wrapped or truncated Text measures against: its own
// width when the design pins one, the space the parent offers otherwise.
func textLimit(t *tree.Text, availW float64) float64 {
	if v := t.Width(); v >= 0 {
		return v
	}
	return availW
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
	case *tree.Flex:
		return &n.Container
	case *tree.TitleBar:
		return &n.Container
	case *tree.Stack:
		return &n.Container
	case *tree.Wrap:
		return &n.Container
	case *tree.Grid:
		return &n.Container
	case *tree.AspectRatio:
		return &n.Container
	case *tree.GestureDetector:
		return &n.Container
	case *tree.Hero:
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
	case *tree.Flex:
		return &n.Container, true
	case *tree.TitleBar:
		return &n.Container, true
	case *tree.Stack:
		return &n.Container, true
	case *tree.Wrap:
		return &n.Container, true
	case *tree.Grid:
		return &n.Container, true
	case *tree.AspectRatio:
		return &n.Container, true
	case *tree.GestureDetector:
		return &n.Container, true
	case *tree.Hero:
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
		Thickness:   c.Thickness.Get(),
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
	if s, ok := b.Node().(*tree.Slider); ok {
		cv.Orientation = s.Orientation.Get()
	}
	if btn, ok := b.Node().(*tree.Button); ok && btn.LabelText != nil {
		lt := btn.LabelText
		cv.LabelRole = lt.Role.Get()
		cv.LabelFontSize = lt.FontSize.Get()
		cv.LabelWeight = lt.Weight.Get()
		cv.LeadingIconSize = btn.LeadingIconSize.Get()
		cv.TrailingIconSize = btn.TrailingIconSize.Get()
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

// popoverNatural is the anchor's own size: the panel floats over the page and
// takes no room in the flow, exactly like a tooltip's bubble.
func (e *Engine) popoverNatural(k *tree.NodeBase, availW, availH float64) (float64, float64) {
	p, ok := k.Node().(*tree.Popover)
	if !ok {
		return 0, 0
	}
	pe, be := k.PaddingEdges(), k.BorderEdges()
	innerW := maxf(availW-pe.Left-pe.Right-be.Left-be.Right, 0)
	innerH := maxf(availH-pe.Top-pe.Bottom-be.Top-be.Bottom, 0)
	spacing := e.containerSpacing(k)
	var sumW, maxH float64
	n := 0
	for _, c := range visibleChildren(k) {
		if p.Panel != nil && c == p.Panel.Base() {
			continue
		}
		cw, ch := e.natural(c, innerW, innerH)
		if v := c.Width(); v >= 0 {
			cw = v
		}
		if v := c.Height(); v >= 0 {
			ch = v
		}
		me := c.MarginEdges()
		if n > 0 {
			sumW += spacing
		}
		sumW += cw + me.Left + me.Right
		maxH = maxf(maxH, ch+me.Top+me.Bottom)
		n++
	}
	return sumW + pe.Left + pe.Right + be.Left + be.Right,
		maxH + pe.Top + pe.Bottom + be.Top + be.Bottom
}

// popoverChildren lays the anchor out as a row and hangs the panel under it,
// lined up with the anchor's own box rather than with the popover's.
func (e *Engine) popoverChildren(b *tree.NodeBase, kids []*tree.NodeBase, w, h, offsetX, offsetY float64) {
	p, ok := b.Node().(*tree.Popover)
	if !ok || p.Panel == nil {
		e.linear(b, kids, w, h, offsetX, true, offsetY)
		return
	}
	panel := p.Panel.Base()
	anchors := make([]*tree.NodeBase, 0, len(kids))
	for _, k := range kids {
		if k != panel {
			anchors = append(anchors, k)
		}
	}
	area := tree.Rect{X: offsetX, Y: offsetY}
	if len(anchors) > 0 {
		e.linear(b, anchors, w, h, offsetX, true, offsetY)
		first := anchors[0].Bounds()
		area = tree.Rect{X: first.X, Y: first.Y, W: first.W, H: first.H}
		for _, k := range anchors[1:] {
			kb := k.Bounds()
			area.W = maxf(area.W, kb.Right()-area.X)
			area.H = maxf(area.H, kb.Bottom()-area.Y)
		}
	}
	pw, ph := e.natural(panel, maxf(w, area.W), h)
	if v := panel.Width(); v >= 0 {
		pw = v
	}
	if v := panel.Height(); v >= 0 {
		ph = v
	}
	x := area.X
	switch p.Align.Get() {
	case "center":
		x = area.X + (area.W-pw)/2
	case "end":
		x = area.X + area.W - pw
	}
	y := area.Y + area.H + tree.PopoverGap
	panel.SetBounds(tree.Rect{X: x, Y: y, W: pw, H: ph})
	e.children(panel, pw, ph)
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
		if gridVariant(k) == "masonry" {
			return minf(e.gridNaturalWidth(kids, innerW, cols, spacing), innerW) + insetW,
				e.masonryHeight(kids, innerW, cols, spacing) + insetH
		}
		return minf(e.gridNaturalWidth(kids, innerW, cols, spacing), innerW) + insetW,
			e.gridHeight(kids, innerW, cols, spacing) + insetH
	}

	horizontal := flowHorizontal(k)
	stack := kind == "Stack" || kind == "Hero" || kind == "GestureDetector" || kind == "Point"
	innerH := maxf(availH-insetH, 0)
	var mainSum, maxW, maxH float64
	for i, c := range kids {
		var cw, ch float64
		if stack && c.Node().Kind() == "Positioned" {
			cw, ch = e.positionedExtent(c, innerW, innerH)
		} else {
			// Children are measured inside this box's padding and border, the
			// same width they are laid out at. Measuring at the outer width made
			// a padded card reserve the wrong number of wrapped lines.
			cw, ch = e.natural(c, innerW, innerH)
			if v := c.Width(); v >= 0 {
				cw = v
			}
			if v := c.Height(); v >= 0 {
				ch = v
			}
		}
		// Margins take room in the flow, so a column with no declared height
		// must reserve them; without them its measured box was shorter than
		// the children it then laid out, and it chose to scroll instead of
		// hugging its content.
		me := c.MarginEdges()
		switch {
		case horizontal:
			mainSum += cw + me.Left + me.Right
		case stack:
			mainSum = maxf(mainSum, cw)
		default:
			mainSum += ch + me.Top + me.Bottom
		}
		if stack {
			maxW = maxf(maxW, cw)
			maxH = maxf(maxH, ch)
		} else {
			maxW = maxf(maxW, cw+me.Left+me.Right)
			maxH = maxf(maxH, ch+me.Top+me.Bottom)
		}
		if i > 0 && !stack {
			mainSum += spacing
		}
	}
	if horizontal {
		return mainSum + insetW, maxH + insetH
	}
	if stack {
		return maxW + insetW, maxH + insetH
	}
	return maxW + insetW, mainSum + insetH
}

// isContainer reports whether a child fills its parent's cross axis. It lists
// the containers that are meant to span the space they are given: a Column or a
// page body, a sheet, a bar. A floating Toolbar is deliberately absent — it is a
// pill that hugs its actions, like a FAB menu.
func isContainer(k *tree.NodeBase) bool {
	switch k.Node().Kind() {
	case "Window", "Column", "Row", "Flex", "TitleBar", "Stack", "Positioned", "Wrap", "Grid",
		"AspectRatio", "GestureDetector",
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
