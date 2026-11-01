package paint

import (
	"math"

	"github.com/ksckaan1/goi/internal/m3"
	"github.com/ksckaan1/goi/internal/text"
	"github.com/ksckaan1/goi/internal/tree"
)

// This file draws Material 3 components. Every number comes from the M3 specs
// (dp for sizes, colors from the theme), so the whole app follows one system.

// style resolves a type-scale role name to a text style.
func (l *List) style(role string) m3.TextStyle { return m3.RoleStyle(role) }

// weightOf maps a declared weight to the face the library actually has, so a
// weight of 600 draws with bold exactly as layout measured it.
func weightOf(w int) float32 { return float32(text.NearestWeight(text.Weight(w))) }

// withAlpha returns c with its alpha multiplied by a.
func withAlpha(c uint32, a float64) uint32 {
	if a <= 0 {
		return 0
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
		Kind:     Text,
		Rect:     rectArr(box),
		Color:    color,
		Clip:     clipArr(clip),
		Text:     s,
		FontSize: float32(st.Size),
		Weight:   weightOf(st.Weight),
		Align:    align,
		TextX:    float32(box.X),
		TextY:    float32(baseline),
		BoxW:     float32(box.W),
	})
}

// iconIn appends an icon op centered at cx, cy.
func (l *List) iconIn(name string, size float64, color uint32, cx, cy float64, clip tree.Rect) {
	if name == "" {
		return
	}
	box := tree.Rect{X: cx - size/2, Y: cy - size/2, W: size, H: size}
	l.Ops = append(l.Ops, Op{
		Kind:     Text,
		Rect:     rectArr(box),
		Color:    color,
		Clip:     clipArr(clip),
		Text:     name,
		FontSize: float32(size),
		Weight:   400,
		Icon:     true,
		Align:    AlignCenter,
		TextX:    float32(box.X),
		TextY:    float32(cy + size*iconBaseline),
		BoxW:     float32(size),
	})
}

// shadow appends an elevation shadow behind a rounded rect.
func (l *List) shadow(r tree.Rect, radius float64, dp int, clip tree.Rect) {
	rad := float32(radius)
	l.shadowRadii(r, [4]float32{rad, rad, rad, rad}, dp, clip)
}

// shadowRadii is shadow for a shape whose corners differ, like a split button.
func (l *List) shadowRadii(r tree.Rect, radii [4]float32, dp int, clip tree.Rect) {
	if dp <= 0 {
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
	if !c.RippleOn || c.Ripple <= 0 {
		return
	}
	// Expand over the first 60% of the life, then fade out.
	p := c.Ripple
	grow := p / 0.6
	if grow > 1 {
		grow = 1
	}
	grow = 1 - (1-grow)*(1-grow) // ease out
	reach := math.Hypot(
		maxf(c.RippleX-r.X, r.Right()-c.RippleX),
		maxf(c.RippleY-r.Y, r.Bottom()-c.RippleY))
	size := 2 * reach * grow
	fade := 1.0
	if p > 0.6 {
		fade = 1 - (p-0.6)/0.4
	}
	// The disc is only clipped to its ancestors, so cut it to the component's
	// own box and round that with the shape radius: otherwise it spills past a
	// pill or a chip.
	cr := intersect(clip, r)
	rad := float32(radius)
	// Only a real cut (an ancestor clipping the box) drops the rounding; a
	// sub-pixel difference from intersecting with a wider clip must not, or
	// the ripple spills past the pill on every press.
	if cr.W < r.W-0.01 || cr.H < r.H-0.01 {
		rad = 0
	}
	l.Ops = append(l.Ops, Op{
		Kind:       Fill,
		Shape:      Circle,
		Rect:       [4]float32{float32(c.RippleX - size/2), float32(c.RippleY - size/2), float32(size), float32(size)},
		Color:      withAlpha(color, m3.StatePressed*fade),
		ClipRadius: rad,
		Clip:       clipArr(cr),
	})
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
	s := c.Text.Get()
	n := minInt(c.Caret, len(s))
	if n < 0 {
		n = 0
	}
	x := textWidth(l, s[:n], st)
	if p := c.Preedit; p != "" {
		m := minInt(c.PreeditCaret, len(p))
		if m < 0 {
			m = 0
		}
		x += textWidth(l, p[:m], st)
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
	case "DatePicker":
		l.paintDatePicker(c, r, clip)
	case "TimePicker":
		l.paintTimePicker(c, r, clip)
	case "Carousel":
		l.paintCarousel(c, r, clip)
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
	rad := c.Base().Radius()
	if rad <= 0 {
		rad = r.H / 2
	}
	l.fillRect(
		tree.Rect{X: r.X - gap, Y: r.Y - gap, W: r.W + 2*gap, H: r.H + 2*gap},
		0, rad+gap, l.Theme.Colors.Primary, width, clip)
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
	l.shadow(r, radius, elev, clip)
	l.fillRect(r, bg, radius, border, bw, clip)
	l.stateLayer(c, r, radius, fg, clip)

	labelW := r.W
	if c.Icon.Get() != "" {
		labelW -= 26
	}
	box := tree.Rect{X: r.X, Y: r.Y, W: labelW, H: r.H}
	if c.Icon.Get() != "" {
		// Icon then label, as one centred pair.
		labelWidth := textWidth(l, c.Label.Get(), l.style("labelLarge")) + 8 + 18
		start := r.X + (r.W-labelWidth)/2
		l.iconIn(c.Icon.Get(), 18, disabledAlpha(c, fg), start+9, r.Y+r.H/2, clip)
		l.textIn(c.Label.Get(), l.style("labelLarge"), disabledAlpha(c, fg),
			tree.Rect{X: start + 18 + 8, Y: r.Y, W: labelWidth - 26, H: r.H}, AlignStart, clip)
		return
	}
	l.textIn(c.Label.Get(), l.style("labelLarge"), disabledAlpha(c, fg), box, AlignCenter, clip)
}

func (l *List) paintIconButton(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	radius := radiusFor(m3.ShapeFull, r)
	icon := disabledAlpha(c, 0)
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
		// Extended FAB: icon + label.
		l.iconIn(c.Icon.Get(), 24, fg, r.X+24, r.Y+r.H/2, clip)
		l.textIn(c.Label.Get(), l.style("labelLarge"), fg,
			tree.Rect{X: r.X + 24 + 12, Y: r.Y, W: r.W - 60, H: r.H}, AlignStart, clip)
		return
	}
	l.iconIn(c.Icon.Get(), iconSize, fg, r.X+r.W/2, r.Y+r.H/2, clip)
}

func (l *List) paintFABMenu(c *tree.Component, r tree.Rect, clip tree.Rect) {
	// The menu items are children above the FAB; the FAB sits at the bottom.
	col := l.Theme.Colors
	radius := m3.ShapeLarge
	l.fillRect(r, col.PrimaryContainer, radius, 0, 0, clip)
	l.iconIn(c.Icon.Get(), 24, col.OnPrimaryContainer, r.X+r.W/2, r.Y+r.H/2, clip)
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
			BorderColor: border, BorderWidth: float32(bw),
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
		l.iconIn(c.Icon.Get(), 18, fg, start+9, r.Y+r.H/2, clip)
		l.textIn(c.Label.Get(), l.style("labelLarge"), fg,
			tree.Rect{X: start + 18 + 8, Y: r.Y, W: labelWidth - 26, H: r.H}, AlignStart, clip)
	} else {
		l.textIn(c.Label.Get(), l.style("labelLarge"), fg, main, AlignCenter, clip)
	}
	l.iconIn("arrow_drop_down", 24, fg, trail.X+trail.W/2, r.Y+r.H/2, clip)
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
		BorderColor: col.Outline, BorderWidth: 1,
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
		l.iconIn(name, 18, fg, start+9, r.Y+r.H/2, clip)
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

	// A leading icon sits 8dp from the edge with an 8dp gap, which pushes the
	// label 18dp further in than the plain 16dp padding.
	x := r.X + 16
	lead := c.Icon.Get()
	if c.Selected.Get() && lead == "" {
		lead = "check"
	}
	if lead != "" {
		l.iconIn(lead, 18, fg, r.X+17, r.Y+r.H/2, clip)
		x = r.X + 8 + 18 + 8
	}
	trail := c.Trailing.Get()
	endPad := 16.0
	if trail != "" {
		endPad = 8 + 18 + 8
	}
	l.textIn(c.Label.Get(), l.style("labelLarge"), fg,
		tree.Rect{X: x, Y: r.Y, W: r.Right() - x - endPad, H: r.H}, AlignStart, clip)
	if trail != "" {
		l.iconIn(trail, 18, fg, r.Right()-17, r.Y+r.H/2, clip)
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
		l.Ops = append(l.Ops, Op{Kind: Fill, Shape: Circle,
			Rect:  [4]float32{float32(cx - dot/2), float32(cy - dot/2), float32(dot), float32(dot)},
			Color: col.OnPrimary, Clip: clipArr(clip)})
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
	l.Ops = append(l.Ops, Op{Kind: Fill, Shape: Circle,
		Rect:  [4]float32{float32(cx - thumbD/2), float32(tr.Y + (trackH-thumbD)/2), float32(thumbD), float32(thumbD)},
		Color: thumb, Clip: clipArr(clip)})
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
	col := l.Theme.Colors
	cy := r.Y + r.H/2
	// Expressive XS slider: a 16dp full-rounded track split by a 6dp gap on
	// each side of a 4x44dp handle. The handle narrows to 2dp while pressed or
	// focused; the 4dp stop dot sits at the inactive end.
	const (
		inset      = 2.0 // half the handle, so the handle touches the bounds
		trackH     = 16.0
		handleW    = 2.0 // half the resting handle width
		gap        = 6.0
		stopSize   = 4.0
		cornerFull = trackH / 2
	)
	track := tree.Rect{X: r.X + inset, Y: cy - trackH/2, W: r.W - 2*inset, H: trackH}
	v := c.Value.Get()
	lo, hi := c.Min.Get(), c.Max.Get()
	frac := 0.0
	if hi > lo {
		frac = (v - lo) / (hi - lo)
	}
	frac = clamp01(frac)

	active := col.Primary
	inactive := col.SecondaryContainer
	handle := col.Primary
	stop := active
	if c.Disabled.Get() {
		active = withAlpha(col.OnSurface, m3.DisabledOpacity)
		inactive = withAlpha(col.OnSurface, m3.DisabledContainerOpacity)
		handle = withAlpha(col.OnSurface, m3.DisabledOpacity)
		stop = active
	}

	// The handle centre; the two track segments stop 6dp away from its edge.
	hx := track.X + track.W*frac
	activeEnd := hx - handleW - gap
	if activeEnd > track.X {
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect,
			Rect:   [4]float32{float32(track.X), float32(track.Y), float32(activeEnd - track.X), float32(trackH)},
			Radius: [4]float32{cornerFull, 2, 2, cornerFull},
			Color:  active, Clip: clipArr(clip),
		})
	}
	inactiveStart := hx + handleW + gap
	if inactiveStart < track.Right() {
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect,
			Rect:   [4]float32{float32(inactiveStart), float32(track.Y), float32(track.Right() - inactiveStart), float32(trackH)},
			Radius: [4]float32{2, cornerFull, cornerFull, 2},
			Color:  inactive, Clip: clipArr(clip),
		})
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: Circle,
			Rect: [4]float32{
				float32(track.Right() - cornerFull - stopSize/2), float32(cy - stopSize/2),
				stopSize, stopSize,
			},
			Color: stop, Clip: clipArr(clip),
		})
	}

	hw := handleW * 2
	if c.Pressed || c.Focused {
		hw = 2
	}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:   [4]float32{float32(hx - hw/2), float32(cy - 22), float32(hw), 44},
		Radius: [4]float32{float32(hw / 2), float32(hw / 2), float32(hw / 2), float32(hw / 2)},
		Color:  handle, Clip: clipArr(clip),
	})
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

	radius := m3.ShapeExtraSmall
	if filled {
		l.fillRect(r, container, radius, 0, 0, clip)
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
	} else {
		if c.Focused {
			border, bw = col.Primary, 2
		}
		l.fillRect(r, 0, radius, border, bw, clip)
	}

	// Floating label: sits above the value once the field has content or focus.
	hasValue := c.Text.Get() != "" || c.Preedit != "" || c.Focused
	lx := r.X + 16
	textW := r.W - 32
	if c.Icon.Get() != "" {
		lx += 40 // make room for the leading icon
		textW -= 40
	}
	if c.Trailing.Get() != "" {
		textW -= 40
	}
	labelStyle := l.style("bodyLarge")
	smallStyle := l.style("bodySmall")

	// The value scrolls instead of running past the field edge: shift it left
	// just enough for the caret to stay visible, and keep every value glyph
	// inside the text box.
	c.TextScroll = 0
	if c.Focused {
		c.TextScroll = textShift(fieldCaretX(l, c, labelStyle), textW)
	}
	shift := c.TextScroll
	textClip := intersect(clip, tree.Rect{X: lx, Y: r.Y, W: textW, H: r.H})

	// Selection sits under the value, in the same box as the caret.
	if c.Focused {
		if a, b, ok := c.Selection(); ok {
			s := c.Text.Get()
			x0 := lx - shift + textWidth(l, s[:a], labelStyle)
			x1 := lx - shift + textWidth(l, s[:b], labelStyle)
			l.Ops = append(l.Ops, Op{
				Kind: Fill, Shape: RoundedRect,
				Rect:   [4]float32{float32(x0), float32(r.Y + 26), float32(x1 - x0), 20},
				Radius: [4]float32{2, 2, 2, 2},
				Color:  withAlpha(col.Primary, m3.SelectionOpacity),
				Clip:   clipArr(textClip),
			})
		}
	}

	if hasValue {
		// Label shrinks to the top 16dp band, value occupies the rest.
		l.textIn(c.Label.Get(), smallStyle, hint,
			tree.Rect{X: lx, Y: r.Y + 8, W: textW, H: 16}, AlignStart, clip)
		l.textIn(c.Text.Get(), labelStyle, fg,
			tree.Rect{X: lx - shift, Y: r.Y + 22, W: textW + shift, H: 26}, AlignStart, textClip)
		// IME composition: inserted at the caret, underlined so it reads as
		// provisional. The caret then sits inside it, not after it.
		if c.Preedit != "" {
			value := c.Text.Get()
			n := minInt(c.Caret, len(value))
			px := lx - shift + textWidth(l, value[:n], labelStyle)
			l.preedit(c.Preedit, px, r.Y+22, labelStyle, fg, r.Y+44, textClip)
		}
	} else {
		label := c.Label.Get()
		if label == "" {
			label = c.Placeholder.Get()
		}
		l.textIn(label, labelStyle, hint, tree.Rect{X: lx, Y: r.Y, W: textW, H: r.H}, AlignStart, clip)
	}

	if c.Icon.Get() != "" {
		l.iconIn(c.Icon.Get(), 24, hint, r.X+28, r.Y+r.H/2, clip)
	}
	if c.Trailing.Get() != "" {
		l.iconIn(c.Trailing.Get(), 24, hint, r.Right()-28, r.Y+r.H/2, clip)
	}

	// Caret.
	if c.Focused {
		caretX := lx - shift + fieldCaretX(l, c, labelStyle)
		caretTop := r.Y + 26
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
	l.iconIn("search", 24, hint, r.X+28, r.Y+r.H/2, clip)
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
		l.iconIn(c.Trailing.Get(), 24, hint, r.Right()-28, r.Y+r.H/2, clip)
	}
}

func (l *List) paintTooltip(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.fillRect(r, col.InverseSurface, radiusFor(m3.ShapeExtraSmall, r), 0, 0, clip)
	l.textIn(c.Label.Get(), l.style("bodySmall"), col.InverseOnSurface, r, AlignCenter, clip)
}

// ---------------------------------------------------------------- containment

func (l *List) paintCard(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	radius := m3.ShapeMedium
	switch variant(c, "elevated") {
	case "filled":
		l.fillRect(r, col.SurfaceContainerHighest, radius, 0, 0, clip)
	case "outlined":
		l.fillRect(r, col.Surface, radius, col.OutlineVariant, 1, clip)
	default: // elevated
		l.shadow(r, radius, m3.Elevation1, clip)
		l.fillRect(r, col.SurfaceContainerLow, radius, 0, 0, clip)
	}
	if c.Label.Get() != "" {
		box := tree.Rect{X: r.X + 16, Y: r.Y + 16, W: r.W - 32, H: 24}
		l.textIn(c.Label.Get(), l.style("titleMedium"), col.OnSurface, box, AlignStart, clip)
	}
	if c.Supporting.Get() != "" {
		l.textIn(c.Supporting.Get(), l.style("bodyMedium"), col.OnSurfaceVariant,
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

	x := r.X + 16
	if c.Icon.Get() != "" {
		l.iconIn(c.Icon.Get(), 24, col.OnSurfaceVariant, x+12, r.Y+r.H/2, clip)
		x += 40
	}
	textW := r.Right() - x - 16
	if c.Trailing.Get() != "" {
		textW -= 40
		l.iconIn(c.Trailing.Get(), 24, col.OnSurfaceVariant, r.Right()-28, r.Y+r.H/2, clip)
	}
	fg := disabledAlpha(c, col.OnSurface)
	if c.Supporting.Get() != "" {
		// Two line boxes centred as one block.
		top := r.Y + (r.H-44)/2
		l.textIn(c.Label.Get(), l.style("bodyLarge"), fg,
			tree.Rect{X: x, Y: top, W: textW, H: 24}, AlignStart, clip)
		l.textIn(c.Supporting.Get(), l.style("bodyMedium"), col.OnSurfaceVariant,
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
		l.iconIn(c.Icon.Get(), 24, col.OnSurfaceVariant, x+12, r.Y+r.H/2, clip)
		x += 36
	}
	textW := r.Right() - x - 12
	if c.Trailing.Get() != "" {
		textW -= 36
		l.iconIn(c.Trailing.Get(), 24, col.OnSurfaceVariant, r.Right()-24, r.Y+r.H/2, clip)
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
		l.iconIn(name, 24, col.OnSurface, r.X+28, r.Y+r.H/2, clip)
		textX = r.X + 56
	}
	l.textIn(c.Label.Get(), l.style("titleLarge"), col.OnSurface,
		tree.Rect{X: textX, Y: r.Y, W: r.Right() - textX - 64, H: r.H}, AlignStart, clip)
	if c.Trailing.Get() != "" {
		l.iconIn(c.Trailing.Get(), 24, col.OnSurface, r.Right()-28, r.Y+r.H/2, clip)
	}
}

// destination items are children; the bar paints the container and the
// active indicator behind the selected child.
func (l *List) paintNavigationBar(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.fillRect(r, col.SurfaceContainer, 0, 0, 0, clip)
	// The indicator follows the destination's own selected flag, so a nav bar, a
	// rail and a drawer all read the same state.
	for _, k := range c.Base().Kids() {
		kc := m3Of(k)
		if kc == nil || !kc.Selected.Get() {
			continue
		}
		kb := k.Bounds()
		cx := r.X + kb.X + kb.W/2
		l.fillRect(tree.Rect{X: cx - 32, Y: r.Y + 12, W: 64, H: 32}, col.SecondaryContainer, 16, 0, 0, clip)
	}
}

func (l *List) paintNavigationRail(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	// SurfaceContainer, like the bottom bar, plus a 1dp edge line. A rail on
	// plain Surface was invisible against a Surface page, and the container
	// alone is only a few units off it; the hairline is what makes the rail
	// read as its own surface in both schemes.
	l.fillRect(r, col.SurfaceContainer, 0, 0, 0, clip)
	l.fillRect(tree.Rect{X: r.Right() - 1, Y: r.Y, W: 1, H: r.H}, col.OutlineVariant, 0, 0, 0, clip)
	for _, k := range c.Base().Kids() {
		kc := m3Of(k)
		if kc == nil || !kc.Selected.Get() {
			continue
		}
		kb := k.Bounds()
		cx := r.X + kb.X + kb.W/2
		l.fillRect(tree.Rect{X: cx - 28, Y: r.Y + kb.Y + 4, W: 56, H: 32}, col.SecondaryContainer, 16, 0, 0, clip)
	}
}

// m3Of returns the component state behind a node, or nil for a layout node.
func m3Of(b *tree.NodeBase) *tree.Component {
	if c, ok := b.Node().(interface{ M3() *tree.Component }); ok {
		return c.M3()
	}
	return nil
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
	// The active destination is whichever item says so; NavigationItem draws
	// its own icon and label on top of this band.
	for _, k := range c.Base().Kids() {
		if kc := m3Of(k); kc != nil && kc.Selected.Get() {
			l.fillRect(tree.Rect{X: r.X + k.Bounds().X, Y: r.Y + k.Bounds().Y, W: k.Bounds().W, H: k.Bounds().H},
				col.SecondaryContainer, 28, 0, 0, clip)
		}
	}
}

func (l *List) paintNavigationItem(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	fg := col.OnSurfaceVariant
	if c.Selected.Get() {
		fg = col.OnSecondaryContainer
	}
	// The parent decides the arrangement, not the item width: a destination in a
	// wide navigation bar is still stacked.
	parent := ""
	if p := c.Base().Parent(); p != nil {
		parent = p.Kind()
	}
	if parent == "NavigationDrawer" {
		l.iconIn(c.Icon.Get(), 24, fg, r.X+24, r.Y+r.H/2, clip)
		l.textIn(c.Label.Get(), l.style("labelMedium"), fg,
			tree.Rect{X: r.X + 52, Y: r.Y, W: r.W - 60, H: r.H}, AlignStart, clip)
		return
	}
	// Icon sits in the 32dp indicator; the label follows underneath. The
	// indicator is at 12dp in a bar and 4dp in a rail.
	iconY, labelY := r.Y+28, r.Y+48
	if parent == "NavigationRail" {
		iconY, labelY = r.Y+20, r.Y+40
	}
	l.iconIn(c.Icon.Get(), 24, fg, r.X+r.W/2, iconY, clip)
	l.textIn(c.Label.Get(), l.style("labelMedium"), fg,
		tree.Rect{X: r.X, Y: labelY, W: r.W, H: 16}, AlignCenter, clip)
}

func (l *List) paintTabs(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.fillRect(r, col.Surface, 0, 0, 0, clip)
	// Divider under the row.
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:  [4]float32{float32(r.X), float32(r.Bottom() - 1), float32(r.W), 1},
		Color: col.SurfaceVariant, Clip: clipArr(clip),
	})
	for i, k := range c.Base().Kids() {
		if i != c.SelectedIndex || i >= len(c.Base().Kids()) {
			continue
		}
		kb := k.Bounds()
		indicator := tree.Rect{X: r.X + kb.X + 8, Y: r.Bottom() - 3, W: kb.W - 16, H: 3}
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: RoundedRect, Rect: rectArr(indicator),
			Radius: [4]float32{3, 3, 0, 0}, Color: col.Primary, Clip: clipArr(clip),
		})
	}
}

func (l *List) paintTab(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	fg := col.OnSurfaceVariant
	if c.Selected.Get() {
		fg = col.Primary
	}
	l.stateLayer(c, r, 0, fg, clip)
	box := tree.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}
	if c.Icon.Get() != "" {
		l.iconIn(c.Icon.Get(), 24, fg, r.X+r.W/2, r.Y+r.H/2-10, clip)
		l.textIn(c.Label.Get(), l.style("titleSmall"), fg,
			tree.Rect{X: r.X, Y: r.Y + 20, W: r.W, H: r.H - 20}, AlignCenter, clip)
		return
	}
	l.textIn(c.Label.Get(), l.style("titleSmall"), fg, box, AlignCenter, clip)
}

// ---------------------------------------------------------------- progress

func (l *List) paintLinearProgress(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	h := 4.0
	if r.H > 0 && r.H < 12 {
		h = r.H
	}
	tr := tree.Rect{X: r.X, Y: r.Y + (r.H-h)/2, W: r.W, H: h}
	l.fillRect(tr, col.SecondaryContainer, h/2, 0, 0, clip)
	lo, hi := c.Min.Get(), c.Max.Get()
	frac := 0.0
	if hi > lo {
		frac = (c.Value.Get() - lo) / (hi - lo)
	}
	frac = clamp01(frac)
	w := tr.W * frac
	l.fillRect(tree.Rect{X: tr.X, Y: tr.Y, W: w, H: tr.H}, col.Primary, h/2, 0, 0, clip)
}

func (l *List) paintCircularProgress(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	size := minf(r.W, r.H)
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	thickness := 4.0
	rr := size/2 - thickness/2
	track := tree.Rect{X: cx - rr, Y: cy - rr, W: rr * 2, H: rr * 2}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Arc, Rect: rectArr(track),
		Color: col.SecondaryContainer, Thickness: float32(thickness),
		StartAngle: 0, EndAngle: 2 * 3.14159265,
		Clip: clipArr(clip),
	})
	lo, hi := c.Min.Get(), c.Max.Get()
	frac := 0.0
	if hi > lo {
		frac = (c.Value.Get() - lo) / (hi - lo)
	}
	frac = clamp01(frac)
	if frac > 0 {
		l.Ops = append(l.Ops, Op{
			Kind: Fill, Shape: Arc, Rect: rectArr(track),
			Color: col.Primary, Thickness: float32(thickness),
			StartAngle: 0, EndAngle: float32(frac * 2 * 3.14159265),
			Clip: clipArr(clip),
		})
	}
}

func (l *List) paintLoadingIndicator(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	size := minf(r.W, r.H)
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	// Expressive loading indicator: a thick rounded arc that keeps a gap.
	rr := size/2 - 4
	track := tree.Rect{X: cx - rr, Y: cy - rr, W: rr * 2, H: rr * 2}
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Arc, Rect: rectArr(track),
		Color: col.Primary, Thickness: float32(size * 0.16),
		StartAngle: 0.6, EndAngle: 5.2,
		Clip: clipArr(clip),
	})
}

// ---------------------------------------------------------------- pickers

func (l *List) paintDatePicker(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.shadow(r, m3.ShapeExtraLarge, m3.Elevation3, clip)
	l.fillRect(r, col.SurfaceContainerHigh, m3.ShapeExtraLarge, 0, 0, clip)
	l.textIn(c.Label.Get(), l.style("labelMedium"), col.OnSurfaceVariant,
		tree.Rect{X: r.X + 24, Y: r.Y + 16, W: r.W - 48, H: 16}, AlignStart, clip)

	// A 7x5 month grid, with today's cell as a filled circle.
	gridY := r.Y + 56
	cellW := (r.W - 48) / 7
	cellH := 40.0
	first := 3 // Sunday-first month starting on Wednesday
	for i := 0; i < 31; i++ {
		col0 := (i + first) % 7
		row := (i + first) / 7
		if row > 5 {
			break
		}
		cx := r.X + 24 + float64(col0)*cellW + cellW/2
		cy := gridY + float64(row)*cellH + cellH/2
		if i+1 == 12 {
			l.Ops = append(l.Ops, Op{Kind: Fill, Shape: Circle,
				Rect:  [4]float32{float32(cx - 16), float32(cy - 16), 32, 32},
				Color: col.Primary, Clip: clipArr(clip)})
		}
		style := l.style("bodyMedium")
		fg := col.OnSurface
		if i+1 == 12 {
			fg = col.OnPrimary
		}
		l.Ops = append(l.Ops, Op{
			Kind: Text, Rect: [4]float32{float32(cx - 16), float32(cy - 10), 32, 20},
			Color: fg, Clip: clipArr(clip), Text: itoa(i + 1),
			FontSize: float32(style.Size), Weight: weightOf(style.Weight),
			Align: AlignCenter, TextX: float32(cx - 16),
			TextY: float32(cy + style.Size*textBaseline), BoxW: 32,
		})
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func (l *List) paintTimePicker(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.shadow(r, m3.ShapeExtraLarge, m3.Elevation3, clip)
	l.fillRect(r, col.SurfaceContainerHigh, m3.ShapeExtraLarge, 0, 0, clip)
	l.textIn("Select time", l.style("labelMedium"), col.OnSurfaceVariant,
		tree.Rect{X: r.X + 24, Y: r.Y + 16, W: r.W - 48, H: 16}, AlignStart, clip)

	// Dial: a large circle with 12 hour labels.
	dial := minf(r.W-96, r.H-120)
	cx, cy := r.X+r.W/2, r.Y+100+dial/2
	ring := tree.Rect{X: cx - dial/2, Y: cy - dial/2, W: dial, H: dial}
	l.Ops = append(l.Ops, Op{Kind: Fill, Shape: Circle, Rect: rectArr(ring),
		Color: col.SurfaceContainerHighest, Clip: clipArr(clip)})
	for i := 0; i < 12; i++ {
		ang := float64(i) * math.Pi / 6
		hx := cx + (dial/2-28)*math.Sin(ang)
		hy := cy - (dial/2-28)*math.Cos(ang)
		l.Ops = append(l.Ops, Op{
			Kind: Text, Rect: [4]float32{float32(hx - 14), float32(hy - 10), 28, 20},
			Color: col.OnSurface, Clip: clipArr(clip), Text: itoa((i+11)%12 + 1),
			FontSize: 14, Weight: 400, Align: AlignCenter,
			TextX: float32(hx - 14), TextY: float32(hy + 14*textBaseline), BoxW: 28,
		})
	}
	// Selected hour: 10 o'clock.
	ang := 10 * math.Pi / 6
	hx := cx + (dial/2-28)*math.Sin(ang)
	hy := cy - (dial/2-28)*math.Cos(ang)
	l.Ops = append(l.Ops, Op{Kind: Fill, Shape: Circle,
		Rect:  [4]float32{float32(hx - 16), float32(hy - 16), 32, 32},
		Color: col.Primary, Clip: clipArr(clip)})
	l.textIn(c.Label.Get(), l.style("displayLarge"), col.OnSurface,
		tree.Rect{X: r.X, Y: r.Bottom() - 72, W: r.W, H: 56}, AlignCenter, clip)
}

// ---------------------------------------------------------------- misc

func (l *List) paintCarousel(c *tree.Component, r tree.Rect, clip tree.Rect) {
	// Items are children, offset horizontally by ScrollX; the list clips.
	_ = c
	_ = r
	_ = clip
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
	if v := t.Weight.Get(); v > 0 {
		st.Weight = int(v)
	}
	color := t.Color.Get()
	if color == 0 {
		color = l.Theme.Colors.OnSurface
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
	var lines []text.Line
	var boxes []tree.Rect
	if t.Wrap.Get() {
		lines = text.WrapRanges(t.Text.Get(), r.W, func(run string) float64 {
			return textWidth(l, run, st)
		})
		lh := st.LineHeight
		if lh <= 0 {
			lh = st.Size * 1.2
		}
		for i := range lines {
			boxes = append(boxes, tree.Rect{X: r.X, Y: r.Y + float64(i)*lh, W: r.W, H: lh})
		}
	} else {
		lines = []text.Line{{Text: t.Text.Get(), Start: 0, End: len(t.Text.Get())}}
		boxes = []tree.Rect{r}
	}
	selA, selB := t.SelA, t.SelB
	if selA > selB {
		selA, selB = selB, selA
	}
	for i, line := range lines {
		box := boxes[i]
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
		l.textIn(line.Text, st, color, box, align, clip)
	}
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
func textWidth(l *List, s string, st m3.TextStyle) float64 {
	if s == "" {
		return 0
	}
	if l.Measure != nil {
		return l.Measure(s, st.Size, st.Weight)
	}
	return float64(len([]rune(s))) * st.Size * 0.55
}
