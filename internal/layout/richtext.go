package layout

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// richTextSize measures a RichText: one line per wrapped paragraph line, with
// each run measured in its own weight. Paint wraps with the same function and
// the same advances, so the reserved height and the drawn lines agree.
func (e *Engine) richTextSize(r *tree.RichText, availW float64) (float64, float64) {
	st := m3.RoleStyle(r.Role.Get())
	size, lh := st.Size, st.LineHeight
	if v := r.FontSize.Get(); v > 0 {
		size, lh = v, v*1.2
	}
	spans := r.Spans()
	family := r.FontFamily()
	measure := func(style int, s string) float64 {
		w, _ := e.measureFamily(s, size, r.SpanWeight(spans[style], st.Weight), family)
		return w
	}
	width := func() float64 {
		total := 0.0
		for i, s := range spans {
			total += measure(i, s.Text)
		}
		return total
	}
	if !r.Wrap.Get() {
		return width(), lh
	}
	limit := availW
	if v := r.Width(); v >= 0 {
		limit = v
	}
	if limit <= 0 {
		return width(), lh
	}
	lines := text.WrapInline(r.Inlines(), limit, measure)
	return limit, float64(len(lines)) * lh
}
