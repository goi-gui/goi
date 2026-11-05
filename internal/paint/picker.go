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

// TimeLayout is where a TimePicker draws its header and dial.
type TimeLayout struct {
	Value     string
	Field     int // 0 the hour dial, 1 the minutes dial
	Hour      int
	Minute    int
	Dial      tree.Rect
	HourBox   tree.Rect
	MinuteBox tree.Rect
}

// TimeLayoutOf lays out value, defaulting to now, with field active.
func TimeLayoutOf(r tree.Rect, value string, field int) TimeLayout {
	t, err := time.Parse("15:04", value)
	if err != nil {
		t = time.Now()
	}
	dial := minf(r.W-96, r.H-190)
	if dial < 0 {
		dial = 0
	}
	cx := r.X + r.W/2
	tl := TimeLayout{
		Value:  pad2(t.Hour()) + ":" + pad2(t.Minute()),
		Field:  field,
		Hour:   t.Hour(),
		Minute: t.Minute(),
		Dial: tree.Rect{
			X: cx - dial/2,
			Y: r.Y + 120,
			W: dial,
			H: dial,
		},
	}
	// The big time is centred on cx as one "HH:MM" run; each half is its own
	// target so a click can switch which dial is active.
	tl.HourBox = tree.Rect{X: cx - 100, Y: r.Y + 32, W: 96, H: 56}
	tl.MinuteBox = tree.Rect{X: cx + 4, Y: r.Y + 32, W: 96, H: 56}
	return tl
}

// OuterRadius and InnerRadius are the two rings of the hour dial, measured from
// the dial centre. A 24-hour clock needs both: 1-12 outside, 13-00 inside.
func (t TimeLayout) OuterRadius() float64 { return t.Dial.W/2 - 28 }

// InnerRadius is the ring for 13-00 on the hour dial.
func (t TimeLayout) InnerRadius() float64 { return t.OuterRadius() * 0.55 }

// TimeAt maps a point on the dial to a value, and reports which field the next
// pick should set: the hour dial hands over to the minutes.
//
// The hour dial is a 24-hour one, so the ring under the pointer chooses the
// half of the day: outside is 1-12, inside is 13-00 (index 0 is 00).
func (t TimeLayout) TimeAt(x, y float64) (string, int, bool) {
	if t.Dial.W <= 0 {
		return "", 0, false
	}
	cx, cy := t.Dial.X+t.Dial.W/2, t.Dial.Y+t.Dial.H/2
	dx, dy := x-cx, y-cy
	dist := math.Hypot(dx, dy)
	if dist > t.OuterRadius()+20 {
		return "", 0, false
	}
	ang := math.Atan2(dx, -dy) // 0 at twelve o'clock, clockwise
	if ang < 0 {
		ang += 2 * math.Pi
	}
	idx := int(ang/(math.Pi/6)+0.5) % 12
	if t.Field == 0 {
		// Halfway between the rings is the boundary; a click in the middle of
		// the dial is closer to the inner ring.
		mid := (t.OuterRadius() + t.InnerRadius()) / 2
		if dist >= mid {
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
	return pad2(t.Hour) + ":" + pad2(idx*5), 0, true
}

// DialIndex is the position of the current value on the active dial.
func (t TimeLayout) DialIndex() int {
	if t.Field == 0 {
		return t.Hour % 12
	}
	return t.Minute / 5
}

// DialInner reports whether the selected hour is on the inner ring, which is
// where 13-00 live. Midnight sits there too: 00 is the top of the inner ring.
func (t TimeLayout) DialInner() bool {
	return t.Field == 0 && (t.Hour == 0 || t.Hour >= 13)
}

// DialLabel is the text at dial position i.
func (t TimeLayout) DialLabel(i int) string {
	if t.Field == 0 {
		if i == 0 {
			return "12"
		}
		return itoa(i)
	}
	return pad2(i * 5)
}

// DialInnerLabel is the inner-ring label at position i: 13-23, then 00.
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

	t := TimeLayoutOf(r, c.Text.Get(), c.PickerField)
	label := c.Label.Get()
	if label == "" {
		label = "Select time"
	}
	l.textIn(label, l.style("labelMedium"), col.OnSurfaceVariant,
		tree.Rect{X: r.X + pickerPad, Y: r.Y + 16, W: r.W - 2*pickerPad, H: 16}, AlignStart, clip)

	// The header is the two halves of the time; the active one owns the dial.
	big := l.style("displayLarge")
	hourColor, minColor := col.OnSurface, col.OnSurface
	if t.Field == 0 {
		hourColor = col.Primary
	} else {
		minColor = col.Primary
	}
	cx := t.HourBox.X + t.HourBox.W + 2
	l.textIn(pad2(t.Hour), big, hourColor,
		tree.Rect{X: cx - 4 - 96, Y: r.Y + 32, W: 96, H: 56}, AlignEnd, clip)
	l.textIn(":", big, col.OnSurface,
		tree.Rect{X: cx - 12, Y: r.Y + 32, W: 24, H: 56}, AlignCenter, clip)
	l.textIn(pad2(t.Minute), big, minColor,
		tree.Rect{X: cx + 4, Y: r.Y + 32, W: 96, H: 56}, AlignStart, clip)

	dcx, dcy := t.Dial.X+t.Dial.W/2, t.Dial.Y+t.Dial.H/2
	l.Ops = append(l.Ops, Op{
		Kind: Fill, Shape: Circle, Rect: rectArr(t.Dial),
		Color: col.SurfaceContainerHighest, Clip: clipArr(clip),
	})

	outer, inner := t.OuterRadius(), t.InnerRadius()
	selected := t.DialIndex()
	numbers := l.style("bodyLarge")
	// A 24-hour picker labels both rings: 1-12 outside, 13-00 inside. The
	// minutes dial has a single ring, so the inner one is skipped there.
	rings := []struct {
		radius float64
		label  func(int) string
		inner  bool
	}{{outer, t.DialLabel, false}}
	if t.Field == 0 {
		rings = append(rings, struct {
			radius float64
			label  func(int) string
			inner  bool
		}{inner, t.DialInnerLabel, true})
	}
	for _, ring := range rings {
		size := 40.0
		if ring.inner {
			size = 34
		}
		for i := 0; i < 12; i++ {
			if i == selected && ring.inner == t.DialInner() {
				continue // its filled circle is drawn below, on top
			}
			lx, ly := dialPoint(dcx, dcy, ring.radius, i)
			l.textIn(ring.label(i), numbers, col.OnSurface,
				tree.Rect{X: lx - size/2, Y: ly - 14, W: size, H: 28}, AlignCenter, clip)
		}
	}
	ringRadius := outer
	if t.DialInner() {
		ringRadius = inner
	}
	sx, sy := dialPoint(dcx, dcy, ringRadius, selected)
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
