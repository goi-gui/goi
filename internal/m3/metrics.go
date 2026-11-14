package m3

// Measurer reports the advance size of a string.
type Measurer func(s string, size float64, weight int) (w, h float64)

// Metrics is the intrinsic size and content inset of one component.
//
// FillW/FillH mean "take the space you are given" rather than hugging content;
// layout uses them for bars, sheets and dividers.
type Metrics struct {
	W, H                     float64
	FillW, FillH             bool
	Top, Right, Bottom, Left float64 // content inset for child nodes
}

// labelStyle returns the type role a component uses for its label.
func labelStyle(kind string) TextStyle {
	t := typeScale
	switch kind {
	case "Button", "Chip", "SegmentedButton", "SplitButton", "MenuItem", "Tab", "FAB", "Badge":
		if kind == "Tab" {
			return t.TitleSmall
		}
		if kind == "Badge" {
			return t.LabelSmall
		}
		return t.LabelLarge
	case "ListItem", "TextField", "SearchBar", "Checkbox", "RadioButton", "Switch":
		return t.BodyLarge
	case "Snackbar", "Tooltip":
		if kind == "Tooltip" {
			return t.BodySmall
		}
		return t.BodyMedium
	case "TopAppBar", "Dialog":
		if kind == "Dialog" {
			return t.HeadlineSmall
		}
		return t.TitleLarge
	case "Card", "NavigationDrawer", "BottomSheet", "SideSheet":
		if kind == "Card" {
			return t.TitleMedium
		}
		return t.TitleLarge
	case "DatePicker", "TimePicker", "ColorPicker":
		return t.LabelMedium
	}
	return t.BodyMedium
}

// labelWidth measures a component label.
func labelWidth(m Measurer, kind, s string) float64 {
	if m == nil || s == "" {
		return 0
	}
	st := labelStyle(kind)
	w, _ := m(s, st.Size, st.Weight)
	return w
}

// MetricsFor computes the intrinsic size and content inset of a component.
// A nil measurer falls back to an estimate, which keeps headless tests stable.
func MetricsFor(kind string, c *ComponentView, m Measurer) Metrics {
	if m == nil {
		m = func(s string, size float64, weight int) (float64, float64) {
			return float64(len([]rune(s))) * size * 0.55, size * 1.2
		}
	}
	label := c.Label
	text := c.Text
	hasIcon := c.Icon != ""
	iconGap := 8.0
	iconW := 18.0

	// w adds an icon's width and gap when the component shows one.
	withIcon := func(w float64) float64 {
		if hasIcon {
			return w + iconW + iconGap
		}
		return w
	}

	switch kind {
	case "Button":
		lw := labelWidth(m, kind, label)
		pad := 24.0
		if c.Variant == "text" {
			pad = 12
		}
		return Metrics{W: withIcon(lw) + pad*2, H: 40}
	case "IconButton":
		return Metrics{W: 40, H: 40}
	case "FAB":
		if label != "" {
			lw := labelWidth(m, kind, label)
			return Metrics{W: 20 + 24 + 12 + lw + 20, H: 56}
		}
		return Metrics{W: 56, H: 56}
	case "FABMenu", "Toolbar":
		if kind == "Toolbar" {
			// The pill is 40dp tall inside a 64dp box, so the actions get 12dp
			// of padding on every side instead of touching the rounded edge.
			return Metrics{W: 0, H: 64, Top: 12, Right: 12, Bottom: 12, Left: 12}
		}
		return Metrics{W: 56, H: 56}
	case "SplitButton":
		lw := labelWidth(m, kind, label)
		return Metrics{W: withIcon(lw) + 24 + 40 + 8, H: 40}
	case "SegmentedButton":
		lw := labelWidth(m, kind, label)
		w := lw + 24
		if c.Selected || hasIcon {
			w += 8
			if c.Selected && !hasIcon {
				w += 18
			}
		}
		return Metrics{W: maxf(w, 48), H: 40}
	case "Chip":
		lw := labelWidth(m, kind, label)
		// 16dp at each end. A leading icon — or the check a selected chip
		// shows in its place — moves the text 18dp right, and a trailing icon
		// takes the same 18dp back on the other side.
		w := lw + 32
		if hasIcon || c.Selected {
			w += 18
		}
		if c.Trailing != "" {
			w += 18
		}
		return Metrics{W: w, H: 32}
	case "Badge":
		if label == "" {
			return Metrics{W: 6, H: 6}
		}
		return Metrics{W: maxf(labelWidth(m, kind, label)+8, 16), H: 16}
	case "Checkbox":
		return Metrics{W: withLabel(18, 8, labelWidth(m, kind, label)), H: 18}
	case "RadioButton":
		return Metrics{W: withLabel(20, 8, labelWidth(m, kind, label)), H: 20}
	case "Switch":
		return Metrics{W: withLabel(52, 8, labelWidth(m, kind, label)), H: 32}
	case "Slider":
		return Metrics{W: 200, H: 44}
	case "TextField":
		_ = text
		return Metrics{W: 200, H: 56}
	case "SearchBar":
		return Metrics{W: 360, H: 56}
	case "Tooltip":
		// The bubble is an overlay, not flow: the box is exactly the child it
		// floats above, so the text does not widen anything.
		return Metrics{}
	case "Card":
		return Metrics{W: 200, H: 0, Top: 16, Right: 16, Bottom: 16, Left: 16}
	case "Dialog":
		top := 24.0
		if label != "" {
			top += 32
		}
		if c.Supporting != "" {
			top += 8
		}
		return Metrics{W: 328, Top: top, Right: 24, Bottom: 24, Left: 24}
	case "BottomSheet":
		return Metrics{W: 0, H: 0, FillW: true, Top: 72, Right: 24, Bottom: 24, Left: 24}
	case "SideSheet":
		return Metrics{W: 320, FillH: true, Top: 72, Right: 16, Bottom: 16, Left: 16}
	case "Divider":
		if c.Variant == "vertical" {
			return Metrics{W: 1, FillH: true}
		}
		return Metrics{W: 0, H: 1, FillW: true}
	case "List":
		return Metrics{W: 0, FillW: true}
	case "Audio":
		// A player is one control bar wide and one bar tall.
		return Metrics{W: 320, H: 48, FillW: true}
	case "Video":
		// 16:9 by default, so a design that only sets a width still gets a
		// sensible box.
		return Metrics{W: 320, H: 180, FillW: true}
	case "LineChart", "AreaChart", "BarChart", "PieChart", "ScatterChart", "RadarChart", "GaugeChart":
		// A chart fills the width it is given and keeps a readable height,
		// which a design overrides with an explicit one. The intrinsic width is
		// what it asks for inside a Row, where `flex` splits the space.
		return Metrics{W: 240, H: 220, FillW: true}
	case "ListItem":
		h := 56.0
		if c.Supporting != "" {
			h = 72
		}
		return Metrics{W: 0, H: h, FillW: true, Left: 16, Right: 16}
	case "Menu":
		lw := 0.0
		return Metrics{W: maxf(lw, 112) + 24, FillW: false}
	case "MenuItem":
		return Metrics{W: labelWidth(m, kind, label) + 60, H: 48}
	case "Snackbar":
		return Metrics{W: labelWidth(m, kind, label) + 48, H: 48}
	case "TopAppBar":
		return Metrics{W: 0, H: 64, FillW: true}
	case "NavigationBar":
		return Metrics{W: 0, H: 80, FillW: true}
	case "NavigationRail":
		return Metrics{W: 80, H: 0, FillH: true}
	case "NavigationDrawer":
		top := 16.0
		if label != "" {
			top += 40
		}
		return Metrics{W: 360, FillH: true, Top: top, Left: 12, Right: 12}
	case "NavigationItem":
		return Metrics{W: 0, H: 64, FillW: true, Top: 12, Bottom: 12}
	case "Tabs":
		return Metrics{W: 0, H: 48, FillW: true}
	case "LinearProgress":
		// A wavy bar is a ribbon, so it needs the room the wave's own height
		// takes; the paint pass scales it down if a design pins a smaller box.
		if c.Variant == "wavy" {
			return Metrics{W: 0, H: waveHeight(c.Thickness, 6), FillW: true}
		}
		return Metrics{W: 0, H: strokeOr(c.Thickness, 4), FillW: true}
	case "CircularProgress":
		return Metrics{W: 48, H: 48}
	case "LoadingIndicator":
		return Metrics{W: 48, H: 48}
	case "TextArea":
		// Three lines is the usual resting height for a multiline field; a
		// design that wants more sets height itself.
		return Metrics{W: 0, H: 132, FillW: true}
	case "Code":
		// Height follows the hard line breaks, since a code block does not
		// wrap: what is counted here is exactly what the painter draws.
		lines := 1
		for i := 0; i < len(text); i++ {
			if text[i] == '\n' {
				lines++
			}
		}
		return Metrics{W: 0, H: float64(lines)*20.15 + 24, FillW: true}
	case "Markdown":
		// The document's blocks are real children, so the height is their sum
		// and the width is whatever the design gives it.
		return Metrics{W: 0, H: 0, FillW: true}
	case "DatePicker":
		return Metrics{W: 328, H: 400, Top: 56, Right: 24, Bottom: 24, Left: 24}
	case "TimePicker":
		// A dial needs the clock below the fields; the input variant is only the
		// header and the actions.
		h := 420.0
		if c.Variant == "input" {
			h = 188
		}
		return Metrics{W: 328, H: h, Top: 56, Right: 24, Bottom: 24, Left: 24}
	case "ColorPicker":
		return Metrics{W: 328, H: 300, Top: 56, Right: 24, Bottom: 24, Left: 24}
	case "Carousel":
		return Metrics{W: 0, H: 200, FillW: true}
	}
	return Metrics{W: 0, H: 0}
}

// ComponentView is the subset of node state metrics need. It keeps m3 free of
// an import cycle with tree.
type ComponentView struct {
	Label, Supporting, Text, Placeholder  string
	Icon, Trailing, Variant               string
	Thickness                             float64
	Disabled, Checked, Selected, Expanded bool
	Value, Min, Max, Step                 float64
	Spacing                               float64
	CrossAlign, MainAlign                 string
}

// strokeOr is a stroke width with the component's own fallback.
func strokeOr(v, def float64) float64 {
	if v > 0 {
		return v
	}
	return def
}

// waveHeight is the box a wavy bar needs: the ribbon is the stroke plus the
// amplitude above and below it, which is three quarters of the stroke.
func waveHeight(thickness, def float64) float64 {
	return strokeOr(thickness, def) * 1.75
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// withLabel adds the gap and label width to a control's own width.
func withLabel(control, gap, label float64) float64 {
	if label <= 0 {
		return control
	}
	return control + gap + label
}
