package paint

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// This file draws Material 3 components. Every number comes from the M3 specs
// (dp for sizes, colors from the theme), so the whole app follows one system.

// style resolves a type-scale role name to a text style.
func (l *List) style(role string) m3.TextStyle { return m3.RoleStyle(role) }

// weightOf maps a declared weight to the face the library actually has, so a
// weight of 600 draws with bold exactly as layout measured it.
func weightOf(w int) float32 { return float32(text.NearestWeight(text.Weight(w))) }

// withAlpha returns c with its alpha multiplied by a.
// scaleAlpha multiplies a colour's existing alpha, so a node's opacity fades a
// bitmap that is already crossfading instead of overwriting the blend.
func scaleAlpha(c uint32, a float64) uint32 {
	if a <= 0 {
		return c & 0xFFFFFF00
	}
	if a > 1 {
		a = 1
	}
	return c&0xFFFFFF00 | uint32(float64(c&0xFF)*a+0.5)
}

func withAlpha(c uint32, a float64) uint32 {
	if a <= 0 {
		// Keep the colour and drop only the alpha: a gradient resamples its
		// stops by mixing colours, so a transparent end written as black would
		// drag the band it fades into towards black.
		return c & 0xFFFFFF00
	}
	if a > 1 {
		a = 1
	}
	alpha := float64(c&0xFF) * a
	return c&0xFFFFFF00 | uint32(alpha+0.5)
}

// blend mixes two opaque colors, for state layers that should not reveal what
// is underneath.
func blend(under, over uint32, a float64) uint32 {
	mix := func(shift uint) uint32 {
		u := float64((under >> shift) & 0xFF)
		o := float64((over >> shift) & 0xFF)
		return uint32(u + (o-u)*a + 0.5)
	}
	return mix(24)<<24 | mix(16)<<16 | mix(8)<<8 | 0xFF
}

// Baseline offsets from the vertical centre of a line, in em. Centering a line
// box means putting its baseline at centre + (ascent - descent) / 2. The values
// are Roboto's and Material Symbols' hhea metrics.
const (
	textBaseline = 0.342 // (0.9277 - 0.2441) / 2
	iconBaseline = 0.5   // (1.1 - 0.1) / 2
)

// textIn appends a text op centered vertically in box.
func (l *List) textIn(s string, st m3.TextStyle, color uint32, box tree.Rect, align Align, clip tree.Rect) {
	if s == "" {
		return
	}
	baseline := box.Y + box.H/2 + st.Size*textBaseline
	l.Ops = append(l.Ops, Op{
		Kind:       Text,
		Rect:       rectArr(box),
		Color:      color,
		Clip:       clipArr(clip),
		Text:       s,
		FontSize:   float32(st.Size),
		Weight:     weightOf(st.Weight),
		FontFamily: l.family,
		Align:      align,
		TextX:      float32(box.X),
		TextY:      float32(baseline),
		BoxW:       float32(box.W),
	})
}

// iconIn appends an icon op centered at cx, cy, in the face the component's
// `filledIcon` picks.
func (l *List) iconIn(name string, size float64, color uint32, cx, cy float64, clip tree.Rect) {
	l.iconInFilled(name, size, color, cx, cy, clip, l.filledIcons)
}

// leadingIconIn is iconIn for the glyph in a component's leading slot, which
// `filledLeadingIcon` fills on its own.
func (l *List) leadingIconIn(name string, size float64, color uint32, cx, cy float64, clip tree.Rect) {
	l.iconInFilled(name, size, color, cx, cy, clip, l.filledIcons || l.filledLeading)
}

// trailingIconIn is leadingIconIn for the trailing slot.
func (l *List) trailingIconIn(name string, size float64, color uint32, cx, cy float64, clip tree.Rect) {
	l.iconInFilled(name, size, color, cx, cy, clip, l.filledIcons || l.filledTrailing)
}

func (l *List) iconInFilled(name string, size float64, color uint32, cx, cy float64, clip tree.Rect, filled bool) {
	if name == "" {
		return
	}
	box := tree.Rect{X: cx - size/2, Y: cy - size/2, W: size, H: size}
	l.Ops = append(l.Ops, Op{
		Kind:       Text,
		Rect:       rectArr(box),
		Color:      color,
		Clip:       clipArr(clip),
		Text:       name,
		FontSize:   float32(size),
		Weight:     400,
		Icon:       true,
		FilledIcon: filled,
		Align:      AlignCenter,
		TextX:      float32(box.X),
		TextY:      float32(cy + size*iconBaseline),
		BoxW:       float32(size),
	})
}

// shadow appends an elevation shadow behind a rounded rect.
func (l *List) shadow(r tree.Rect, radius float64, dp int, clip tree.Rect) {
	rad := float32(radius)
	l.shadowRadii(r, [4]float32{rad, rad, rad, rad}, dp, clip)
}

// shadowRadii is shadow for a shape whose corners differ, like a split button.
func (l *List) shadowRadii(r tree.Rect, radii [4]float32, dp int, clip tree.Rect) {
	// An explicit `shadow` on the node stands in for the component's elevation,
	// including `shadow: none`, which drops it.
	if dp <= 0 || l.suppressElev {
		return
	}
	blur := float64(dp) * 2.0
	off := float64(dp) * 0.5
	sr := tree.Rect{X: r.X - blur, Y: r.Y - blur + off, W: r.W + 2*blur, H: r.H + 2*blur}
	b := float32(blur)
	l.Ops = append(l.Ops, Op{
		Kind:   Shadow,
		Rect:   rectArr(sr),
		Radius: [4]float32{radii[0] + b, radii[1] + b, radii[2] + b, radii[3] + b},
		Blur:   b,
		Color:  withAlpha(l.Theme.Colors.Shadow, 0.22),
		Clip:   clipArr(clip),
	})
}

// stateLayer overlays the M3 interaction tint and the press ripple on a shape.
// componentRadii is the corner radius a component draws with: the design's
// per-corner radii when it declared one, else the M3 shape's uniform radius.
func componentRadii(b *tree.NodeBase, fallback float64) [4]float64 {
	if b.RadiusSet() {
		return b.Radii()
	}
	return [4]float64{fallback, fallback, fallback, fallback}
}

// radii32 is the renderer's form of the per-corner radii.
func radii32(r [4]float64) [4]float32 {
	return [4]float32{float32(r[0]), float32(r[1]), float32(r[2]), float32(r[3])}
}

// componentFill is the colour and gradient a component fills with: the design's
// background when it set one, else the component's own container.
func componentFill(b *tree.NodeBase, fill uint32) (uint32, *tree.Gradient) {
	if g := b.Gradient("background"); g != nil {
		return 0, g
	}
	if v := b.Background(); v != 0 {
		return v, nil
	}
	return fill, nil
}

// componentBorder is the border a component draws itself: its own M3 outline.
// A design border is paintComponentBorder's job, because it belongs above the
// content and not on the surface the component painted first.
func componentBorder(b *tree.NodeBase, color uint32, width float64) (uint32, float64) {
	if b.BorderSet() {
		return 0, 0
	}
	return color, width
}

// componentColor is the `color` a component with its own foreground cell was
// given, zero when the design left it to the component.
func componentColor(c *tree.Component) uint32 {
	switch n := c.Node().(type) {
	case *tree.Button:
		return n.Color.Get()
	case *tree.TextField:
		return n.Color.Get()
	}
	return 0
}

func (l *List) stateLayer(c *tree.Component, r tree.Rect, radius float64, color uint32, clip tree.Rect) {
	if c.Disabled.Get() {
		return
	}
	l.ripple(c, r, radius, color, clip)
	rad := float32(radius)
	l.tint(c, r, [4]float32{rad, rad, rad, rad}, color, clip)
}

// tint draws only the interaction overlay, for shapes whose corners differ.
func (l *List) tint(c *tree.Component, r tree.Rect, radii [4]float32, color uint32, clip tree.Rect) {
	a := 0.0
	if c.Pressed {
		a = m3.StatePressed
	} else if c.Hover {
		a = m3.StateHover
	} else if c.Focused {
		a = m3.StateFocus
	}
	if a <= 0 {
		return
	}
	l.Ops = append(l.Ops, Op{
		Kind:   Fill,
		Shape:  RoundedRect,
		Rect:   rectArr(r),
		Radius: radii,
		Color:  withAlpha(color, a),
		Clip:   clipArr(clip),
	})
}

// ripple expands a circle from the press point over the component's shape. The
// interactor owns the clock; paint only maps progress to geometry.
func (l *List) ripple(c *tree.Component, r tree.Rect, radius float64, color uint32, clip tree.Rect) {
	l.rippleNode(c.Base(), r, radius, color, clip)
}

// rippleNode is ripple for any element, which is what `ripple: true` on a plain
// box needs. A design that wrote `ripple: false` keeps a component from drawing
// the press feedback it would otherwise show.
func (l *List) rippleNode(b *tree.NodeBase, r tree.Rect, radius float64, color uint32, clip tree.Rect) {
	if b.RippleSet() && !b.WantsRipple() {
		return
	}
	if !b.RippleOn || b.Ripple <= 0 {
		return
	}
	// Expand over the first 60% of the life, then fade out.
	p := b.Ripple
	grow := p / 0.6
	if grow > 1 {
		grow = 1
	}
	grow = 1 - (1-grow)*(1-grow) // ease out
	reach := math.Hypot(
		maxf(b.RippleX-r.X, r.Right()-b.RippleX),
		maxf(b.RippleY-r.Y, r.Bottom()-b.RippleY),
	)
	size := 2 * reach * grow
	fade := 1.0
	if p > 0.6 {
		fade = 1 - (p-0.6)/0.4
	}
	// The disc is only clipped to its ancestors, so cut it to the element's own
	// box and round that with the shape radius: otherwise it spills past a pill
	// or a chip. ClipBox carries the whole box, so a scrolling ancestor that
	// cuts the element leaves a straight edge instead of rounding the cut.
	cr := intersect(clip, r)
	l.rippleDrawn = true
	l.Ops = append(l.Ops, Op{
		Kind:       Fill,
		Shape:      Circle,
		Rect:       [4]float32{float32(b.RippleX - size/2), float32(b.RippleY - size/2), float32(size), float32(size)},
		Color:      withAlpha(color, m3.StatePressed*fade),
		ClipRadius: float32(radius),
		ClipBox:    rectArr(r),
		Clip:       clipArr(cr),
	})
}

// rippleRadius is the corner a generic ripple is clipped to: the radius of the
// surface the element painted (a Card's M3 shape included), the design's own
// radius when it covers the box, or square corners when the four differ.
func (l *List) rippleRadius(b *tree.NodeBase, r tree.Rect, from int) float64 {
	if i := surfaceOp(l.Ops, from, r); i >= 0 {
		rad := l.Ops[i].Radius
		if rad[0] == rad[1] && rad[1] == rad[2] && rad[2] == rad[3] {
			return float64(rad[0])
		}
		return 0
	}
	radii := b.Radii()
	if radii[0] == radii[1] && radii[1] == radii[2] && radii[2] == radii[3] {
		return radii[0]
	}
	return 0
}

// textShift keeps the caret inside a text box: the value text is moved left by
// this many pixels so the caret sits at most caretPad from the right edge, and
// never before the left edge. caretX is the caret's distance from the text
// start, IME composition included.
func textShift(caretX, width float64) float64 {
	if width <= 0 || caretX <= 0 {
		return 0
	}
	const caretPad = 2.0 // the caret itself is 2dp wide
	if caretX <= width-caretPad {
		return 0
	}
	return caretX - (width - caretPad)
}

// fieldCaretX is where the caret sits inside a field's value, composition
// included, in pixels from the start of the text.
func fieldCaretX(l *List, c *tree.Component, st m3.TextStyle) float64 {
	obscure := c.Obscured()
	s := c.Text.Get()
	n := tree.FieldIndex(s, minInt(c.Caret, len(s)), obscure)
	x := textWidth(l, tree.FieldText(s, obscure)[:n], st)
	if p := c.Preedit; p != "" {
		m := tree.FieldIndex(p, minInt(c.PreeditCaret, len(p)), obscure)
		x += textWidth(l, tree.FieldText(p, obscure)[:m], st)
	}
	return x
}

// preedit draws IME composition text at x, top y, with an underline under it.
// underlineY is the baseline of that rule. Underlining the whole run is enough
// to read as provisional; per-clause styling (the protocol's cursor range) is
// not drawn yet.
func (l *List) preedit(text string, x, y float64, st m3.TextStyle, color uint32, underlineY float64, clip tree.Rect) {
	if text == "" {
		return
	}
	w := textWidth(l, text, st)
	l.textIn(text, st, color, tree.Rect{X: x, Y: y, W: w, H: 26}, AlignStart, clip)
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:  [4]float32{float32(x), float32(underlineY), float32(w), 1},
		Color: color, Clip: clipArr(clip),
	})
}

// radiusFor resolves a shape scale value; ShapeFull becomes half the short side.
func radiusFor(v float64, r tree.Rect) float64 {
	if v == m3.ShapeFull || v < 0 {
		return minf(r.W, r.H) / 2
	}
	return v
}

// variant returns the component variant, falling back to def.
func variant(c *tree.Component, def string) string {
	if v := c.Variant.Get(); v != "" {
		return v
	}
	return def
}

// disabledAlpha applies the M3 disabled treatment to a foreground color.
func disabledAlpha(c *tree.Component, color uint32) uint32 {
	if c.Disabled.Get() {
		return withAlpha(color, m3.DisabledOpacity)
	}
	return color
}

// paintComponent dispatches to the per-component painter.
func (l *List) paintComponent(c *tree.Component, r tree.Rect, clip tree.Rect) {
	switch c.Node().Kind() {
	case "Button":
		l.paintButton(c, r, clip)
	case "IconButton":
		l.paintIconButton(c, r, clip)
	case "FAB":
		l.paintFAB(c, r, clip)
	case "FABMenu":
		l.paintFABMenu(c, r, clip)
	case "ButtonGroup":
		l.paintButtonGroup(c, r, clip)
	case "SplitButton":
		l.paintSplitButton(c, r, clip)
	case "SegmentedButton":
		l.paintSegmentedButton(c, r, clip)
	case "Chip":
		l.paintChip(c, r, clip)
	case "Badge":
		l.paintBadge(c, r, clip)
	case "Checkbox":
		l.paintCheckbox(c, r, clip)
	case "RadioButton":
		l.paintRadio(c, r, clip)
	case "Switch":
		l.paintSwitch(c, r, clip)
	case "Slider":
		l.paintSlider(c, r, clip)
	case "TextField":
		l.paintTextField(c, r, clip)
	case "SearchBar":
		l.paintSearchBar(c, r, clip)
	case "Tooltip":
		l.paintTooltip(c, r, clip)
	case "Card":
		l.paintCard(c, r, clip)
	case "Dialog":
		l.paintDialog(c, r, clip)
	case "BottomSheet":
		l.paintBottomSheet(c, r, clip)
	case "SideSheet":
		l.paintSideSheet(c, r, clip)
	case "Divider":
		l.paintDivider(c, r, clip)
	case "List":
		l.paintList(c, r, clip)
	case "ListItem":
		l.paintListItem(c, r, clip)
	case "Table":
		if t, ok := c.Node().(*tree.Table); ok {
			l.paintTable(t, r, clip)
		}
	case "LineChart", "AreaChart", "BarChart", "PieChart", "ScatterChart", "RadarChart", "GaugeChart":
		if ch, ok := c.Node().(interface{ ChartCore() *tree.Chart }); ok {
			l.paintChart(ch.ChartCore(), r, clip)
		}
	case "Map":
		if m, ok := c.Node().(*tree.Map); ok {
			l.paintMap(m, r, clip)
		}
	case "TableColumn":
		l.paintTableColumn(c, r, clip)
	case "TableRow":
		// The table paints the bands; a row's own box goes on top of them, so
		// `background` on a row wins over striped and selected.
		l.paintBox(c.Base(), r, clip)
	case "TableCell":
		l.paintTableCell(c, r, clip)
	case "Menu":
		l.paintMenu(c, r, clip)
	case "MenuItem":
		l.paintMenuItem(c, r, clip)
	case "Snackbar":
		l.paintSnackbar(c, r, clip)
	case "TopAppBar":
		l.paintTopAppBar(c, r, clip)
	case "NavigationBar":
		l.paintNavigationBar(c, r, clip)
	case "NavigationRail":
		l.paintNavigationRail(c, r, clip)
	case "NavigationDrawer":
		l.paintNavigationDrawer(c, r, clip)
	case "NavigationItem":
		l.paintNavigationItem(c, r, clip)
	case "Breadcrumb":
		l.paintBreadcrumb(c, r, clip)
	case "Tabs":
		l.paintTabs(c, r, clip)
	case "Tab":
		l.paintTab(c, r, clip)
	case "LinearProgress":
		l.paintLinearProgress(c, r, clip)
	case "CircularProgress":
		l.paintCircularProgress(c, r, clip)
	case "LoadingIndicator":
		l.paintLoadingIndicator(c, r, clip)
	case "Icon":
		l.paintIcon(c, r, clip)
	case "Skeleton":
		l.paintSkeleton(c, r, clip)
	case "DatePicker":
		l.paintDatePicker(c, r, clip)
	case "TimePicker":
		l.paintTimePicker(c, r, clip)
	case "ColorPicker":
		l.paintColorPicker(c, r, clip)
	case "TextArea":
		l.paintTextArea(c, r, clip)
	case "Code":
		l.paintCode(c, r, clip)
	case "Terminal":
		l.paintTerminal(c, r, clip)
	case "Mermaid":
		l.paintMermaid(c, r, clip)
	case "Carousel":
		l.paintCarousel(c, r, clip)
	case "CarouselItem":
		l.paintCarouselItem(c, r, clip)
	case "Toolbar":
		l.paintToolbar(c, r, clip)
	}
	// Keyboard focus gets an M3 outline. A text field draws its own focus
	// border, so it is the one kind that skips this. A pointer press focuses the
	// node too but does not set FocusRing, so clicking never draws the outline.
	if c.FocusRing {
		switch c.Node().Kind() {
		case "TextField", "SearchBar":
		default:
			l.focusRing(c, r, clip)
		}
	}
}

// focusRing draws the 2dp outline just outside the node, following its corner
// radius. M3 offsets the indicator by 3dp.
func (l *List) focusRing(c *tree.Component, r tree.Rect, clip tree.Rect) {
	const gap, width = 3.0, 2.0
	rad := l.shapeRadius(c.Base(), r)
	l.fillRect(
		tree.Rect{X: r.X - gap, Y: r.Y - gap, W: r.W + 2*gap, H: r.H + 2*gap},
		0, rad+gap, l.Theme.Colors.Primary, width, clip,
	)
}

// ---------------------------------------------------------------- buttons

// buttonColors returns container, content, border and elevation for a variant.
func (l *List) buttonColors(c *tree.Component) (bg, fg, border uint32, elev int, bw float64) {
	col := l.Theme.Colors
	switch variant(c, "filled") {
	case "tonal":
		return col.SecondaryContainer, col.OnSecondaryContainer, 0, 0, 0
	case "elevated":
		return col.SurfaceContainerLow, col.Primary, 0, 1, 0
	case "outlined":
		return 0, col.Primary, col.Outline, 0, 1
	case "text":
		return 0, col.Primary, 0, 0, 0
	default: // filled
		return col.Primary, col.OnPrimary, 0, 0, 0
	}
}

func (l *List) paintButton(c *tree.Component, r tree.Rect, clip tree.Rect) {
	bg, fg, border, elev, bw := l.buttonColors(c)
	col := l.Theme.Colors
	radius := radiusFor(m3.ShapeFull, r)
	if themeRadius := r.H / 2; themeRadius > 0 {
		radius = themeRadius
	}
	// The design may style the box itself: a background, a border and a radius
	// replace the variant's, and color replaces the label's foreground.
	radii := componentRadii(c.Base(), radius)
	bg, gradient := componentFill(c.Base(), bg)
	if v := componentColor(c); v != 0 {
		fg = v
	}
	if c.Disabled.Get() {
		// M3 dims the label against the page, not against the container's
		// content colour: onSurface at 38% over onSurface at 12%. Dimming
		// OnPrimary instead left white at 38% on a pale container, so the label
		// nearly vanished. The 38% comes from disabledAlpha below; applying it
		// here as well would multiply into 14%.
		bg = withAlpha(col.OnSurface, m3.DisabledContainerOpacity)
		border = withAlpha(col.OnSurface, m3.DisabledContainerOpacity)
		fg = col.OnSurface
	}
	border, bw = componentBorder(c.Base(), border, bw)
	l.shadowRadii(r, radii32(radii), elev, clip)
	l.fillShape(r, bg, gradient, radii, border, bw, clip)
	if !c.Disabled.Get() {
		l.ripple(c, r, radii[0], fg, clip)
		l.tint(c, r, radii32(radii), fg, clip)
	}

	labelW := r.W
	if c.Icon.Get() != "" {
		labelW -= 26
	}
	box := tree.Rect{X: r.X, Y: r.Y, W: labelW, H: r.H}
	if c.Icon.Get() != "" {
		// Icon then label, as one centred pair.
		labelWidth := textWidth(l, c.Label.Get(), l.style("labelLarge")) + 8 + 18
		start := r.X + (r.W-labelWidth)/2
		l.leadingIconIn(c.Icon.Get(), 18, disabledAlpha(c, fg), start+9, r.Y+r.H/2, clip)
		l.textIn(c.Label.Get(), l.style("labelLarge"), disabledAlpha(c, fg),
			tree.Rect{X: start + 18 + 8, Y: r.Y, W: labelWidth - 26, H: r.H}, AlignStart, clip)
		return
	}
	l.textIn(c.Label.Get(), l.style("labelLarge"), disabledAlpha(c, fg), box, AlignCenter, clip)
}

func (l *List) paintIconButton(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	radius := radiusFor(m3.ShapeFull, r)
	var icon uint32
	switch variant(c, "standard") {
	case "filled":
		bg := col.Primary
		if c.Selected.Get() {
			bg = col.Primary
		}
		if c.Disabled.Get() {
			bg = withAlpha(bg, m3.DisabledContainerOpacity)
		}
		l.fillRect(r, bg, radius, 0, 0, clip)
		icon = disabledAlpha(c, col.OnPrimary)
	case "tonal":
		bg := col.SecondaryContainer
		if c.Selected.Get() {
			bg = col.SecondaryContainer
		}
		if c.Disabled.Get() {
			bg = withAlpha(bg, m3.DisabledContainerOpacity)
		}
		l.fillRect(r, bg, radius, 0, 0, clip)
		icon = disabledAlpha(c, col.OnSecondaryContainer)
	case "outlined":
		// A disabled outlined button dims its border with its glyph; leaving the
		// border solid made the control read as active next to a dimmed icon.
		l.fillRect(r, 0, radius, disabledAlpha(c, col.Outline), 1, clip)
		icon = disabledAlpha(c, col.OnSurfaceVariant)
	default:
		if c.Selected.Get() {
			l.fillRect(r, col.SecondaryContainer, radius, 0, 0, clip)
			icon = disabledAlpha(c, col.OnSecondaryContainer)
		} else {
			icon = disabledAlpha(c, col.OnSurfaceVariant)
		}
	}
	if ib, ok := c.Node().(*tree.IconButton); ok {
		if v := ib.IconColor.Get(); v != 0 {
			icon = disabledAlpha(c, v)
		}
	}
	l.stateLayer(c, r, radius, icon, clip)
	l.iconIn(c.Icon.Get(), 24, icon, r.X+r.W/2, r.Y+r.H/2, clip)
}

func (l *List) paintFAB(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	size := variant(c, "md")
	radius := m3.ShapeLarge
	elev := m3.Elevation3
	iconSize := 24.0
	switch size {
	case "sm", "small":
		radius = m3.ShapeMedium
		iconSize = 24
		elev = m3.Elevation3
	case "lg", "large":
		radius = m3.ShapeExtraLarge
		iconSize = 36
		elev = m3.Elevation3
	}
	bg := col.PrimaryContainer
	fg := col.OnPrimaryContainer
	if variantColor := variant(c, ""); variantColor == "secondary" {
		bg, fg = col.SecondaryContainer, col.OnSecondaryContainer
	} else if variantColor == "tertiary" {
		bg, fg = col.TertiaryContainer, col.OnTertiaryContainer
	}
	if c.Disabled.Get() {
		// Same rule as Button: the container and the icon both come from
		// onSurface, at 12% and 38%.
		bg = withAlpha(col.OnSurface, m3.DisabledContainerOpacity)
		fg = withAlpha(col.OnSurface, m3.DisabledOpacity)
	}
	l.shadow(r, radius, elev, clip)
	l.fillRect(r, bg, radius, 0, 0, clip)
	l.stateLayer(c, r, radius, fg, clip)

	if c.Label.Get() != "" {
		// Extended FAB: 20dp of padding, then the icon and a 12dp gap when
		// there is one, then the label, then 20dp again. The metrics use the
		// same numbers, so the box hugs its content.
		x := r.X + 20
		if c.Icon.Get() != "" {
			l.iconIn(c.Icon.Get(), 24, fg, x+12, r.Y+r.H/2, clip)
			x += 24 + 12
		}
		l.textIn(c.Label.Get(), l.style("labelLarge"), fg,
			tree.Rect{X: x, Y: r.Y, W: r.Right() - 20 - x, H: r.H}, AlignStart, clip)
		return
	}
	l.iconIn(c.Icon.Get(), iconSize, fg, r.X+r.W/2, r.Y+r.H/2, clip)
}

func (l *List) paintFABMenu(c *tree.Component, r tree.Rect, clip tree.Rect) {
	// Only the FAB is drawn here: layout stacks the actions above it, and they
	// paint themselves. While the menu is closed the rect is the FAB.
	const fab = 56.0
	side := minf(minf(r.W, r.H), fab)
	if side <= 0 {
		return
	}
	box := tree.Rect{X: r.Right() - side, Y: r.Bottom() - side, W: side, H: side}
	col := l.Theme.Colors
	bg, fg := col.PrimaryContainer, col.OnPrimaryContainer
	switch variant(c, "") {
	case "secondary":
		bg, fg = col.SecondaryContainer, col.OnSecondaryContainer
	case "tertiary":
		bg, fg = col.TertiaryContainer, col.OnTertiaryContainer
	}
	if c.Disabled.Get() {
		bg = withAlpha(bg, m3.DisabledContainerOpacity)
		fg = withAlpha(fg, m3.DisabledOpacity)
	}
	radius := radiusFor(m3.ShapeLarge, box)
	l.shadow(box, radius, m3.Elevation3, clip)
	l.fillRect(box, bg, radius, 0, 0, clip)
	l.iconIn(c.Icon.Get(), 24, fg, box.X+box.W/2, box.Y+box.H/2, clip)
}

func (l *List) paintButtonGroup(c *tree.Component, r tree.Rect, clip tree.Rect) {
	// Children are the buttons; the group only provides spacing and clipping.
	_ = c
	_ = r
	_ = clip
}

func (l *List) paintSplitButton(c *tree.Component, r tree.Rect, clip tree.Rect) {
	bg, fg, border, elev, bw := l.buttonColors(c)

	// Expressive keeps two segments apart with a 2dp gap. Outer ends stay full
	// and inner corners are only lightly rounded, so the pair reads as one
	// control that is split, not one pill with a line. The radii never change
	// with hover or press: only the state layer moves.
	const (
		gap   = 2.0
		menuW = 48.0
	)
	mainW, trailW := r.W-menuW-gap, menuW
	if mainW < 48 {
		mainW = (r.W - gap) / 2
		trailW = mainW
	}
	main := tree.Rect{X: r.X, Y: r.Y, W: mainW, H: r.H}
	trail := tree.Rect{X: r.X + mainW + gap, Y: r.Y, W: trailW, H: r.H}
	outer := float32(r.H / 2)
	inner := float32(4)
	segs := []struct {
		r     tree.Rect
		radii [4]float32
	}{
		{main, [4]float32{outer, inner, inner, outer}},
		{trail, [4]float32{inner, outer, outer, inner}},
	}
	for _, s := range segs {
		l.shadowRadii(s.r, s.radii, elev, clip)
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect, Rect: rectArr(s.r),
			Radius: s.radii, Color: bg,
			BorderColor: border, BorderThickness: float32(bw),
			Clip: clipArr(clip),
		})
	}
	// Ripple starts in the segment under the pointer; the shared click state
	// tints both, since the framework exposes one button here.
	if !c.Disabled.Get() {
		hit := main
		if c.RippleX >= trail.X {
			hit = trail
		}
		l.ripple(c, hit, r.H/2, fg, clip)
		for _, s := range segs {
			l.tint(c, s.r, s.radii, fg, clip)
		}
	}

	if c.Icon.Get() != "" {
		// Icon then label, as one centred pair inside the leading segment.
		labelWidth := textWidth(l, c.Label.Get(), l.style("labelLarge")) + 8 + 18
		start := main.X + (main.W-labelWidth)/2
		l.leadingIconIn(c.Icon.Get(), 18, fg, start+9, r.Y+r.H/2, clip)
		l.textIn(c.Label.Get(), l.style("labelLarge"), fg,
			tree.Rect{X: start + 18 + 8, Y: r.Y, W: labelWidth - 26, H: r.H}, AlignStart, clip)
	} else {
		l.textIn(c.Label.Get(), l.style("labelLarge"), fg, main, AlignCenter, clip)
	}
	trailIcon := c.Trailing.Get()
	if trailIcon == "" {
		trailIcon = "arrow_drop_down"
	}
	l.trailingIconIn(trailIcon, 24, fg, trail.X+trail.W/2, r.Y+r.H/2, clip)
}

func (l *List) paintSegmentedButton(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	bg := uint32(0)
	fg := disabledAlpha(c, col.OnSurface)
	if c.Selected.Get() {
		bg = col.SecondaryContainer
		fg = disabledAlpha(c, col.OnSecondaryContainer)
	}
	// The group rounds only its outer ends: the first and last segment of the
	// contiguous SegmentedButton run in a parent. Siblings inside the run keep
	// square corners so a border between them reads as one divider.
	left, right := segmentEdges(c)
	radii := [4]float32{}
	if left {
		radii[0], radii[3] = float32(r.H/2), float32(r.H/2)
	}
	if right {
		radii[1], radii[2] = float32(r.H/2), float32(r.H/2)
	}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect, Rect: rectArr(r),
		Radius:      radii,
		Color:       bg,
		BorderColor: col.Outline, BorderThickness: 1,
		Clip: clipArr(clip),
	})
	l.ripple(c, r, 0, fg, clip)
	l.tint(c, r, radii, fg, clip)

	labelW := 0.0
	if c.Label.Get() != "" {
		labelW = textWidth(l, c.Label.Get(), l.style("labelLarge"))
	}
	iconW := 0.0
	if c.Selected.Get() {
		iconW = 26
	} else if c.Icon.Get() != "" {
		iconW = 26
	}
	start := r.X + (r.W-labelW-iconW)/2
	if iconW > 0 {
		name := c.Icon.Get()
		if c.Selected.Get() && name == "" {
			name = "check"
		}
		l.leadingIconIn(name, 18, fg, start+9, r.Y+r.H/2, clip)
	}
	if labelW > 0 {
		l.textIn(c.Label.Get(), l.style("labelLarge"), fg,
			tree.Rect{X: start + iconW, Y: r.Y, W: labelW + 4, H: r.H}, AlignStart, clip)
	}
}

// segmentEdges reports whether a segmented button starts or ends a run of
// segments in its parent, so only the run's outer ends get full radius.
func segmentEdges(c *tree.Component) (left, right bool) {
	p := c.Base().Parent()
	if p == nil {
		return true, true
	}
	kids := p.Base().Kids()
	idx := -1
	for i, k := range kids {
		if k == c.Base() {
			idx = i
			break
		}
	}
	if idx < 0 {
		return true, true
	}
	return idx == 0 || kids[idx-1].Node().Kind() != "SegmentedButton",
		idx == len(kids)-1 || kids[idx+1].Node().Kind() != "SegmentedButton"
}

func (l *List) paintChip(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	radius := radiusFor(m3.ShapeSmall, r)
	fg := col.OnSurfaceVariant
	bg := uint32(0)
	border := col.OutlineVariant
	bw := 1.0
	switch variant(c, "assist") {
	case "filter":
		if c.Selected.Get() {
			bg, fg, border, bw = col.SecondaryContainer, col.OnSecondaryContainer, 0, 0
		}
	case "input", "suggestion":
		if c.Selected.Get() {
			bg, fg, border, bw = col.SecondaryContainer, col.OnSecondaryContainer, 0, 0
		}
	}
	if c.Disabled.Get() {
		fg = withAlpha(fg, m3.DisabledOpacity)
		border = withAlpha(border, m3.DisabledContainerOpacity)
	}
	l.fillRect(r, bg, radius, border, bw, clip)
	l.stateLayer(c, r, radius, fg, clip)
	iconColor := fg
	if v := c.LeadingIconColor.Get(); v != 0 {
		iconColor = disabledAlpha(c, v)
	}
	labelColor := fg
	if v := c.LabelColor.Get(); v != 0 {
		labelColor = disabledAlpha(c, v)
	}
	trailColor := fg
	if v := c.TrailingIconColor.Get(); v != 0 {
		trailColor = disabledAlpha(c, v)
	}

	// A leading icon sits 8dp from the edge with an 8dp gap, which pushes the
	// label 18dp further in than the plain 16dp padding.
	x := r.X + 16
	lead := c.Icon.Get()
	if c.Selected.Get() && lead == "" {
		lead = "check"
	}
	if lead != "" {
		l.leadingIconIn(lead, 18, iconColor, r.X+17, r.Y+r.H/2, clip)
		x = r.X + 8 + 18 + 8
	}
	trail := c.Trailing.Get()
	endPad := 16.0
	if trail != "" {
		endPad = 8 + 18 + 8
	}
	l.textIn(c.Label.Get(), l.style("labelLarge"), labelColor,
		tree.Rect{X: x, Y: r.Y, W: r.Right() - x - endPad, H: r.H}, AlignStart, clip)
	if trail != "" {
		l.trailingIconIn(trail, 18, trailColor, r.Right()-17, r.Y+r.H/2, clip)
	}
}

func (l *List) paintBadge(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	if c.Label.Get() == "" {
		// Small dot.
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: Circle, Rect: rectArr(r),
			Color: col.Error, Clip: clipArr(clip),
		})
		return
	}
	l.fillRect(r, col.Error, radiusFor(m3.ShapeFull, r), 0, 0, clip)
	l.textIn(c.Label.Get(), l.style("labelSmall"), col.OnError, r, AlignCenter, clip)
}

// ---------------------------------------------------------------- selection

func (l *List) paintCheckbox(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	box := minf(r.H, 18)
	if box <= 0 {
		box = 18
	}
	// The box hugs the left edge; the label follows immediately.
	br := tree.Rect{X: r.X, Y: r.Y + (r.H-box)/2, W: box, H: box}
	if c.Checked.Get() {
		bg := col.Primary
		mark := col.OnPrimary
		if c.Disabled.Get() {
			bg = withAlpha(bg, m3.DisabledContainerOpacity)
			mark = withAlpha(mark, m3.DisabledOpacity)
		}
		l.fillRect(br, bg, 2, 0, 0, clip)
		l.iconIn("check", box, mark, br.X+box/2, br.Y+box/2, clip)
	} else {
		border := col.OnSurfaceVariant
		if c.Disabled.Get() {
			border = withAlpha(border, m3.DisabledOpacity)
		}
		l.fillRect(br, 0, 2, border, 2, clip)
	}
	if c.Label.Get() != "" {
		l.textIn(c.Label.Get(), l.style("bodyLarge"), disabledAlpha(c, col.OnSurface),
			tree.Rect{X: br.Right() + 12, Y: r.Y, W: r.Right() - br.Right() - 12, H: r.H}, AlignStart, clip)
	}
}

func (l *List) paintRadio(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	size := minf(r.H, 20)
	if size <= 0 {
		size = 20
	}
	br := tree.Rect{X: r.X, Y: r.Y + (r.H-size)/2, W: size, H: size}
	cx, cy := br.X+size/2, br.Y+size/2
	if c.Checked.Get() {
		l.Ops = append(l.Ops, Op{Kind: Fill, Shape: Circle, Rect: rectArr(br), Color: col.Primary, Clip: clipArr(clip)})
		dot := size * 0.5
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: Circle,
			Rect:  [4]float32{float32(cx - dot/2), float32(cy - dot/2), float32(dot), float32(dot)},
			Color: col.OnPrimary, Clip: clipArr(clip),
		})
	} else {
		l.fillRect(br, 0, size/2, col.OnSurfaceVariant, 2, clip)
	}
	if c.Label.Get() != "" {
		l.textIn(c.Label.Get(), l.style("bodyLarge"), disabledAlpha(c, col.OnSurface),
			tree.Rect{X: br.Right() + 12, Y: r.Y, W: r.Right() - br.Right() - 12, H: r.H}, AlignStart, clip)
	}
}

// thumbIconColor is the glyph on the thumb: Primary on the checked OnPrimary
// thumb, Surface (white in the light scheme) on the unchecked Outline thumb, so
// the glyph reads as a cut-out of the track at both ends.
func thumbIconColor(c *tree.Component, on bool, col m3.Colors) uint32 {
	if c.Disabled.Get() {
		return withAlpha(col.OnSurfaceVariant, m3.DisabledOpacity)
	}
	if on {
		return col.Primary
	}
	return col.Surface
}

func (l *List) paintSwitch(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	// Track is 52x32, the whole row may be taller.
	trackW, trackH := 52.0, 32.0
	tr := tree.Rect{X: r.X, Y: r.Y + (r.H-trackH)/2, W: trackW, H: trackH}
	on := c.Checked.Get()
	// pos is where the handle is drawn: 0 off, 1 on, and anything between while
	// the ui layer is animating a toggle.
	pos := 0.0
	if on {
		pos = 1
	}
	if c.TogglePosed {
		pos = c.TogglePos
	}

	track := blend(col.SurfaceContainerHighest, col.Primary, pos)
	thumb := blend(col.Outline, col.OnPrimary, pos)
	if c.Disabled.Get() {
		track = withAlpha(track, m3.DisabledContainerOpacity)
		thumb = withAlpha(thumb, m3.DisabledOpacity)
	}
	l.fillRect(tr, track, trackH/2, 0, 0, clip)
	if pos < 1 && !c.Disabled.Get() {
		// The outline fades as the track fills in.
		l.fillRect(tr, 0, trackH/2, col.Outline, 2*(1-pos), clip)
	}

	// A glyph in the thumb needs room, so a switch with an icon keeps the
	// checked thumb size in both states and draws the icon at 16dp inside it.
	icon := c.Icon.Get()
	if pos >= 0.5 {
		if v := c.IconOn.Get(); v != "" {
			icon = v
		}
	} else if v := c.IconOff.Get(); v != "" {
		icon = v
	}
	hasGlyph := c.Icon.Get() != "" || c.IconOn.Get() != "" || c.IconOff.Get() != ""
	// The thumb grows from 16 to 24dp as it travels, with 8dp of inset when it
	// is small and 4dp when it is large; a glyph needs the large size in both
	// states. Since inset+thumbD/2 is 16 at either end, the centre only depends
	// on how far along the travel is.
	thumbD := 16 + 8*pos
	if hasGlyph {
		thumbD = 24
	}
	cx := tr.X + 16 + (trackW-32)*pos
	l.stateLayer(c, tr, trackH/2, thumb, clip)
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Circle,
		Rect:  [4]float32{float32(cx - thumbD/2), float32(tr.Y + (trackH-thumbD)/2), float32(thumbD), float32(thumbD)},
		Color: thumb, Clip: clipArr(clip),
	})
	if icon != "" {
		// The glyph belongs to one side, so it fades out and back in as the
		// handle crosses the middle instead of swapping abruptly.
		alpha := 1.0
		if c.TogglePosed {
			alpha = math.Abs(2*pos - 1)
		}
		l.iconIn(icon, 16, withAlpha(thumbIconColor(c, on, col), alpha), cx, tr.Y+trackH/2, clip)
	}

	if c.Label.Get() != "" {
		l.textIn(c.Label.Get(), l.style("bodyLarge"), disabledAlpha(c, col.OnSurface),
			tree.Rect{X: tr.Right() + 12, Y: r.Y, W: r.Right() - tr.Right() - 12, H: r.H}, AlignStart, clip)
	}
}

func (l *List) paintSlider(c *tree.Component, r tree.Rect, clip tree.Rect) {
	s, _ := c.Node().(*tree.Slider)
	vertical := s != nil && s.Orientation.Get() == "vertical"
	col := l.Theme.Colors
	const (
		inset       = 2.0 // half the handle, so the handle touches the bounds
		handleMain  = 4.0
		handleCross = 44.0
		gap         = 6.0
		stopSize    = 4.0
		iconPad     = 6.0
		iconMax     = 28.0
		thinTrack   = 16.0
		iconTrack   = 40.0
	)
	leading, trailing := c.Icon.Get(), c.Trailing.Get()
	hasIcon := leading != "" || trailing != ""
	cross, main := r.H, r.W
	if vertical {
		cross, main = r.W, r.H
	}
	if cross <= 0 || main <= 2*inset {
		return
	}
	trackCross := thinTrack
	if hasIcon {
		// The track grows only when there is a glyph to hold, so a slider
		// without icons keeps the M3 XS shape.
		trackCross = math.Min(iconTrack, cross)
	}
	trackMain := main - 2*inset
	var track tree.Rect
	if vertical {
		track = tree.Rect{X: r.X + (r.W-trackCross)/2, Y: r.Y + inset, W: trackCross, H: trackMain}
	} else {
		track = tree.Rect{X: r.X + inset, Y: r.Y + (r.H-trackCross)/2, W: trackMain, H: trackCross}
	}
	crossCenter := track.Y + track.H/2
	if vertical {
		crossCenter = track.X + track.W/2
	}
	// pos maps a distance from the leading end (left or bottom) to the screen.
	pos := func(p float64) float64 {
		if vertical {
			return track.Bottom() - p
		}
		return track.X + p
	}
	point := func(p float64) (float64, float64) {
		if vertical {
			return crossCenter, pos(p)
		}
		return pos(p), crossCenter
	}

	// The outer corners follow the track's shape. Without icons the track is a
	// capsule; with icons it keeps a small radius, or the design's own.
	outer := [4]float64{}
	if c.Base().RadiusSet() {
		outer = c.Base().Radii()
	} else if !hasIcon {
		rad := trackCross / 2
		outer = [4]float64{rad, rad, rad, rad}
	} else {
		rad := math.Min(8, trackCross/2)
		outer = [4]float64{rad, rad, rad, rad}
	}
	seam := [4]float64{2, 2, 2, 2}
	active := col.Primary
	inactive := col.SecondaryContainer
	handle := col.Primary
	stopColor := active
	if c.Disabled.Get() {
		active = withAlpha(col.OnSurface, m3.DisabledOpacity)
		inactive = withAlpha(col.OnSurface, m3.DisabledContainerOpacity)
		handle = withAlpha(col.OnSurface, m3.DisabledOpacity)
		stopColor = active
	}
	bar := func(lo, hi float64, color uint32) {
		if hi-lo <= 0 {
			return
		}
		radii := seam
		if lo <= 0.01 {
			if vertical {
				radii[2], radii[3] = outer[2], outer[3]
			} else {
				radii[0], radii[3] = outer[0], outer[3]
			}
		}
		if hi >= trackMain-0.01 {
			if vertical {
				radii[0], radii[1] = outer[0], outer[1]
			} else {
				radii[1], radii[2] = outer[1], outer[2]
			}
		}
		var box tree.Rect
		if vertical {
			box = tree.Rect{X: track.X, Y: pos(hi), W: track.W, H: pos(lo) - pos(hi)}
		} else {
			box = tree.Rect{X: pos(lo), Y: track.Y, W: pos(hi) - pos(lo), H: track.H}
		}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect,
			Rect: rectArr(box), Radius: radiusArr(radii, box),
			Color: color, Clip: clipArr(clip),
		})
	}
	dot := func(leadingEnd bool) {
		p := trackCross / 2
		if !leadingEnd {
			p = trackMain - p
		}
		x, y := point(p)
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: Circle,
			Rect: [4]float32{
				float32(x - stopSize/2), float32(y - stopSize/2),
				stopSize, stopSize,
			},
			Color: stopColor, Clip: clipArr(clip),
		})
	}

	lo, hi := c.Min.Get(), c.Max.Get()
	fracOf := func(v float64) float64 {
		if hi <= lo {
			return 0
		}
		return clamp01((v - lo) / (hi - lo))
	}
	frac := fracOf(c.Value.Get())

	handleAt := func(p float64, grabbed bool) {
		thick := handleMain
		if c.Focused || (c.Pressed && grabbed) {
			thick = 2
		}
		x, y := point(p)
		var box tree.Rect
		if vertical {
			box = tree.Rect{X: x - handleCross/2, Y: y - thick/2, W: handleCross, H: thick}
		} else {
			box = tree.Rect{X: x - thick/2, Y: y - handleCross/2, W: thick, H: handleCross}
		}
		rad := float32(thick / 2)
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect,
			Rect: rectArr(box), Radius: [4]float32{rad, rad, rad, rad},
			Color: handle, Clip: clipArr(clip),
		})
	}
	pressed := func(i int) bool { return s != nil && s.Grab == i }
	// Every segment stops `gap` away from the handle edge, so the handle has
	// clear space around it.
	const edge = handleMain/2 + gap

	// aStart/aEnd are the active segment in main-axis distance. iLeftEnd and
	// iRightStart are the inactive segments at the two ends; the icons sit in
	// those caps until a thumb leaves too little room, then move into the fill.
	var aStart, aEnd, iLeftEnd, iRightStart float64
	tipP, tipV := math.NaN(), 0.0
	showTip := s != nil && s.ShowTooltip.Get() && !c.Disabled.Get() && (c.Hover || c.Pressed)

	switch variant(c, "standard") {
	case "range":
		if s == nil {
			return
		}
		loV, hiV := s.Range()
		a, b := fracOf(loV)*trackMain, fracOf(hiV)*trackMain
		iLeftEnd, iRightStart = a-edge, b+edge
		aStart, aEnd = a+edge, b-edge
		bar(0, a-edge, inactive)
		bar(a+edge, b-edge, active)
		bar(b+edge, trackMain, inactive)
		if leading == "" {
			dot(true)
		}
		if trailing == "" {
			dot(false)
		}
		handleAt(a, pressed(0))
		handleAt(b, pressed(1))
		if showTip {
			high := s.HoverGrab == 1
			if c.Pressed {
				high = s.Grab == 1
			}
			if high {
				tipP, tipV = b, hiV
			} else {
				tipP, tipV = a, loV
			}
		}

	case "centered":
		cf := 0.5
		if s != nil && !math.IsNaN(s.Center.Get()) {
			cf = fracOf(s.Center.Get())
		}
		cP, hP := cf*trackMain, frac*trackMain
		if hP > cP {
			iLeftEnd, iRightStart = cP, hP+edge
			aStart, aEnd = cP, hP-edge
			bar(0, cP, inactive)
			bar(aStart, aEnd, active)
			bar(hP+edge, trackMain, inactive)
		} else {
			iLeftEnd, iRightStart = hP-edge, cP
			aStart, aEnd = hP+edge, cP
			bar(0, hP-edge, inactive)
			bar(aStart, aEnd, active)
			bar(cP, trackMain, inactive)
		}
		if leading == "" {
			dot(true)
		}
		if trailing == "" {
			dot(false)
		}
		handleAt(hP, pressed(0))
		if showTip {
			tipP, tipV = hP, c.Value.Get()
		}

	default:
		hP := frac * trackMain
		iLeftEnd, iRightStart = 0, hP+edge
		aStart, aEnd = 0, hP-edge
		bar(0, aEnd, active)
		bar(hP+edge, trackMain, inactive)
		if trailing == "" {
			dot(false)
		}
		handleAt(hP, pressed(0))
		if showTip {
			tipP, tipV = hP, c.Value.Get()
		}
	}

	if hasIcon {
		size := math.Max(4, math.Min(iconMax, trackCross-2*iconPad))
		iconOnInactive := col.OnSecondaryContainer
		iconOnActive := col.OnPrimary
		if c.Disabled.Get() {
			iconOnInactive = withAlpha(col.OnSurface, m3.DisabledOpacity)
			iconOnActive = iconOnInactive
		}
		place := func(name string, leadingEnd bool) {
			if name == "" {
				return
			}
			var center float64
			if leadingEnd {
				if iLeftEnd >= size+2*iconPad {
					center = iconPad + size/2
				} else {
					center = aStart + iconPad + size/2
				}
			} else {
				if trackMain-iRightStart >= size+2*iconPad {
					center = trackMain - iconPad - size/2
				} else {
					center = aEnd - iconPad - size/2
				}
			}
			center = math.Max(size/2, math.Min(trackMain-size/2, center))
			// A moved icon is on the fill only when there is a fill under it;
			// at a standard slider's minimum the active interval is empty.
			onActive := aEnd > aStart && center >= aStart && center <= aEnd
			color := iconOnInactive
			if onActive {
				color = iconOnActive
			}
			x, y := point(center)
			l.iconIn(name, size, color, x, y, clip)
		}
		place(leading, true)
		place(trailing, false)
	}

	if showTip && !math.IsNaN(tipP) {
		x, y := point(tipP)
		l.paintSliderTooltip(x, y, vertical, tipV, clip)
	}
}

// paintSliderTooltip draws the M3 value indicator: a rounded bubble above a
// horizontal handle or beside a vertical one, kept inside the clip.
func (l *List) paintSliderTooltip(cx, cy float64, vertical bool, v float64, clip tree.Rect) {
	col := l.Theme.Colors
	st := l.style("labelLarge")
	const padX, h, gap, half = 12.0, 32.0, 6.0, 22.0
	txt := sliderValueText(v)
	w := math.Max(textWidth(l, txt, st)+padX*2, h)
	var box tree.Rect
	if vertical {
		box = tree.Rect{X: cx + half + gap, Y: cy - h/2, W: w, H: h}
		if box.Right() > clip.Right()-2 {
			box.X = cx - half - gap - w
		}
		if box.X < clip.X+2 {
			box.X = clip.X + 2
		}
		if box.Y < clip.Y+2 {
			box.Y = clip.Y + 2
		}
		if box.Bottom() > clip.Bottom()-2 {
			box.Y = clip.Bottom() - h - 2
		}
	} else {
		x := cx - w/2
		if lo := clip.X + 2; x < lo {
			x = lo
		}
		if hi := clip.X + clip.W - w - 2; x > hi && hi > clip.X {
			x = hi
		}
		box = tree.Rect{X: x, Y: cy - half - gap - h, W: w, H: h}
	}
	l.fillRect(box, col.InverseSurface, h/2, 0, 0, clip)
	l.textIn(txt, st, col.InverseOnSurface, box, AlignCenter, clip)
}

// sliderValueText shows a slider number without float noise: the shortest
// spelling, rounded to four decimals.
func sliderValueText(v float64) string {
	if v == 0 {
		v = 0 // -0 prints as "-0"
	}
	return strconv.FormatFloat(math.Round(v*1e4)/1e4, 'f', -1, 64)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ---------------------------------------------------------------- text input

func (l *List) paintTextField(c *tree.Component, r tree.Rect, clip tree.Rect) {
	r = TextFieldBox(r, c.Supporting.Get() != "")
	col := l.Theme.Colors
	filled := variant(c, "filled") == "filled"
	fg := disabledAlpha(c, col.OnSurface)
	hint := disabledAlpha(c, col.OnSurfaceVariant)
	container := col.SurfaceContainerHighest
	border := col.Outline
	bw := 1.0
	if c.Disabled.Get() {
		container = withAlpha(container, m3.DisabledContainerOpacity)
		border = withAlpha(border, m3.DisabledContainerOpacity)
	}

	radii := componentRadii(c.Base(), m3.ShapeExtraSmall)
	// Label geometry: its travel also decides the notch in an outlined border.
	lead, trail := c.FieldGutters()
	lx := r.X + 16
	textW := r.W - 32
	if lead {
		lx += 40 // make room for the leading icon or slot
		textW -= 40
	}
	if trail {
		textW -= 40
	}
	labelStyle := l.style("bodyLarge")
	smallStyle := l.style("bodySmall")
	label := c.Label.Get()
	placeholder := c.Placeholder.Get()
	float := 0.0
	if c.LabelPosed {
		float = clamp01(c.LabelFloat)
	} else if c.LabelFloats() {
		float = 1 // a still frame shows the resting state, not frame zero
	}
	floatStyle := labelStyle
	floatStyle.Size += (smallStyle.Size - labelStyle.Size) * float
	labelCenter := r.Y + r.H/2
	floatCenter := r.Y + 16
	if !filled {
		floatCenter = r.Y // the floated label sits on the outline
	}
	labelCenter += (floatCenter - labelCenter) * float
	// An outlined border is cut behind a floating label, so the line does not
	// run through the glyphs.
	gapStart, gapEnd := 0.0, 0.0
	// The notch opens as the label's box reaches the line, so the border is not
	// cut early while the label is still inside. The corners stay whole: a large
	// radius can reach past the label's 16dp inset.
	band := maxf(maxf(radii[0], radii[1]), maxf(radii[2], radii[3]))
	band = maxf(band, bw)
	if !filled && label != "" && labelCenter-10 <= r.Y+bw {
		gapStart = lx - 4
		gapEnd = gapStart + textWidth(l, label, floatStyle) + 8
		gapStart = maxf(gapStart, r.X+band)
		gapEnd = minf(gapEnd, r.Right()-band)
	}
	if !filled {
		// An outlined field is transparent until the design gives it a fill, so
		// its own container colour is no fallback.
		container = 0
	}
	fill, gradient := componentFill(c.Base(), container)
	border, bw = componentBorder(c.Base(), border, bw)
	if filled {
		l.fillShape(r, fill, gradient, radii, 0, 0, clip)
		// A design border is the field's outline, so the M3 indicator stays
		// away from it; paintComponentBorder draws that border above the text.
		if !c.Base().BorderSet() {
			if c.Base().RadiusSet() {
				// A rounded field keeps a focus cue with a border instead: a square
				// indicator would cut the corners it now has.
				bc, bwid := col.Outline, 1.0
				if c.Focused {
					bc, bwid = col.Primary, 2
				}
				if c.Disabled.Get() {
					bc = withAlpha(bc, m3.DisabledContainerOpacity)
				}
				l.fillShape(r, 0, nil, radii, bc, bwid, clip)
			} else {
				// Active indicator along the bottom.
				ind := col.OnSurfaceVariant
				th := 1.0
				if c.Focused {
					ind, th = col.Primary, 2.0
				}
				l.Ops = append(l.Ops, Op{
					Kind: Fill, Shape: RoundedRect,
					Rect:  [4]float32{float32(r.X), float32(r.Bottom() - th), float32(r.W), float32(th)},
					Color: ind, Clip: clipArr(clip),
				})
			}
		}
	} else {
		if c.Focused && !c.Base().BorderSet() {
			border, bw = col.Primary, 2
		}
		if gapEnd > gapStart {
			l.notchedOutline(r, fill, gradient, radii, border, bw, gapStart, gapEnd, clip)
		} else {
			l.fillShape(r, fill, gradient, radii, border, bw, clip)
		}
	}
	if v := componentColor(c); v != 0 {
		fg = v
		hint = withAlpha(v, 0.7)
	}

	// A hidden field paints dots in place of the value, and the caret and the
	// selection follow them, so every width below is measured on the dots.
	obscure := c.Obscured()
	value := tree.FieldText(c.Text.Get(), obscure)
	hasValue := c.Text.Get() != "" || c.Preedit != "" || c.Focused

	// The value scrolls instead of running past the field edge: shift it left
	// just enough for the caret to stay visible, and keep every value glyph
	// inside the text box.
	c.TextScroll = 0
	if c.Focused {
		c.TextScroll = textShift(fieldCaretX(l, c, labelStyle), textW)
	}
	shift := c.TextScroll
	textClip := intersect(clip, tree.Rect{X: lx, Y: r.Y, W: textW, H: r.H})
	// A filled field keeps its value under the label band. An outlined label
	// lives on the border instead, so its value takes the middle of the box.
	valueTop, valueH := r.Y+22.0, 26.0
	if !filled {
		valueTop = r.Y + maxf((r.H-valueH)/2, 0)
	}

	// Selection sits under the value, in the same box as the caret.
	if c.Focused {
		if a, b, ok := c.Selection(); ok {
			a = tree.FieldIndex(c.Text.Get(), a, obscure)
			b = tree.FieldIndex(c.Text.Get(), b, obscure)
			x0 := lx - shift + textWidth(l, value[:a], labelStyle)
			x1 := lx - shift + textWidth(l, value[:b], labelStyle)
			l.Ops = append(l.Ops, Op{
				Kind: Fill, Shape: RoundedRect,
				Rect:   [4]float32{float32(x0), float32(valueTop + 4), float32(x1 - x0), 20},
				Radius: [4]float32{2, 2, 2, 2},
				Color:  withAlpha(col.Primary, m3.SelectionOpacity),
				Clip:   clipArr(textClip),
			})
		}
	}

	if hasValue {
		l.textIn(value, labelStyle, fg,
			tree.Rect{X: lx - shift, Y: valueTop, W: textW + shift, H: valueH}, AlignStart, textClip)
		// IME composition: inserted at the caret, underlined so it reads as
		// provisional. The caret then sits inside it, not after it.
		if c.Preedit != "" {
			n := tree.FieldIndex(c.Text.Get(), minInt(c.Caret, len(c.Text.Get())), obscure)
			px := lx - shift + textWidth(l, value[:n], labelStyle)
			l.preedit(tree.FieldText(c.Preedit, obscure), px, valueTop, labelStyle, fg, valueTop+22, textClip)
		}
	} else if placeholder != "" {
		l.textIn(placeholder, labelStyle, hint,
			tree.Rect{X: lx, Y: valueTop, W: textW, H: valueH}, AlignStart, textClip)
	}
	if label != "" {
		l.textIn(label, floatStyle, hint,
			tree.Rect{X: lx, Y: labelCenter - 10, W: textW, H: 20}, AlignStart, clip)
	}

	if c.Icon.Get() != "" {
		l.leadingIconIn(c.Icon.Get(), 24, hint, r.X+28, r.Y+r.H/2, clip)
	}
	if c.Trailing.Get() != "" {
		l.trailingIconIn(c.Trailing.Get(), 24, hint, r.Right()-28, r.Y+r.H/2, clip)
	}

	// Caret.
	if c.Focused {
		caretX := lx - shift + fieldCaretX(l, c, labelStyle)
		caretTop := valueTop + 4
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect,
			Rect:  [4]float32{float32(caretX), float32(caretTop), 2, 20},
			Color: col.Primary, Clip: clipArr(clip),
		})
	}
	if c.Supporting.Get() != "" {
		l.textIn(c.Supporting.Get(), l.style("bodySmall"), hint,
			tree.Rect{X: r.X + 16, Y: r.Bottom() + 4, W: r.W, H: 16}, AlignStart, clip)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (l *List) paintSearchBar(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	radius := radiusFor(m3.ShapeFull, r)
	l.shadow(r, radius, m3.Elevation3, clip)
	l.fillRect(r, col.SurfaceContainerHigh, radius, 0, 0, clip)
	fg := disabledAlpha(c, col.OnSurface)
	hint := col.OnSurfaceVariant
	// A leading body replaces the search glyph; the text still starts at the
	// same 52dp so the query does not jump when the slot appears.
	if sb, _ := c.Node().(*tree.SearchBar); sb == nil || sb.LeadingNode == nil {
		l.iconIn("search", 24, hint, r.X+28, r.Y+r.H/2, clip)
	}
	start := r.X + 52
	style := l.style("bodyLarge")
	// Same caret-follow scroll as a text field, so a long query does not run
	// under the trailing icon.
	c.TextScroll = 0
	if c.Focused {
		c.TextScroll = textShift(fieldCaretX(l, c, style), r.W-104)
	}
	shift := c.TextScroll
	// Glyphs stay inside the fixed text box; only the text itself moves, so a
	// long query slides under the leading icon instead of over it.
	textBox := tree.Rect{X: r.X + 52, Y: r.Y, W: r.W - 104, H: r.H}
	textClip := intersect(clip, textBox)
	start -= shift
	line := tree.Rect{X: start, Y: r.Y + r.H/2 - 10, W: r.W - 104 + shift, H: 20}

	// Selection sits under the value, in the same line box as the caret.
	if c.Focused {
		if a, b, ok := c.Selection(); ok {
			s := c.Text.Get()
			x0 := start + textWidth(l, s[:a], style)
			x1 := start + textWidth(l, s[:b], style)
			l.Ops = append(l.Ops, Op{
				Kind: Fill, Shape: RoundedRect,
				Rect:   [4]float32{float32(x0), float32(line.Y), float32(x1 - x0), 20},
				Radius: [4]float32{2, 2, 2, 2},
				Color:  withAlpha(col.Primary, m3.SelectionOpacity),
				Clip:   clipArr(textClip),
			})
		}
	}

	text := c.Text.Get()
	if text == "" {
		text = c.Placeholder.Get()
		if text == "" {
			text = c.Label.Get()
		}
		l.textIn(text, style, hint,
			tree.Rect{X: start, Y: r.Y, W: r.W - 104 + shift, H: r.H}, AlignStart, textClip)
	} else {
		l.textIn(text, style, fg,
			tree.Rect{X: start, Y: r.Y, W: r.W - 104 + shift, H: r.H}, AlignStart, textClip)
	}
	if c.Preedit != "" && c.Focused {
		value := c.Text.Get()
		n := minInt(c.Caret, len(value))
		px := start + textWidth(l, value[:n], style)
		l.preedit(c.Preedit, px, line.Y, style, fg, line.Y+22, textClip)
	}
	// Caret, so typing into the bar is visible. Search has no label band, so the
	// caret uses the full line height.
	if c.Focused {
		x := start + fieldCaretX(l, c, style)
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect,
			Rect:  [4]float32{float32(x), float32(line.Y), 2, 20},
			Color: col.Primary, Clip: clipArr(clip),
		})
	}
	if c.Trailing.Get() != "" {
		l.trailingIconIn(c.Trailing.Get(), 24, hint, r.Right()-28, r.Y+r.H/2, clip)
	}
}

// paintTooltip draws the hint above the child the tooltip wraps, on hover. It
// is the only component whose box is its content: the bubble floats outside it,
// so hovering the child is what brings it out and the layout never reserves
// room for text that is not there.
func (l *List) paintTooltip(c *tree.Component, r tree.Rect, clip tree.Rect) {
	if !c.Hover || c.Text.Get() == "" {
		return
	}
	col := l.Theme.Colors
	st := l.style("bodySmall")
	const padX, gap, h = 8.0, 6.0, 24.0
	w := textWidth(l, c.Text.Get(), st) + padX*2
	x := r.X + r.W/2 - w/2
	// A bubble that would leave the window slides back in, so a tooltip on a
	// control near the edge stays readable instead of being half cut off.
	if min := clip.X + 2; x < min {
		x = min
	}
	if max := clip.X + clip.W - w - 2; x > max && max > clip.X {
		x = max
	}
	y := r.Y - h - gap
	if y < clip.Y+2 {
		y = r.Y + r.H + gap
	}
	box := tree.Rect{X: x, Y: y, W: w, H: h}
	l.fillRect(box, col.InverseSurface, radiusFor(m3.ShapeExtraSmall, box), 0, 0, clip)
	l.textIn(c.Text.Get(), st, col.InverseOnSurface, box, AlignCenter, clip)
}

// ---------------------------------------------------------------- containment

func (l *List) paintCard(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	radius := m3.ShapeMedium
	fg, sub := col.OnSurface, col.OnSurfaceVariant
	switch variant(c, "elevated") {
	case "filled":
		bg := col.SurfaceContainerHighest
		if c.Disabled.Get() {
			bg = withAlpha(bg, m3.DisabledContainerOpacity)
			fg = withAlpha(fg, m3.DisabledOpacity)
			sub = withAlpha(sub, m3.DisabledOpacity)
		}
		l.fillRect(r, bg, radius, 0, 0, clip)
	case "outlined":
		border := col.OutlineVariant
		if c.Disabled.Get() {
			border = withAlpha(border, m3.DisabledContainerOpacity)
			fg = withAlpha(fg, m3.DisabledOpacity)
			sub = withAlpha(sub, m3.DisabledOpacity)
		}
		l.fillRect(r, col.Surface, radius, border, 1, clip)
	default: // elevated
		if !c.Disabled.Get() {
			l.shadow(r, radius, m3.Elevation1, clip)
		}
		bg := col.SurfaceContainerLow
		if c.Disabled.Get() {
			bg = withAlpha(bg, m3.DisabledContainerOpacity)
			fg = withAlpha(fg, m3.DisabledOpacity)
			sub = withAlpha(sub, m3.DisabledOpacity)
		}
		l.fillRect(r, bg, radius, 0, 0, clip)
	}
	if c.Label.Get() != "" {
		box := tree.Rect{X: r.X + 16, Y: r.Y + 16, W: r.W - 32, H: 24}
		l.textIn(c.Label.Get(), l.style("titleMedium"), fg, box, AlignStart, clip)
	}
	if c.Supporting.Get() != "" {
		l.textIn(c.Supporting.Get(), l.style("bodyMedium"), sub,
			tree.Rect{X: r.X + 16, Y: r.Y + 40, W: r.W - 32, H: 20}, AlignStart, clip)
	}
}

func (l *List) paintDialog(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	radius := m3.ShapeExtraLarge
	l.shadow(r, radius, m3.Elevation3, clip)
	l.fillRect(r, col.SurfaceContainerHigh, radius, 0, 0, clip)
	if c.Icon.Get() != "" {
		l.iconIn(c.Icon.Get(), 24, col.Secondary, r.X+r.W/2, r.Y+40, clip)
	}
	y := r.Y + 24.0
	if c.Icon.Get() != "" {
		y = r.Y + 64
	}
	if c.Label.Get() != "" {
		l.textIn(c.Label.Get(), l.style("headlineSmall"), col.OnSurface,
			tree.Rect{X: r.X + 24, Y: y, W: r.W - 48, H: 32}, AlignCenter, clip)
		y += 40
	}
	if c.Supporting.Get() != "" {
		l.textIn(c.Supporting.Get(), l.style("bodyMedium"), col.OnSurfaceVariant,
			tree.Rect{X: r.X + 24, Y: y, W: r.W - 48, H: 20}, AlignStart, clip)
	}
}

func (l *List) paintBottomSheet(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	// Only the top corners are rounded.
	rad := [4]float32{m3.ShapeExtraLarge, m3.ShapeExtraLarge, 0, 0}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect, Rect: rectArr(r), Radius: rad,
		Color: col.SurfaceContainerLow, Clip: clipArr(clip),
	})
	// Drag handle.
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:   [4]float32{float32(r.X + r.W/2 - 16), float32(r.Y + 22), 32, 4},
		Radius: [4]float32{2, 2, 2, 2},
		Color:  col.OnSurfaceVariant, Clip: clipArr(clip),
	})
	if c.Label.Get() != "" {
		l.textIn(c.Label.Get(), l.style("titleLarge"), col.OnSurface,
			tree.Rect{X: r.X + 24, Y: r.Y + 40, W: r.W - 48, H: 28}, AlignStart, clip)
	}
}

func (l *List) paintSideSheet(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	rad := [4]float32{0, m3.ShapeLarge, m3.ShapeLarge, 0}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect, Rect: rectArr(r), Radius: rad,
		Color: col.SurfaceContainerLow, Clip: clipArr(clip),
	})
	if c.Label.Get() != "" {
		l.textIn(c.Label.Get(), l.style("titleLarge"), col.OnSurface,
			tree.Rect{X: r.X + 16, Y: r.Y + 16, W: r.W - 32, H: 28}, AlignStart, clip)
	}
}

func (l *List) paintDivider(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	if c.Variant.Get() == "vertical" {
		w := r.W
		if w <= 0 {
			w = 1
		}
		l.fillRect(tree.Rect{X: r.X + (r.W-w)/2, Y: r.Y, W: w, H: r.H},
			col.OutlineVariant, 0, 0, 0, clip)
		return
	}
	h := r.H
	if h <= 0 {
		h = 1
	}
	l.fillRect(tree.Rect{X: r.X, Y: r.Y + (r.H-h)/2, W: r.W, H: h}, col.OutlineVariant, 0, 0, 0, clip)
}

func (l *List) paintList(c *tree.Component, r tree.Rect, clip tree.Rect) {
	// List items paint themselves; the list only clips.
	_ = c
	_ = r
	_ = clip
}

func (l *List) paintListItem(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	if c.Selected.Get() {
		l.fillRect(r, col.SecondaryContainer, 0, 0, 0, clip)
	}
	l.stateLayer(c, r, 0, col.OnSurface, clip)

	leading, trailing := "", ""
	var slots *tree.ListItem
	if li, ok := c.Node().(*tree.ListItem); ok {
		slots = li
	}
	leadColor, trailColor := col.OnSurfaceVariant, col.OnSurfaceVariant
	if v := c.LeadingIconColor.Get(); v != 0 {
		leadColor = v
	}
	if v := c.TrailingIconColor.Get(); v != 0 {
		trailColor = v
	}
	labelColor, supportColor := col.OnSurface, col.OnSurfaceVariant
	if v := c.LabelColor.Get(); v != 0 {
		labelColor = v
	}
	if v := c.SupportingColor.Get(); v != 0 {
		supportColor = v
	}

	// An explicit icon wins over the slot; the slot node is only laid out when
	// the icon is empty, so its space is reserved exactly when it draws.
	x := r.X + 16
	if c.Icon.Get() != "" {
		leading = c.Icon.Get()
		l.leadingIconIn(leading, 24, leadColor, x+12, r.Y+r.H/2, clip)
		x += 40
	} else if slots != nil && slots.LeadingNode != nil {
		x += 40
	}
	textW := r.Right() - x - 16
	if c.Trailing.Get() != "" {
		trailing = c.Trailing.Get()
		textW -= 40
		l.trailingIconIn(trailing, 24, trailColor, r.Right()-28, r.Y+r.H/2, clip)
	} else if slots != nil && slots.TrailingNode != nil {
		textW -= 40
	}

	fg := disabledAlpha(c, labelColor)
	if c.Supporting.Get() != "" {
		// Two line boxes centred as one block.
		top := r.Y + (r.H-44)/2
		l.textIn(c.Label.Get(), l.style("bodyLarge"), fg,
			tree.Rect{X: x, Y: top, W: textW, H: 24}, AlignStart, clip)
		l.textIn(c.Supporting.Get(), l.style("bodyMedium"), disabledAlpha(c, supportColor),
			tree.Rect{X: x, Y: top + 24, W: textW, H: 20}, AlignStart, clip)
		return
	}
	l.textIn(c.Label.Get(), l.style("bodyLarge"), fg,
		tree.Rect{X: x, Y: r.Y, W: textW, H: r.H}, AlignStart, clip)
}

func (l *List) paintMenu(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.shadow(r, m3.ShapeExtraSmall, m3.Elevation2, clip)
	l.fillRect(r, col.SurfaceContainer, m3.ShapeExtraSmall, 0, 0, clip)
}

func (l *List) paintMenuItem(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	if c.Selected.Get() {
		l.fillRect(r, col.SurfaceContainerHighest, 0, 0, 0, clip)
	}
	l.stateLayer(c, r, 0, col.OnSurface, clip)
	x := r.X + 12
	if c.Icon.Get() != "" {
		l.leadingIconIn(c.Icon.Get(), 24, col.OnSurfaceVariant, x+12, r.Y+r.H/2, clip)
		x += 36
	}
	textW := r.Right() - x - 12
	if c.Trailing.Get() != "" {
		textW -= 36
		l.trailingIconIn(c.Trailing.Get(), 24, col.OnSurfaceVariant, r.Right()-24, r.Y+r.H/2, clip)
	}
	l.textIn(c.Label.Get(), l.style("labelLarge"), disabledAlpha(c, col.OnSurface),
		tree.Rect{X: x, Y: r.Y, W: textW, H: r.H}, AlignStart, clip)
}

func (l *List) paintSnackbar(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.shadow(r, m3.ShapeExtraSmall, m3.Elevation3, clip)
	l.fillRect(r, col.InverseSurface, m3.ShapeExtraSmall, 0, 0, clip)
	l.textIn(c.Label.Get(), l.style("bodyMedium"), col.InverseOnSurface,
		tree.Rect{X: r.X + 16, Y: r.Y, W: r.W - 32, H: r.H}, AlignStart, clip)
}

// ---------------------------------------------------------------- navigation

func (l *List) paintTopAppBar(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.fillRect(r, col.Surface, 0, 0, 0, clip)
	// No leading icon means no navigation affordance: a wide window shows the
	// rail instead, so the bar drops the hamburger and starts the title at the
	// M3 edge inset rather than leaving a gap where the icon was.
	textX := r.X + 16
	if name := c.Icon.Get(); name != "" {
		l.leadingIconIn(name, 24, col.OnSurface, r.X+28, r.Y+r.H/2, clip)
		textX = r.X + 56
	}
	l.textIn(c.Label.Get(), l.style("titleLarge"), col.OnSurface,
		tree.Rect{X: textX, Y: r.Y, W: r.Right() - textX - 64, H: r.H}, AlignStart, clip)
	if c.Trailing.Get() != "" {
		l.trailingIconIn(c.Trailing.Get(), 24, col.OnSurface, r.Right()-28, r.Y+r.H/2, clip)
	}
}

// The bar paints the container; each destination draws its own indicator, so
// an item nested inside a Column still shows its state.
func (l *List) paintNavigationBar(c *tree.Component, r tree.Rect, clip tree.Rect) {
	l.fillRect(r, l.Theme.Colors.SurfaceContainer, 0, 0, 0, clip)
	l.paintNavIndicator(c, "NavigationBar", clip)
}

func (l *List) paintNavigationRail(c *tree.Component, r tree.Rect, clip tree.Rect) {
	// No edge line of its own: a rail on Surface was invisible against a
	// Surface page, so it sits on SurfaceContainer, and a design that wants a
	// divider asks for one with borderRightThickness or a Divider.
	l.fillRect(r, l.Theme.Colors.SurfaceContainer, 0, 0, 0, clip)
	l.paintNavIndicator(c, "NavigationRail", clip)
}

func (l *List) paintNavigationDrawer(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	// A drawer is a sheet: rounded on the right, elevated above the page.
	rad := float32(m3.ShapeLarge)
	radii := [4]float32{0, rad, rad, 0}
	l.shadowRadii(r, radii, m3.Elevation1, clip)
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect, Rect: rectArr(r), Radius: radii,
		Color: col.SurfaceContainerLow, Clip: clipArr(clip),
	})
	if c.Label.Get() != "" {
		l.textIn(c.Label.Get(), l.style("titleSmall"), col.OnSurfaceVariant,
			tree.Rect{X: r.X + 28, Y: r.Y + 16, W: r.W - 56, H: 20}, AlignStart, clip)
	}
	l.paintNavIndicator(c, "NavigationDrawer", clip)
}

// enclosingNav is the nearest navigation container above a node, so an item
// wrapped in a Column for layout still knows how its parent arranges it. The
// component is returned with the kind because a bar's variant decides how its
// items are arranged.
func enclosingNav(b *tree.NodeBase) (string, *tree.Component) {
	for p := b.Parent(); p != nil; p = p.Base().Parent() {
		switch p.Kind() {
		case "NavigationBar", "NavigationRail", "NavigationDrawer":
			return p.Kind(), m3Of(p.Base())
		}
	}
	return "", nil
}

func (l *List) paintNavigationItem(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	nav, navComp := enclosingNav(c.Base())
	selected := c.Selected.Get()
	animating := navComp != nil && navComp.NavActive
	ni, _ := c.Node().(*tree.NavigationItem)
	// A medium bar puts the icon and the label side by side in one pill; the
	// compact one stacks them under the icon.
	medium := nav == "NavigationBar" && navComp != nil && variant(navComp, "compact") == "medium"

	container := col.SecondaryContainer
	if ni != nil {
		if v := ni.SelectedContainerColor.Get(); v != 0 {
			container = v
		}
	}

	// The indicator belongs to the item, so it follows the item wherever the
	// design puts it, and the container only paints its own surface.
	if selected && !medium && !animating {
		switch nav {
		case "NavigationDrawer":
			l.fillRect(r, container, 28, 0, 0, clip)
		case "NavigationRail":
			cx := r.X + r.W/2
			l.fillRect(tree.Rect{X: cx - 28, Y: r.Y + 4, W: 56, H: 32}, container, 16, 0, 0, clip)
		default:
			cx := r.X + r.W/2
			l.fillRect(tree.Rect{X: cx - 32, Y: r.Y + 12, W: 64, H: 32}, container, 16, 0, 0, clip)
		}
	}

	fg := col.OnSurfaceVariant
	if selected {
		fg = col.OnSecondaryContainer
	}
	icon, label := fg, fg
	if v := c.LeadingIconColor.Get(); v != 0 {
		icon = v
	}
	if ni != nil {
		if v := ni.IconColor.Get(); v != 0 {
			icon = v
		}
		if selected {
			if v := ni.SelectedIconColor.Get(); v != 0 {
				icon = v
			}
		}
	}
	if v := c.LabelColor.Get(); v != 0 {
		label = v
	}
	if ni != nil && selected {
		if v := ni.SelectedLabelColor.Get(); v != 0 {
			label = v
		}
	}

	// A selected destination shows the filled glyph: the state is the signal, so
	// the icon follows it even when the design left the filled properties off.
	filled := selected || l.filledIcons || l.filledLeading

	if medium {
		// Icon, an 8dp gap and the label, all inside a 32dp pill with 16dp of
		// padding at each end, centred in the destination's share of the bar.
		st := l.style("labelMedium")
		textW := textWidth(l, c.Label.Get(), st)
		content := textW
		if c.Icon.Get() != "" {
			content += 24 + 8
		}
		pillW := content + 32
		pillX := r.X + (r.W-pillW)/2
		cy := r.Y + r.H/2
		if selected && !animating {
			l.fillRect(tree.Rect{X: pillX, Y: cy - 16, W: pillW, H: 32}, container, 16, 0, 0, clip)
		}
		x := pillX + 16
		if name := c.Icon.Get(); name != "" {
			l.iconInFilled(name, 24, disabledAlpha(c, icon), x+12, cy, clip, filled)
			x += 24 + 8
		}
		l.textIn(c.Label.Get(), st, disabledAlpha(c, label),
			tree.Rect{X: x, Y: r.Y, W: pillX + pillW - 16 - x, H: r.H}, AlignStart, clip)
		return
	}

	if nav == "NavigationDrawer" {
		l.iconInFilled(c.Icon.Get(), 24, disabledAlpha(c, icon), r.X+24, r.Y+r.H/2, clip, filled)
		l.textIn(c.Label.Get(), l.style("labelMedium"), disabledAlpha(c, label),
			tree.Rect{X: r.X + 52, Y: r.Y, W: r.W - 60, H: r.H}, AlignStart, clip)
		return
	}
	// Icon sits in the 32dp indicator; the label follows underneath. The
	// indicator is at 12dp in a bar and 4dp in a rail.
	iconY, labelY := r.Y+28, r.Y+48
	if nav == "NavigationRail" {
		iconY, labelY = r.Y+20, r.Y+40
	}
	l.iconInFilled(c.Icon.Get(), 24, disabledAlpha(c, icon), r.X+r.W/2, iconY, clip, filled)
	l.textIn(c.Label.Get(), l.style("labelMedium"), disabledAlpha(c, label),
		tree.Rect{X: r.X, Y: labelY, W: r.W, H: 16}, AlignCenter, clip)
}

const navIndicatorDuration = 250 * time.Millisecond

// paintNavIndicator draws the moving band behind a navigation destination.
// Items skip their own indicator while NavActive is set, so there is only one
// band on screen; at the end the target item draws it again.
func (l *List) paintNavIndicator(c *tree.Component, nav string, clip tree.Rect) {
	if !c.NavActive || c.NavFrom == nil || c.NavTo == nil {
		return
	}
	mode := c.Animation.Get()
	if mode == "" {
		mode = "slide"
	}
	if mode == "none" {
		c.NavActive = false
		return
	}
	if !c.NavPosed {
		// Start on the first painted frame. A click after an idle wake can move
		// the frame clock by the idle time before the first paint, which would
		// otherwise skip most of the transition.
		c.NavStart = l.Clock
		c.NavPosed = true
	}
	t := navProgress(l.Clock, c.NavStart)
	if t >= 1 {
		c.NavActive = false
		return
	}
	l.Animating = true
	from := l.navIndicatorRect(c, nav, c.NavFrom)
	to := l.navIndicatorRect(c, nav, c.NavTo)
	radius := navIndicatorRadius(nav)
	if mode == "fade" {
		// A crossfade has to spend time at the middle. The slide's ease-out
		// would already be at ~88% by the half-way frame and read as a cut.
		l.fillRect(from, withAlpha(l.navIndicatorColor(c.NavFrom), 1-t), radius, 0, 0, clip)
		l.fillRect(to, withAlpha(l.navIndicatorColor(c.NavTo), t), radius, 0, 0, clip)
		return
	}
	l.fillRect(lerpRect(from, to, navEase(t)), l.navIndicatorColor(c.NavTo), radius, 0, 0, clip)
}

// navProgress is linear time through the indicator transition. Fade uses it
// directly; slide shapes it with navEase.
func navProgress(clock, start time.Duration) float64 {
	if clock <= start {
		return 0
	}
	t := float64(clock-start) / float64(navIndicatorDuration)
	if t > 1 {
		t = 1
	}
	return t
}

func navEase(t float64) float64 { return 1 - (1-t)*(1-t)*(1-t) }

func lerpRect(a, b tree.Rect, t float64) tree.Rect {
	return tree.Rect{
		X: a.X + (b.X-a.X)*t,
		Y: a.Y + (b.Y-a.Y)*t,
		W: a.W + (b.W-a.W)*t,
		H: a.H + (b.H-a.H)*t,
	}
}

func navIndicatorRadius(nav string) float64 {
	if nav == "NavigationDrawer" {
		return 28
	}
	return 16
}

func (l *List) navIndicatorColor(n tree.Node) uint32 {
	if ni, ok := n.(*tree.NavigationItem); ok {
		if v := ni.SelectedContainerColor.Get(); v != 0 {
			return v
		}
	}
	return l.Theme.Colors.SecondaryContainer
}

func (l *List) navIndicatorRect(c *tree.Component, nav string, n tree.Node) tree.Rect {
	r := windowRect(n.Base())
	switch nav {
	case "NavigationDrawer":
		return r
	case "NavigationRail":
		cx := r.X + r.W/2
		return tree.Rect{X: cx - 28, Y: r.Y + 4, W: 56, H: 32}
	default:
		cx := r.X + r.W/2
		if variant(c, "compact") == "medium" {
			item, _ := n.(*tree.NavigationItem)
			st := l.style("labelMedium")
			textW := textWidth(l, item.Label.Get(), st)
			content := textW
			if item.Icon.Get() != "" {
				content += 24 + 8
			}
			pillW := content + 32
			return tree.Rect{X: r.X + (r.W-pillW)/2, Y: r.Y + r.H/2 - 16, W: pillW, H: 32}
		}
		return tree.Rect{X: cx - 32, Y: r.Y + 12, W: 64, H: 32}
	}
}

// windowRect is a node's laid-out box in window coordinates, including the
// scroll of every ancestor.
func windowRect(b *tree.NodeBase) tree.Rect {
	r := b.Bounds()
	for p := b.Parent(); p != nil; p = p.Base().Parent() {
		pb := p.Base()
		r.X += pb.Bounds().X
		r.Y += pb.Bounds().Y
		sx, sy := pb.ScrollShift()
		r.X += sx
		r.Y += sy
	}
	return r
}

// paintBreadcrumb draws the separator in each gap. The crumbs are the children
// and paint themselves, so a crumb can be a Text, a link or anything else.
func (l *List) paintBreadcrumb(c *tree.Component, r tree.Rect, clip tree.Rect) {
	bc, ok := c.Node().(*tree.Breadcrumb)
	if !ok || bc.Separator.Get() == "" {
		return
	}
	size := bc.SeparatorSize.Get()
	if size <= 0 {
		size = 18
	}
	col := l.Theme.Colors.OnSurfaceVariant
	if v := bc.SeparatorColor.Get(); v != 0 {
		col = v
	}
	sx, sy := scrollShift(c.Base())
	var prev tree.Rect
	have := false
	for _, k := range c.Base().Kids() {
		if !k.Visible() {
			continue
		}
		b := k.Bounds()
		if have {
			cx := r.X + sx + (prev.X+prev.W+b.X)/2
			cy := r.Y + sy + (prev.Y+prev.H/2+b.Y+b.H/2)/2
			l.iconIn(bc.Separator.Get(), size, col, cx, cy, clip)
		}
		prev, have = b, true
	}
}

// m3Of returns the component state behind a node, or nil for a layout node.
func m3Of(b *tree.NodeBase) *tree.Component {
	if c, ok := b.Node().(interface{ M3() *tree.Component }); ok {
		return c.M3()
	}
	return nil
}

func (l *List) paintTabs(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	primary := variant(c, "secondary") == "primary"
	l.fillRect(r, col.Surface, 0, 0, 0, clip)
	// Divider under the row.
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:  [4]float32{float32(r.X), float32(r.Bottom() - 1), float32(r.W), 1},
		Color: col.SurfaceVariant, Clip: clipArr(clip),
	})
	kids := c.Base().Kids()
	// A design marks the active tab with `selected: true`; code that only set
	// the container's SelectedIndex still gets the underline on that tab.
	anySelected := false
	for _, k := range kids {
		if kc := m3Of(k); kc != nil && kc.Selected.Get() {
			anySelected = true
			break
		}
	}
	for i, k := range kids {
		kc := m3Of(k)
		active := false
		if kc != nil {
			active = kc.Selected.Get()
		}
		if !anySelected {
			active = i == c.SelectedIndex
		}
		if !active {
			continue
		}
		kb := k.Bounds()
		indicator := tree.Rect{X: r.X + kb.X + 8, Y: r.Bottom() - 3, W: kb.W - 16, H: 3}
		radius := [4]float32{3, 3, 0, 0}
		if primary {
			// A primary tab's indicator hugs the content instead of the cell.
			w := 24.0
			if kc != nil {
				w = maxf(textWidth(l, kc.Label.Get(), l.style("titleSmall")), 24)
			}
			w = minf(w, kb.W-16)
			indicator = tree.Rect{X: r.X + kb.X + (kb.W-w)/2, Y: r.Bottom() - 3, W: w, H: 3}
			radius = [4]float32{3, 3, 3, 3}
		}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect, Rect: rectArr(indicator),
			Radius: radius, Color: col.Primary, Clip: clipArr(clip),
		})
	}
}

func (l *List) paintTab(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	fg := col.OnSurfaceVariant
	if c.Selected.Get() {
		fg = col.Primary
	}
	fg = disabledAlpha(c, fg)
	l.stateLayer(c, r, 0, fg, clip)
	box := tree.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}
	if c.Icon.Get() != "" {
		l.leadingIconIn(c.Icon.Get(), 24, fg, r.X+r.W/2, r.Y+r.H/2-10, clip)
		l.textIn(c.Label.Get(), l.style("titleSmall"), fg,
			tree.Rect{X: r.X, Y: r.Y + 20, W: r.W, H: r.H - 20}, AlignCenter, clip)
		return
	}
	l.textIn(c.Label.Get(), l.style("titleSmall"), fg, box, AlignCenter, clip)
}

// ---------------------------------------------------------------- progress

func (l *List) paintLinearProgress(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	wavy := variant(c, "normal") == "wavy"
	// thickness sizes the stroke; a box shorter than the stroke wins, so the
	// bar never draws outside the space layout gave it.
	trackH := strokeOr(c.Thickness.Get(), 4)
	if wavy {
		trackH = waveStroke(c.Thickness.Get(), r.H)
	}
	if r.H > 0 && r.H < trackH {
		trackH = r.H
	}
	tr := tree.Rect{X: r.X, Y: r.Y + (r.H-trackH)/2, W: r.W, H: trackH}
	lo, hi := c.Min.Get(), c.Max.Get()
	frac := 0.0
	if hi > lo {
		frac = (c.Value.Get() - lo) / (hi - lo)
	}
	frac = clamp01(frac)
	w := tr.W * frac
	if wavy {
		l.paintWavyBar(tr, frac, trackH, clip)
	} else if w > 0 {
		l.fillRect(tree.Rect{X: tr.X, Y: tr.Y, W: w, H: tr.H}, col.Primary, trackH/2, 0, 0, clip)
	}
	// The track is the part that is left, not a full line under the progress:
	// the two never touch, and the space between them is a gap the width of the
	// stroke.
	start := tr.X + w
	if w > 0 {
		start += gapFor(trackH)
	}
	// The track stops short of the end to leave room for the stop indicator.
	end := tr.Right() - 8
	if rem := end - start; rem > 0 {
		l.fillRect(tree.Rect{X: start, Y: tr.Y, W: rem, H: tr.H}, col.SecondaryContainer, trackH/2, 0, 0, clip)
	}
	// The stop indicator is the dot at the far end of the track, which is what
	// tells a determinate bar where its scale ends.
	l.fillRect(tree.Rect{X: tr.Right() - 4, Y: tr.Y + tr.H/2 - 2, W: 4, H: 4}, col.OnSurfaceVariant, 2, 0, 0, clip)
}

// gapFor is the space between the active part and the track: a stroke's width,
// never less than 3dp, which is what keeps the two ends from touching.
func gapFor(stroke float64) float64 { return math.Max(3, stroke) }

// waveStroke is a wave's stroke width: the property's thickness, or the
// component's own 6dp, scaled down when the box is shorter than the ribbon.
func waveStroke(thickness, boxH float64) float64 {
	stroke := strokeOr(thickness, 6)
	// The ribbon is the stroke plus a quarter of it above and below.
	if boxH > 0 && boxH < stroke*1.75 {
		stroke = boxH / 1.75
	}
	return stroke
}

// wavePhase is how far a wave has travelled: one wavelength every 1.6s, which
// reads as a slow flow rather than a twitch.
func (l *List) wavePhase() float64 {
	return l.Clock.Seconds() / 1.6 * 2 * math.Pi
}

// paintWavyBar draws the active part as a ribbon: a sine along the track,
// sampled and joined with the rounded segments the charts use, so the wave has
// the same anti-aliased join as a chart line. The phase rides the list's clock,
// so the wave travels and the caller keeps drawing frames.
func (l *List) paintWavyBar(tr tree.Rect, frac, stroke float64, clip tree.Rect) {
	col := l.Theme.Colors
	if frac <= 0 {
		return
	}
	l.Animating = true
	w := tr.W * frac
	if w < 2 {
		return
	}
	amplitude := stroke * 0.375
	wavelength := stroke * 4.25
	phase := l.wavePhase()
	cx, cy := tr.X, tr.Y+tr.H/2
	mid := cy
	point := func(x float64) chartPoint {
		return chartPoint{X: cx + x, Y: mid + amplitude*math.Sin(x/wavelength*2*math.Pi-phase)}
	}
	if w <= stroke {
		l.chartDot(point(w/2), stroke/2, col.Primary, clip)
		return
	}
	step := math.Max(0.75, 2.0/l.deviceScale())
	var prev chartPoint
	for x := 0.0; ; x += step {
		p := point(x)
		if x > 0 {
			l.chartSegment(prev, p, col.Primary, stroke, clip)
		}
		prev = p
		if x >= w {
			break
		}
	}
	l.chartDot(point(0), stroke/2, col.Primary, clip)
	l.chartDot(point(w), stroke/2, col.Primary, clip)
}

func (l *List) paintCircularProgress(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	size := minf(r.W, r.H)
	lo, hi := c.Min.Get(), c.Max.Get()
	frac := 0.0
	if hi > lo {
		frac = (c.Value.Get() - lo) / (hi - lo)
	}
	frac = clamp01(frac)
	if variant(c, "normal") == "wavy" {
		l.paintWavyRing(r, frac, c.Thickness.Get(), clip)
		return
	}
	thickness := strokeOr(c.Thickness.Get(), 4)
	if thickness > size {
		thickness = size
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	mid := size/2 - thickness/2
	end := frac * 2 * math.Pi
	l.progressArc(cx, cy, mid, thickness, 0, end, col.Primary, clip)
	// The track is what is left of the ring, with a gap at each end of the
	// filled part, so the two never touch and nothing runs under the arc. The
	// angle has to carry the two round caps as well as the gap: each band ends
	// half a stroke past the angle it is cut at.
	gap := (thickness + gapFor(thickness)) / mid
	switch {
	case end <= 0:
		l.progressArc(cx, cy, mid, thickness, 0, 2*math.Pi, col.SecondaryContainer, clip)
	case 2*math.Pi-gap > end+gap:
		l.progressArc(cx, cy, mid, thickness, end+gap, 2*math.Pi-gap, col.SecondaryContainer, clip)
	}
}

// progressArc draws one band of a ring: the arc itself and, unless it closes the
// circle, a round cap at each end. The sweep is a straight cut in the shader, so
// the caps are what make an end read as soft rather than square. mid is the
// centreline radius.
func (l *List) progressArc(cx, cy, mid, thickness, start, end float64, color uint32, clip tree.Rect) {
	if end-start <= 0.001 {
		return
	}
	outer := mid + thickness/2
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Arc,
		Rect:       rectArr(tree.Rect{X: cx - outer, Y: cy - outer, W: outer * 2, H: outer * 2}),
		Color:      color,
		Thickness:  float32(thickness),
		StartAngle: float32(start),
		EndAngle:   float32(end),
		Clip:       clipArr(clip),
	})
	if end-start >= 2*math.Pi-0.01 {
		return
	}
	for _, a := range []float64{start, end} {
		l.chartDot(chartPoint{X: cx + mid*math.Sin(a), Y: cy - mid*math.Cos(a)}, thickness/2, color, clip)
	}
}

// paintWavyRing draws the active part of a circular progress as a ribbon: the
// arc's radius pulses as it goes round, sampled into rounded segments the way
// the charts draw a curve. The track is the plain ring that is left, with the
// same gap the bar leaves, so the wave never sits on top of it.
func (l *List) paintWavyRing(r tree.Rect, frac, thickness float64, clip tree.Rect) {
	col := l.Theme.Colors
	size := minf(r.W, r.H)
	if size <= 0 || frac <= 0 {
		return
	}
	l.Animating = true
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	const waves = 7.0
	stroke := strokeOr(thickness, 5)
	// The ring carries its wave inward, so the box caps the stroke.
	if limit := size / 2.5; stroke > limit {
		stroke = limit
	}
	amplitude := stroke * 0.34
	rr := size/2 - stroke/2 - amplitude
	if rr <= 0 {
		return
	}
	end := frac * 2 * math.Pi
	phase := l.wavePhase()
	point := func(t float64) chartPoint {
		rad := rr + amplitude*math.Sin(t*waves-phase)
		return chartPoint{X: cx + rad*math.Sin(t), Y: cy - rad*math.Cos(t)}
	}
	// A step of about two device pixels of arc keeps the ribbon smooth whatever
	// the ring's size and the panel's ratio.
	step := math.Max(2*math.Pi/240, (2.0/l.deviceScale())/rr)
	var prev chartPoint
	for t := 0.0; ; t += step {
		p := point(t)
		if t > 0 {
			l.chartSegment(prev, p, col.Primary, stroke, clip)
		}
		prev = p
		if t >= end {
			break
		}
	}
	l.chartDot(point(0), stroke/2, col.Primary, clip)
	l.chartDot(point(end), stroke/2, col.Primary, clip)
	// The track: a plain 4dp ring for the rest of the circle, a gap away from
	// each end of the ribbon. The angle carries the ribbon's cap and the
	// track's own.
	const trackW = 4.0
	gap := (stroke/2 + trackW/2 + gapFor(trackW)) / rr
	if 2*math.Pi-gap > end+gap {
		l.progressArc(cx, cy, rr, trackW, end+gap, 2*math.Pi-gap, col.SecondaryContainer, clip)
	}
}

// paintIcon draws a standalone glyph: it fills the shorter side of its box and
// is centred, so a square `size` gives the M3 icon and a wider box pads it. With
// no colour named it takes the M3 icon role.
func (l *List) paintIcon(c *tree.Component, r tree.Rect, clip tree.Rect) {
	ic, ok := c.Node().(*tree.Icon)
	if !ok || r.W <= 0 || r.H <= 0 {
		return
	}
	col := l.Theme.Colors.OnSurfaceVariant
	if v := ic.Color.Get(); v != 0 {
		col = v
	}
	l.iconIn(c.Icon.Get(), minf(r.W, r.H), col, r.X+r.W/2, r.Y+r.H/2, clip)
}

// paintLoadingIndicator draws the M3 loading shape: a rounded star whose lobe
// count morphs while it turns. The shape is a signed distance in the renderer,
// so it stays sharp at any size and needs no bitmap; the list only says where
// the morph and the turn are, and keeps drawing frames while it plays.
func (l *List) paintLoadingIndicator(c *tree.Component, r tree.Rect, clip tree.Rect) {
	if minf(r.W, r.H) <= 0 {
		return
	}
	col := l.Theme.Colors.Primary
	if li, ok := c.Node().(*tree.LoadingIndicator); ok {
		if v := li.Color.Get(); v != 0 {
			col = v
		}
	}
	l.Animating = true
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Loading, Rect: rectArr(r),
		Color: col,
		// The turn rides the list's clock, like a wave's phase.
		StartAngle: float32(l.loadingTurn()),
		Depth:      float32(l.loadingDepth()),
		Square:     float32(l.loadingSquare()),
		Squash:     float32(l.loadingSquash()),
		Clip:       clipArr(clip),
	})
}

// paintSkeleton draws the placeholder: a rounded block in the base colour with
// a highlight band sweeping across it, which is what tells a reader the content
// is on its way rather than missing.
func (l *List) paintSkeleton(c *tree.Component, r tree.Rect, clip tree.Rect) {
	sk, ok := c.Node().(*tree.Skeleton)
	if !ok || r.W <= 0 || r.H <= 0 {
		return
	}
	base := sk.Color.Get()
	if base == 0 {
		base = l.Theme.Colors.SurfaceContainerHighest
	}
	rad := l.shapeRadius(c.Base(), r)
	radii := componentRadii(c.Base(), rad)
	l.fillShape(r, base, nil, radii, 0, 0, clip)
	if !sk.Animated.Get() {
		return
	}
	l.Animating = true
	hi := sk.Highlight.Get()
	if hi == 0 {
		hi = l.shimmerHighlight(base)
	}
	l.paintShimmerBand(r, hi, float32(rad), clip)
}

// shimmerHighlight is the band colour a skeleton uses when the design named
// none: the block lifted towards white. No role is lighter than the surface in
// both schemes, so this reads in light and dark; a light block has less room to
// lift and takes the larger step.
func (l *List) shimmerHighlight(base uint32) uint32 {
	lift := 0.45
	if !l.Theme.Dark {
		lift = 0.8
	}
	return blend(base, 0xFFFFFFFF, lift)
}

// paintShimmerBand draws the moving highlight of a loading placeholder. The
// band is a third of the block, swept from just off one edge to just off the
// other, so the highlight enters and leaves instead of popping. It keeps the
// frame loop awake while the content is on its way.
func (l *List) paintShimmerBand(r tree.Rect, hi uint32, rad float32, clip tree.Rect) {
	l.Animating = true
	band := math.Max(48, r.W/3)
	span := r.W + band
	phase := math.Mod(float64(l.Clock)/float64(skeletonPeriod), 1)
	g := &tree.Gradient{Kind: tree.LinearKind, Angle: 90, Stops: []tree.GradientStop{
		{At: 0, Color: withAlpha(hi, 0)},
		{At: 0.5, Color: withAlpha(hi, 0.75)},
		{At: 1, Color: withAlpha(hi, 0)},
	}}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: CSSGradient, Gradient: g,
		Rect:       rectArr(tree.Rect{X: r.X - band + span*phase, Y: r.Y, W: band, H: r.H}),
		Color:      hi,
		Clip:       clipArr(intersect(clip, r)),
		ClipRadius: rad,
		ClipBox:    rectArr(r),
	})
}

// skeletonPeriod is one sweep of the highlight. 1.4s reads as loading without
// pulling the eye away from the rest of the page.
const skeletonPeriod = 1400 * time.Millisecond

// loadingMorphDur is one breath of the loading shape, in seconds. 4.55s is the
// timing the M3 indicator morphs on.
const loadingMorphDur = 4.55

// loadingSquare is how far the shape has moved from its twelve-lobed star to
// the four-lobed rounded square: a cosine breath, so the two shapes trade places
// once a cycle without a jump at the seam.
func (l *List) loadingSquare() float64 {
	s := l.Clock.Seconds() / loadingMorphDur * 2 * math.Pi
	return 0.5 - 0.5*math.Cos(s)
}

// loadingSquash narrows the shape only while it is the square: a cubic keeps
// the star untouched and turns the square into the M3 indicator's pill.
func (l *List) loadingSquash() float64 {
	s := l.loadingSquare()
	return 0.34 * s * s * s
}

// loadingDepth is how deep the lobes are at this moment. The M3 indicator's
// depth runs about twice as fast as its shape morph, which is what makes a
// turning shape read as breathing rather than as a slow pulse.
func (l *List) loadingDepth() float64 {
	s := l.Clock.Seconds() / (loadingMorphDur / 2) * 2 * math.Pi
	return 0.85 - 0.15*math.Cos(s)
}

// loadingTurn is the shape's rotation: 900 degrees every six seconds, which is
// the speed the M3 indicator turns at.
func (l *List) loadingTurn() float64 {
	return l.Clock.Seconds() * (900.0 / 6.0) * math.Pi / 180
}

// ---------------------------------------------------------------- misc

func (l *List) paintCarousel(c *tree.Component, r tree.Rect, clip tree.Rect) {
	// Items are children, offset horizontally by ScrollX; the list clips.
	_ = c
	_ = r
	_ = clip
}

// paintCarouselItem draws the card the child content sits on. The label and
// scrim are an overlay, painted after the child so they stay readable.
func (l *List) paintCarouselItem(c *tree.Component, r tree.Rect, clip tree.Rect) {
	radius := c.Radius()
	l.fillRect(r, l.Theme.Colors.SurfaceContainerHigh, radius, 0, 0, clip)
}

// paintCarouselItemOverlay is paintComponentBorder's sibling for a carousel
// item: after the image and before the rounded clip, the card gets its scrim,
// press state and labels.
func (l *List) paintCarouselItemOverlay(c *tree.Component, r tree.Rect, clip tree.Rect) {
	radius := c.Radius()
	label, supporting := c.Label.Get(), c.Supporting.Get()
	if label != "" || supporting != "" {
		scrim := &tree.Gradient{
			Kind:  tree.LinearKind,
			Angle: 180,
			Stops: []tree.GradientStop{
				{At: 0.35, Color: 0x00000000},
				{At: 1, Color: 0x00000099},
			},
		}
		radii := [4]float64{radius, radius, radius, radius}
		l.fillShape(r, 0, scrim, radii, 0, 0, clip)
	}
	l.stateLayer(c, r, radius, l.Theme.Colors.Primary, clip)
	if label == "" && supporting == "" {
		return
	}
	const pad = 14.0
	labelColor := c.LabelColor.Get()
	if labelColor == 0 {
		labelColor = 0xFFFFFFFF
	}
	supportColor := c.SupportingColor.Get()
	if supportColor == 0 {
		supportColor = 0xFFFFFFCC
	}
	bottom := r.Bottom() - pad
	if supporting != "" {
		l.textIn(supporting, l.style("bodySmall"), supportColor,
			tree.Rect{X: r.X + pad, Y: bottom - 18, W: r.W - 2*pad, H: 18}, AlignStart, clip)
		bottom -= 18
	}
	if label != "" {
		l.textIn(label, l.style("labelLarge"), labelColor,
			tree.Rect{X: r.X + pad, Y: bottom - 22, W: r.W - 2*pad, H: 22}, AlignStart, clip)
	}
}

func (l *List) paintToolbar(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.shadow(r, radiusFor(m3.ShapeFull, r), m3.Elevation2, clip)
	l.fillRect(r, col.SurfaceContainer, radiusFor(m3.ShapeFull, r), 0, 0, clip)
}

// ---------------------------------------------------------------- text

// paintText draws a free-standing Text node using an M3 type role.
func (l *List) paintText(t *tree.Text, r tree.Rect, clip tree.Rect) {
	st := l.style(t.Role.Get())
	if v := t.FontSize.Get(); v > 0 {
		st.Size = v
		st.LineHeight = v * 1.2
	}
	st.Weight = t.WeightFor(st.Weight)
	color := t.Color.Get()
	if color == 0 {
		color = l.Theme.Colors.OnSurface
	}
	// A text that is a link draws the way a link run does: primary and
	// underlined, whatever colour the design gave it.
	link := t.Link.Get()
	if link != "" {
		color = l.Theme.Colors.Primary
	}
	align := AlignStart
	switch t.Align.Get() {
	case "center":
		align = AlignCenter
	case "end":
		align = AlignEnd
	}
	// Wrapped text is one op per line, laid out with the same greedy break the
	// layout pass used, so the reserved height and the drawn lines agree. A
	// single line keeps the node's box, which is what centers short text.
	src := t.Text.Get()
	// Padding and border are inside the box the design gave the Text, so the
	// glyphs wrap and align in the remaining room.
	pe, be := t.PaddingEdges(), t.BorderEdges()
	insetL, insetT := pe.Left+be.Left, pe.Top+be.Top
	insetR, insetB := pe.Right+be.Right, pe.Bottom+be.Bottom
	inner := tree.Rect{
		X: r.X + insetL, Y: r.Y + insetT,
		W: r.W - insetL - insetR, H: r.H - insetT - insetB,
	}
	inner.W, inner.H = maxf(inner.W, 0), maxf(inner.H, 0)
	var lines []text.Line
	lh := st.LineHeight
	if lh <= 0 {
		lh = st.Size * 1.2
	}
	maxLines := int(t.MaxLines.Get())
	if maxLines < 0 {
		maxLines = 0
	}
	var single [1]text.Line
	wrapped := t.Wrap.Get() || maxLines > 0
	switch {
	case t.Truncate.Get():
		// One line, cut to the box; the ellipsis says the rest is not shown.
		single[0] = text.Line{Text: l.ellipsize(src, inner.W, st), Start: 0, End: len(src)}
		lines = single[:]
		wrapped = false
	case wrapped:
		lines = l.wrapLines(src, inner.W, st)
		if maxLines > 0 && len(lines) > maxLines {
			// Copy before rewriting the last line: wrapLines returns the cached
			// slice, and the next paint must still see the whole paragraph.
			clamped := make([]text.Line, maxLines)
			copy(clamped, lines[:maxLines])
			// The clamp hid at least one line, so the last one ends with the
			// ellipsis even when it happens to fit by itself.
			clamped[maxLines-1].Text = l.endWithEllipsis(clamped[maxLines-1].Text, inner.W, st)
			lines = clamped
		}
	default:
		single[0] = text.Line{Text: src, Start: 0, End: len(src)}
		lines = single[:]
	}
	selA, selB := t.SelA, t.SelB
	if selA > selB {
		selA, selB = selB, selA
	}
	for i, line := range lines {
		box := inner
		if wrapped {
			box = tree.Rect{X: inner.X, Y: inner.Y + float64(i)*lh, W: inner.W, H: lh}
		}
		// The highlight sits under the glyphs and only spans the selected
		// characters, so a line that follows a wrapped paragraph still reads.
		if t.Selectable.Get() && selA != selB {
			a := clampInt(selA-line.Start, 0, len(line.Text))
			b := clampInt(selB-line.Start, 0, len(line.Text))
			if a != b {
				lineW := textWidth(l, line.Text, st)
				x := box.X + alignOffset(box.W, lineW, align)
				x0 := x + textWidth(l, line.Text[:a], st)
				x1 := x + textWidth(l, line.Text[:b], st)
				h := st.Size * 1.35
				l.Ops = append(l.Ops, Op{
					Kind: Fill, Shape: RoundedRect,
					Rect:  [4]float32{float32(x0), float32(box.Y + (box.H-h)/2), float32(x1 - x0), float32(h)},
					Color: withAlpha(l.Theme.Colors.Primary, m3.SelectionOpacity),
					Clip:  clipArr(clip),
				})
			}
		}
		before := len(l.Ops)
		l.textIn(line.Text, st, color, box, align, clip)
		// Text draws its own shadow: each layer is an offset copy of the glyphs,
		// softened by Blur. An inset layer has no meaning for a glyph run, so it
		// is skipped rather than drawn as a box.
		if len(l.Ops) > before {
			if shadows := l.textShadows(t); len(shadows) > 0 {
				l.Ops[len(l.Ops)-1].TextShadows = shadows
			}
			if t.FontStyle.Get() == "italic" {
				l.Ops[len(l.Ops)-1].Italic = true
			}
		}
		if line.Text != "" {
			lineW := textWidth(l, line.Text, st)
			if link != "" {
				l.decorationLine(box.X+alignOffset(box.W, lineW, align),
					box.Y+box.H/2+st.Size*textBaseline+st.Size*0.12,
					lineW, math.Max(1, math.Round(st.Size/14)), "solid", color, clip)
			}
			l.textDecoration(t, box, lineW, align, color, st.Size, clip)
		}
	}
}

// ellipsize cuts a run to the longest prefix that fits width once an ellipsis is
// added. It is what `truncate` and the last line of `maxLines` end a run with.
func (l *List) ellipsize(s string, width float64, st m3.TextStyle) string {
	if width <= 0 || textWidth(l, s, st) <= width {
		return s
	}
	return l.endWithEllipsis(s, width, st)
}

// endWithEllipsis cuts s so the run plus an ellipsis fits width, trimming even a
// line that fits on its own.
func (l *List) endWithEllipsis(s string, width float64, st m3.TextStyle) string {
	const mark = "…"
	markW := textWidth(l, mark, st)
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if textWidth(l, string(runes[:mid]), st)+markW <= width {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(runes[:lo]) + mark
}

// alignOffset is where a run starts inside a box of width boxW.
func alignOffset(boxW, runW float64, align Align) float64 {
	switch align {
	case AlignCenter:
		return (boxW - runW) / 2
	case AlignEnd:
		return boxW - runW
	}
	return 0
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// textWidth measures a run, falling back to an estimate when no measurer is set.
// wrapKey identifies a wrapped run: the same text at the same column and style
// always breaks the same way, so a repaint can reuse the lines instead of
// re-walking every word.
type wrapKey struct {
	s      string
	width  float64
	size   float64
	weight int
	family string
}

// maxWrapEntries bounds the wrap cache. A page shows far fewer distinct runs
// than this; past it the map is dropped, and the visible runs wrap once more.
const maxWrapEntries = 512

// wrapLines wraps a run, reusing the previous answer when nothing changed.
func (l *List) wrapLines(s string, width float64, st m3.TextStyle) []text.Line {
	key := wrapKey{s: s, width: width, size: st.Size, weight: st.Weight, family: l.family}
	if lines, ok := l.wrap[key]; ok {
		return lines
	}
	lines := text.WrapRanges(s, width, func(run string) float64 {
		return textWidth(l, run, st)
	})
	if l.wrap == nil || len(l.wrap) >= maxWrapEntries {
		l.wrap = make(map[wrapKey][]text.Line, maxWrapEntries)
	}
	l.wrap[key] = lines
	return lines
}

func textWidth(l *List, s string, st m3.TextStyle) float64 {
	if s == "" {
		return 0
	}
	if l.MeasureFamily != nil && l.family != "" {
		return l.MeasureFamily(s, st.Size, st.Weight, l.family)
	}
	if l.Measure != nil {
		return l.Measure(s, st.Size, st.Weight)
	}
	return float64(len([]rune(s))) * st.Size * 0.55
}

// textShadows returns the layers a Text run draws, without the inset ones: CSS
// text-shadow has no inset keyword.
func (l *List) textShadows(t *tree.Text) []tree.ShadowLayer {
	all := t.Base().Shadow()
	out := make([]tree.ShadowLayer, 0, len(all))
	for _, s := range all {
		if s.Inset {
			continue
		}
		s.Color = shadowColor(s, l.Theme)
		out = append(out, s)
	}
	return out
}

// textDecoration paints the strokes `decoration` names over one line, the way
// CSS text-decoration does: the tokens combine, the style picks the stroke, and
// thickness and colour default to the font.
func (l *List) textDecoration(t *tree.Text, box tree.Rect, lineW float64, align Align, color uint32, size float64, clip tree.Rect) {
	deco := t.Decoration.Get()
	if deco == "" || deco == "none" {
		return
	}
	thickness := t.DecorationThickness.Get()
	if thickness <= 0 {
		thickness = math.Max(1, math.Round(size/14))
	}
	dcol := t.DecorationColor.Get()
	if dcol == 0 {
		dcol = color
	}
	style := t.DecorationStyle.Get()
	baseline := box.Y + box.H/2 + size*textBaseline
	x := box.X + alignOffset(box.W, lineW, align)
	for _, tok := range strings.Fields(deco) {
		var y float64
		switch tok {
		case "underline":
			y = baseline + size*0.12
		case "overline":
			y = baseline - size*0.83
		case "line-through":
			y = baseline - size*0.3
		default:
			continue
		}
		l.decorationLine(x, y, lineW, thickness, style, dcol, clip)
	}
}

// decorationLine draws one horizontal stroke in the requested style.
func (l *List) decorationLine(x, y, w, t float64, style string, color uint32, clip tree.Rect) {
	rect := func(x, y, w, h float64) tree.Rect {
		return tree.Rect{X: x, Y: y, W: w, H: h}
	}
	switch style {
	case "double":
		thin := math.Max(1, t/3)
		l.decorationLine(x, y-thin*0.75, w, thin, "solid", color, clip)
		l.decorationLine(x, y+thin*0.75, w, thin, "solid", color, clip)
	case "dotted":
		step := t * 2.5
		for px := x; px < x+w; px += step {
			l.fill(rect(px, y-t/2, math.Min(t, x+w-px), t), color, Circle, clip)
		}
	case "dashed":
		step := t * 5
		for px := x; px < x+w; px += step {
			l.fill(rect(px, y-t/2, math.Min(t*3, x+w-px), t), color, RoundedRect, clip)
		}
	case "wavy":
		h := t * 3
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: Wave, Rect: rectArr(rect(x, y-h/2, w, h)),
			Color: color, Thickness: float32(t), Clip: clipArr(clip),
		})
	default: // solid
		l.fill(rect(x, y-t/2, w, t), color, RoundedRect, clip)
	}
}

// fill appends a plain fill op for a decoration stroke.
func (l *List) fill(r tree.Rect, color uint32, shape Shape, clip tree.Rect) {
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: shape, Rect: rectArr(r), Color: color, Clip: clipArr(clip),
	})
}
