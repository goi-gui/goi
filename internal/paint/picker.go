package paint

import (
	"math"
	"time"

	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// The pickers are the only components whose content is laid out by the painter
// rather than by the layout engine: a date grid and a clock dial have no nodes
// per cell. Their geometry therefore lives here, and the input layer reads the
// same layout back (DateLayoutOf/TimeLayoutOf) so a click lands on the cell the
// pointer is over.
const (
	pickerPad   = 24.0
	dateGridTop = 122.0
	dateCellH   = 36.0
)

// DateLayout is where a DatePicker draws its month.
type DateLayout struct {
	Month  int       // shown month as year*12 + month-1
	Grid   tree.Rect // the 7-column day grid; day cells are Cell wide
	Cell   float64
	CellH  float64
	Days   int // days in the shown month
	First  int // column of the 1st, Monday = 0
	Picked int // day of month of the value, 0 when it is another month
	Today  int // day of month of today, 0 when it is another month
	Prev   tree.Rect
	Next   tree.Rect
}

// DateLayoutOf lays out the month of value, or shown when it is set. It is a
// pure function of the box so paint and hit testing cannot drift.
func DateLayoutOf(r tree.Rect, value string, shown int) DateLayout {
	if shown == 0 {
		shown = monthOf(value)
	}
	if shown == 0 {
		shown = monthIndex(time.Now())
	}
	y, m := shown/12, time.Month(shown%12+1)
	first := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	cell := (r.W - 2*pickerPad) / 7
	d := DateLayout{
		Month: shown,
		Grid: tree.Rect{
			X: r.X + pickerPad,
			Y: r.Y + dateGridTop,
			W: r.W - 2*pickerPad,
			H: 6 * dateCellH,
		},
		Cell:  cell,
		CellH: dateCellH,
		Days:  time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day(),
		// Monday-first, matching the weekday header.
		First: (int(first.Weekday()) + 6) % 7,
	}
	d.Next = tree.Rect{X: r.X + r.W - pickerPad - 32, Y: r.Y + 68, W: 32, H: 32}
	d.Prev = tree.Rect{X: d.Next.X - 40, Y: d.Next.Y, W: 32, H: 32}
	if t, err := time.Parse("2006-01-02", value); err == nil && monthOf(value) == shown {
		d.Picked = t.Day()
	}
	now := time.Now()
	if monthIndex(now) == shown {
		d.Today = now.Day()
	}
	return d
}

// DateAt returns the ISO date of the cell at (x, y).
func (d DateLayout) DateAt(x, y float64) (string, bool) {
	col := int((x - d.Grid.X) / d.Cell)
	row := int((y - d.Grid.Y) / d.CellH)
	if col < 0 || col > 6 || row < 0 || row > 5 {
		return "", false
	}
	day := row*7 + col - d.First + 1
	if day < 1 || day > d.Days {
		return "", false
	}
	yy, m := d.Month/12, time.Month(d.Month%12+1)
	return time.Date(yy, m, day, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), true
}

// Title names the shown month.
func (d DateLayout) Title() string {
	y, m := d.Month/12, time.Month(d.Month%12+1)
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).Format("January 2006")
}

// monthOf returns the month index of a "2006-01-02" value, or 0.
func monthOf(v string) int {
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return 0
	}
	return monthIndex(t)
}

// monthIndex maps a time to year*12 + month-1.
func monthIndex(t time.Time) int { return t.Year()*12 + int(t.Month()) - 1 }

// TimeLayout is where a TimePicker draws its header, the AM/PM switch and, in
// the dial variant, the clock. Paint and hit testing read the same layout, so a
// click always lands on the cell the pointer is over.
type TimeLayout struct {
	Value   string
	Variant string // "dial" or "input"
	Field   int    // 0 the hour, 1 the minutes
	Hour    int    // 0..23
	Minute  int
	// Format is 12 (AM/PM, the default) or 24. The value stays 24-hour either
	// way; only the display, the dial and the AM/PM switch follow it.
	Format int
	AM     bool

	Title   tree.Rect
	HourBox tree.Rect
	MinBox  tree.Rect
	// HourLabel and MinuteLabel sit under the fields in the input variant.
	HourLabel, MinuteLabel tree.Rect
	// AMBox and PMBox are empty in the 24-hour format, which has no half-day.
	AMBox, PMBox tree.Rect
	Dial         tree.Rect
	// FormatToggle is the small `12h`/`24h` switch beside the variant icon.
	FormatToggle       tree.Rect
	Switch, Cancel, OK tree.Rect
}

// Time picker geometry, in the card's own logical pixels.
const (
	timePad       = 24.0
	timeTitleTop  = 16.0
	timeHeaderTop = 44.0
	timeFieldW    = 88.0
	timeFieldH    = 64.0
	timeAMPMW     = 44.0
	timeActionH   = 32.0
)

// TimeLayoutOf lays out value, defaulting to now, with field active. variant is
// "dial" for the clock and "input" for the text fields.
func TimeLayoutOf(r tree.Rect, value string, field int, variant string, format int) TimeLayout {
	if variant != "input" {
		variant = "dial"
	}
	if format != 24 {
		format = 12
	}
	h, m := parseClock(value)
	tl := TimeLayout{
		Value: pad2(h) + ":" + pad2(m), Variant: variant, Field: field,
		Hour: h, Minute: m, Format: format, AM: h < 12,
	}
	left := r.X + timePad
	top := r.Y + timeHeaderTop
	tl.Title = tree.Rect{X: left, Y: r.Y + timeTitleTop, W: r.W - 2*timePad, H: 16}
	tl.HourBox = tree.Rect{X: left, Y: top, W: timeFieldW, H: timeFieldH}
	tl.MinBox = tree.Rect{X: left + timeFieldW + 24, Y: top, W: timeFieldW, H: timeFieldH}
	if format == 12 {
		ampmX := tl.MinBox.Right() + 12
		tl.AMBox = tree.Rect{X: ampmX, Y: top, W: timeAMPMW, H: timeFieldH / 2}
		tl.PMBox = tree.Rect{X: ampmX, Y: top + timeFieldH/2, W: timeAMPMW, H: timeFieldH / 2}
	}
	tl.HourLabel = tree.Rect{X: tl.HourBox.X, Y: tl.HourBox.Bottom() + 4, W: timeFieldW, H: 16}
	tl.MinuteLabel = tree.Rect{X: tl.MinBox.X, Y: tl.MinBox.Bottom() + 4, W: timeFieldW, H: 16}

	// The actions sit on the bottom edge; the switch is the keyboard/clock
	// toggle on the left and the two buttons on the right.
	actionY := r.Bottom() - timePad - timeActionH
	tl.Switch = tree.Rect{X: left, Y: actionY, W: 32, H: timeActionH}
	tl.FormatToggle = tree.Rect{X: tl.Switch.Right() + 8, Y: actionY, W: 44, H: timeActionH}
	tl.OK = tree.Rect{X: r.Right() - timePad - 48, Y: actionY, W: 48, H: timeActionH}
	tl.Cancel = tree.Rect{X: tl.OK.X - 8 - 72, Y: actionY, W: 72, H: timeActionH}

	if variant == "dial" {
		availW := r.W - 2*timePad - 48
		availH := actionY - 12 - (top + timeFieldH + 12)
		dial := minf(availW, availH)
		if dial < 0 {
			dial = 0
		}
		cx := r.X + r.W/2
		tl.Dial = tree.Rect{X: cx - dial/2, Y: top + timeFieldH + 12, W: dial, H: dial}
	}
	return tl
}

// parseClock reads "15:04" as 24-hour hour and minute, or now when it does not
// parse.
func parseClock(value string) (int, int) {
	t, err := time.Parse("15:04", value)
	if err != nil {
		t = time.Now()
	}
	return t.Hour(), t.Minute()
}

// DialRadius is the ring the numbers sit on, measured from the centre.
func (t TimeLayout) DialRadius() float64 { return t.Dial.W/2 - 24 }

// InnerRadius is the second ring of a 24-hour dial, which is where 13-00 live.
func (t TimeLayout) InnerRadius() float64 { return t.DialRadius() * 0.62 }

// HourText is the hour as the chosen format spells it: 07 on a 24-hour clock,
// 7 on a 12-hour one.
func (t TimeLayout) HourText() string {
	if t.Format == 24 {
		return pad2(t.Hour)
	}
	return pad12(t.Hour)
}

// DialInner reports whether the selected hour sits on the inner ring, which
// only a 24-hour dial has.
func (t TimeLayout) DialInner() bool {
	return t.Format == 24 && t.Field == 0 && (t.Hour == 0 || t.Hour >= 13)
}

// TimeAt maps a point on the dial to a value, and reports which field the next
// pick should set: the hour ring hands over to the minutes.
func (t TimeLayout) TimeAt(x, y float64) (string, int, bool) {
	if t.Dial.W <= 0 {
		return "", 0, false
	}
	cx, cy := t.Dial.X+t.Dial.W/2, t.Dial.Y+t.Dial.H/2
	dx, dy := x-cx, y-cy
	if math.Hypot(dx, dy) > t.DialRadius()+20 {
		return "", 0, false
	}
	ang := math.Atan2(dx, -dy) // 0 at twelve o'clock, clockwise
	if ang < 0 {
		ang += 2 * math.Pi
	}
	idx := int(ang/(math.Pi/6)+0.5) % 12
	if t.Field == 0 {
		if t.Format == 24 {
			// Two rings: 1-12 outside, 13-00 inside.
			if math.Hypot(dx, dy) >= (t.DialRadius()+t.InnerRadius())/2 {
				h := idx
				if h == 0 {
					h = 12
				}
				return pad2(h) + ":" + pad2(t.Minute), 1, true
			}
			h := 0
			if idx != 0 {
				h = idx + 12
			}
			return pad2(h) + ":" + pad2(t.Minute), 1, true
		}
		// A 12-hour dial: AM/PM decides which half of the day it is.
		h := idx
		if h == 0 {
			h = 12
		}
		h %= 12
		if !t.AM {
			h += 12
		}
		return pad2(h) + ":" + pad2(t.Minute), 1, true
	}
	return pad2(t.Hour) + ":" + pad2(idx*5), 0, true
}

// DialIndex is the position of the current value on the active dial.
func (t TimeLayout) DialIndex() int {
	if t.Field == 0 {
		if t.Format == 24 {
			if t.Hour == 0 {
				return 0
			}
			if t.Hour >= 13 {
				return t.Hour - 12
			}
			return t.Hour % 12
		}
		return t.Hour % 12
	}
	return t.Minute / 5
}

// DialLabel is the text at dial position i on the outer ring.
func (t TimeLayout) DialLabel(i int) string {
	if t.Field == 0 {
		if i == 0 {
			return "12"
		}
		return itoa(i)
	}
	return pad2(i * 5)
}

// DialInnerLabel is the inner-ring label of a 24-hour dial: 13-23, then 00.
func (t TimeLayout) DialInnerLabel(i int) string {
	if i == 0 {
		return "00"
	}
	return itoa(i + 12)
}

// ---------------------------------------------------------------- paint

func (l *List) paintDatePicker(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.shadow(r, m3.ShapeExtraLarge, m3.Elevation3, clip)
	l.fillRect(r, col.SurfaceContainerHigh, m3.ShapeExtraLarge, 0, 0, clip)

	d := DateLayoutOf(r, c.Text.Get(), c.PickerMonth)
	inner := tree.Rect{X: r.X + pickerPad, W: r.W - 2*pickerPad}
	l.textIn(c.Label.Get(), l.style("labelMedium"), col.OnSurfaceVariant,
		tree.Rect{X: inner.X, Y: r.Y + 16, W: inner.W, H: 16}, AlignStart, clip)
	l.textIn(dateTitle(c.Text.Get()), l.style("titleMedium"), col.OnSurface,
		tree.Rect{X: inner.X, Y: r.Y + 36, W: inner.W, H: 28}, AlignStart, clip)
	l.textIn(d.Title(), l.style("labelLarge"), col.OnSurface,
		tree.Rect{X: inner.X, Y: r.Y + 72, W: inner.W - 96, H: 24}, AlignStart, clip)
	l.iconIn("chevron_left", 20, col.OnSurfaceVariant, d.Prev.X+16, d.Prev.Y+16, clip)
	l.iconIn("chevron_right", 20, col.OnSurfaceVariant, d.Next.X+16, d.Next.Y+16, clip)

	for i, wd := range [7]string{"M", "T", "W", "T", "F", "S", "S"} {
		l.textIn(wd, l.style("labelSmall"), col.OnSurfaceVariant,
			tree.Rect{X: d.Grid.X + float64(i)*d.Cell, Y: r.Y + 102, W: d.Cell, H: 16},
			AlignCenter, clip)
	}
	day := l.style("bodyMedium")
	for n := 1; n <= d.Days; n++ {
		col0 := (n - 1 + d.First) % 7
		row := (n - 1 + d.First) / 7
		cx := d.Grid.X + float64(col0)*d.Cell + d.Cell/2
		cy := d.Grid.Y + float64(row)*d.CellH + d.CellH/2
		box := tree.Rect{X: cx - 16, Y: cy - 16, W: 32, H: 32}
		fg := col.OnSurface
		switch n {
		case d.Picked:
			l.fillRect(box, col.Primary, 16, 0, 0, clip)
			fg = col.OnPrimary
		case d.Today:
			l.fillRect(box, 0, 16, col.Primary, 1, clip)
		}
		l.textIn(itoa(n), day, fg, tree.Rect{X: cx - 16, Y: cy - 10, W: 32, H: 20}, AlignCenter, clip)
	}
}

func (l *List) paintTimePicker(c *tree.Component, r tree.Rect, clip tree.Rect) {
	col := l.Theme.Colors
	l.shadow(r, m3.ShapeExtraLarge, m3.Elevation3, clip)
	l.fillRect(r, col.SurfaceContainerHigh, m3.ShapeExtraLarge, 0, 0, clip)

	format := 12
	if tp, ok := c.Node().(*tree.TimePicker); ok && tp.HourFormat.Get() == 24 {
		format = 24
	}
	t := TimeLayoutOf(r, c.Text.Get(), c.PickerField, c.Variant.Get(), format)
	label := c.Label.Get()
	if label == "" {
		if t.Variant == "input" {
			label = "Enter time"
		} else {
			label = "Select time"
		}
	}
	l.textIn(label, l.style("labelMedium"), col.OnSurfaceVariant, t.Title, AlignStart, clip)

	big := l.style("displayMedium")
	// A dial's active field is a filled container; the input variant outlines
	// it instead, which says "type here" without a caret over the digits.
	field := func(box tree.Rect, text string, active bool) {
		fg := col.OnSurface
		if active {
			l.fillRect(box, col.PrimaryContainer, 12, 0, 0, clip)
			if t.Variant == "input" {
				l.fillRect(box, 0, 12, col.Primary, 2, clip)
			}
			fg = col.Primary
		} else {
			l.fillRect(box, col.SurfaceContainerHighest, 12, 0, 0, clip)
		}
		l.textIn(text, big, fg, box, AlignCenter, clip)
	}
	field(t.HourBox, t.HourText(), t.Field == 0)
	field(t.MinBox, pad2(t.Minute), t.Field == 1)

	colon := l.style("displayMedium")
	l.textIn(":", colon, col.OnSurface,
		tree.Rect{X: t.HourBox.Right() + 4, Y: t.HourBox.Y, W: 16, H: timeFieldH}, AlignCenter, clip)

	// AM/PM: one bordered control split in two, the chosen half filled. A
	// 24-hour clock has no half-day, so the control is absent there.
	if t.Format == 12 {
		l.fillRect(tree.Rect{X: t.AMBox.X, Y: t.AMBox.Y, W: t.AMBox.W, H: t.AMBox.H * 2},
			col.Surface, 8, col.Outline, 1, clip)
		ampm := l.style("labelMedium")
		for _, half := range []struct {
			box   tree.Rect
			text  string
			on    bool
			radii [4]float32
		}{
			{t.AMBox, "AM", t.AM, [4]float32{7, 7, 0, 0}},
			{t.PMBox, "PM", !t.AM, [4]float32{0, 0, 7, 7}},
		} {
			fg := col.OnSurface
			if half.on {
				l.Ops = append(l.Ops, Op{
					Kind: Fill, Shape: RoundedRect, Rect: rectArr(half.box),
					Radius: half.radii, Color: col.TertiaryContainer, Clip: clipArr(clip),
				})
				fg = col.OnTertiaryContainer
			}
			l.textIn(half.text, ampm, fg, half.box, AlignCenter, clip)
		}
	}

	if t.Variant == "dial" {
		l.paintTimeDial(t, clip)
	} else {
		l.textIn("Hour", l.style("labelSmall"), col.OnSurfaceVariant, t.HourLabel, AlignStart, clip)
		l.textIn("Minute", l.style("labelSmall"), col.OnSurfaceVariant, t.MinuteLabel, AlignStart, clip)
	}

	// The switch and the two actions.
	icon := "keyboard"
	if t.Variant == "input" {
		icon = "schedule"
	}
	l.iconIn(icon, 22, col.OnSurfaceVariant, t.Switch.X+t.Switch.W/2, t.Switch.Y+t.Switch.H/2, clip)
	// The format chip is both a label and the switch: it says which clock is
	// showing and flips it on click.
	l.fillRect(t.FormatToggle, col.Surface, 8, col.OutlineVariant, 1, clip)
	l.textIn(itoa(t.Format)+"h", l.style("labelSmall"), col.OnSurfaceVariant,
		t.FormatToggle, AlignCenter, clip)
	action := l.style("labelLarge")
	l.textIn("Cancel", action, col.Primary, t.Cancel, AlignCenter, clip)
	l.textIn("OK", action, col.Primary, t.OK, AlignCenter, clip)
}

// paintTimeDial draws the clock: one 12-hour ring, a hand and the selected
// number as a filled circle.
func (l *List) paintTimeDial(t TimeLayout, clip tree.Rect) {
	col := l.Theme.Colors
	dcx, dcy := t.Dial.X+t.Dial.W/2, t.Dial.Y+t.Dial.H/2
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Circle, Rect: rectArr(t.Dial),
		Color: col.SurfaceContainerHighest, Clip: clipArr(clip),
	})
	numbers := l.style("bodyLarge")
	innerNumbers := l.style("bodyMedium")
	selected := t.DialIndex()
	selRadius := t.DialRadius()
	if t.DialInner() {
		selRadius = t.InnerRadius()
	}
	// A 24-hour clock labels both rings: 1-12 outside, 13-00 inside. The minute
	// dial and a 12-hour clock have one ring.
	rings := []struct {
		radius float64
		inner  bool
	}{{t.DialRadius(), false}}
	if t.Format == 24 && t.Field == 0 {
		rings = append(rings, struct {
			radius float64
			inner  bool
		}{t.InnerRadius(), true})
	}
	for _, ring := range rings {
		size := 40.0
		style := numbers
		if ring.inner {
			size, style = 30, innerNumbers
		}
		for i := 0; i < 12; i++ {
			if i == selected && ring.inner == t.DialInner() {
				continue // its filled circle is drawn below, on top
			}
			label := t.DialLabel(i)
			if ring.inner {
				label = t.DialInnerLabel(i)
			}
			lx, ly := dialPoint(dcx, dcy, ring.radius, i)
			l.textIn(label, style, col.OnSurface,
				tree.Rect{X: lx - size/2, Y: ly - 14, W: size, H: 28}, AlignCenter, clip)
		}
	}
	sx, sy := dialPoint(dcx, dcy, selRadius, selected)
	// The hand: a 2dp rule from the centre dot to the picked number.
	dx, dy := sx-dcx, sy-dcy
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: RoundedRect,
		Rect:   [4]float32{float32(dcx), float32(dcy - 1), float32(math.Hypot(dx, dy)), 2},
		Radius: [4]float32{1, 1, 1, 1},
		Color:  col.Primary, Clip: clipArr(clip),
		Rotate: float32(math.Atan2(dy, dx)), PivotX: float32(dcx), PivotY: float32(dcy),
	})
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Circle,
		Rect:  [4]float32{float32(dcx - 3), float32(dcy - 3), 6, 6},
		Color: col.Primary, Clip: clipArr(clip),
	})
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Circle,
		Rect:  [4]float32{float32(sx - 18), float32(sy - 18), 36, 36},
		Color: col.Primary, Clip: clipArr(clip),
	})
	selLabel := t.DialLabel(selected)
	if t.DialInner() {
		selLabel = t.DialInnerLabel(selected)
	}
	l.textIn(selLabel, numbers, col.OnPrimary,
		tree.Rect{X: sx - 20, Y: sy - 14, W: 40, H: 28}, AlignCenter, clip)
}

// pad12 is the 12-hour spelling of an hour: 0 and 12 both read 12, and the
// AM/PM switch says which one it is.
func pad12(h int) string {
	v := h % 12
	if v == 0 {
		v = 12
	}
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}

// dialPoint is the centre of dial position i, i = 0 at twelve o'clock.
func dialPoint(cx, cy, radius float64, i int) (float64, float64) {
	ang := float64(i) * math.Pi / 6
	return cx + radius*math.Sin(ang), cy - radius*math.Cos(ang)
}

// dateTitle spells out the picked date for the header. An unset value reads as
// a prompt instead of an empty line.
func dateTitle(v string) string {
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return "No date"
	}
	return t.Format("Mon, Jan 2")
}

// pad2 zero-pads a two-digit number.
func pad2(v int) string {
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}

// itoa formats a small non-negative int without fmt's allocation.
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
