package paint

import (
	"math"

	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// richKey identifies a wrapped RichText. version moves when the spans are
// rewritten, so a cache hit means the paragraph did not change.
type richKey struct {
	node    *tree.RichText
	version int
	width   float64
	size    float64
	weight  int
	family  string
}

// richLines wraps a RichText with the same function layout measured with, so
// the drawn lines and the reserved height agree. Each run is measured in its
// own weight, which is what makes a bold word take the room it needs.
func (l *List) richLines(r *tree.RichText, width float64, st m3.TextStyle) [][]text.InlineRun {
	key := richKey{node: r, version: r.Version(), width: width, size: st.Size, weight: st.Weight, family: l.family}
	if got, ok := l.rich[key]; ok {
		return got
	}
	spans := r.Spans()
	lines := text.WrapInline(r.Inlines(), width, func(style int, s string) float64 {
		m := st
		m.Weight = r.SpanWeight(spans[style], st.Weight)
		return textWidth(l, s, m)
	})
	if l.rich == nil || len(l.rich) >= maxWrapEntries {
		l.rich = make(map[richKey][][]text.InlineRun, maxWrapEntries)
	}
	l.rich[key] = lines
	return lines
}

// paintRichText draws one paragraph, run by run: a bold word heavier, a link in
// the primary colour and underlined, a code span on a tinted pill. The runs
// were wrapped once for the whole paragraph, so a link can cross a line break.
func (l *List) paintRichText(r *tree.RichText, box tree.Rect, clip tree.Rect) {
	st := l.style(r.Role.Get())
	if v := r.FontSize.Get(); v > 0 {
		st.Size, st.LineHeight = v, v*1.2
	}
	lh := st.LineHeight
	if lh <= 0 {
		lh = st.Size * 1.2
	}
	color := r.Color.Get()
	if color == 0 {
		color = l.Theme.Colors.OnSurface
	}
	align := alignOf(r.Align.Get())
	spans := r.Spans()
	thickness := math.Max(1, math.Round(st.Size/14))

	for i, line := range l.richLines(r, box.W, st) {
		row := tree.Rect{X: box.X, Y: box.Y + float64(i)*lh, W: box.W, H: lh}
		lineW := 0.0
		if n := len(line); n > 0 {
			lineW = line[n-1].X + line[n-1].W
		}
		left := row.X + alignOffset(row.W, lineW, align)
		for _, run := range line {
			if run.Style < 0 || run.Style >= len(spans) {
				continue
			}
			sp := spans[run.Style]
			runBox := tree.Rect{X: left + run.X, Y: row.Y, W: run.W, H: lh}
			if sp.Code {
				l.fillRect(tree.Rect{
					X: runBox.X - 2,
					Y: row.Y + (lh-st.Size*1.3)/2,
					W: run.W + 4,
					H: st.Size * 1.3,
				}, l.Theme.Colors.SurfaceContainerHighest, 3, 0, 0, clip)
			}
			runColor := sp.Color
			if sp.ColorRole != "" {
				if c, ok := l.Theme.Colors.Role(sp.ColorRole); ok {
					runColor = c
				}
			}
			if runColor == 0 {
				runColor = color
			}
			if sp.Link != "" {
				runColor = l.Theme.Colors.Primary
			}
			runStyle := st
			runStyle.Weight = r.SpanWeight(sp, st.Weight)
			before := len(l.Ops)
			l.textIn(run.Text, runStyle, runColor, runBox, AlignStart, clip)
			if sp.Italic && len(l.Ops) > before {
				l.Ops[len(l.Ops)-1].Italic = true
			}
			baseline := row.Y + lh/2 + st.Size*textBaseline
			if sp.Underline || sp.Link != "" {
				l.decorationLine(runBox.X, baseline+st.Size*0.12, run.W, thickness, "solid", runColor, clip)
			}
			if sp.Strike {
				l.decorationLine(runBox.X, baseline-st.Size*0.3, run.W, thickness, "solid", runColor, clip)
			}
		}
	}
}
