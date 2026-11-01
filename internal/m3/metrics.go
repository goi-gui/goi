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
	t := defaultType()
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
	case "DatePicker", "TimePicker":
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
		size := 40.0
		switch c.Size {
		case "sm":
			size = 32
		case "lg":
			size = 48
		}
		return Metrics{W: size, H: size}
	case "FAB":
		if label != "" {
			lw := labelWidth(m, kind, label)
			return Metrics{W: 20 + 24 + 12 + lw + 20, H: 56}
		}
		switch c.Size {
		case "sm":
			return Metrics{W: 40, H: 40}
		case "lg":
			return Metrics{W: 96, H: 96}
		}
		return Metrics{W: 56, H: 56}
	case "FABMenu", "Toolbar":
		if kind == "Toolbar" {
			return Metrics{W: 0, H: 64}
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
		return Metrics{W: labelWidth(m, kind, label) + 16, H: 24}
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
		return Metrics{W: 0, H: 1, FillW: true}
	case "List":
		return Metrics{W: 0, FillW: true}
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
		return Metrics{W: 0, H: 4, FillW: true}
	case "CircularProgress":
		return Metrics{W: 48, H: 48}
	case "LoadingIndicator":
		return Metrics{W: 48, H: 48}
	case "DatePicker":
		return Metrics{W: 328, H: 400, Top: 56, Right: 24, Bottom: 24, Left: 24}
	case "TimePicker":
		return Metrics{W: 328, H: 420, Top: 56, Right: 24, Bottom: 24, Left: 24}
	case "Carousel":
		return Metrics{W: 0, H: 200, FillW: true}
	}
	return Metrics{W: 0, H: 0}
}

// ComponentView is the subset of node state metrics need. It keeps m3 free of
// an import cycle with tree.
type ComponentView struct {
	Label, Supporting, Text, Placeholder  string
	Icon, Trailing, Variant, Size         string
	Disabled, Checked, Selected, Expanded bool
	Value, Min, Max, Step                 float64
	Spacing                               float64
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
