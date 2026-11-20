package paint

import (
	"math"

	"github.com/goi-gui/goi/internal/tree"
)

// heroFinish handles a Hero at the end of its walk. It removes the old Hero
// while its tag is in flight, and turns the new Hero's painted subtree into the
// flight copy: the same content at the interpolated box, over the page.
func (l *List) heroFinish(b *tree.NodeBase, r tree.Rect, from int) {
	h, ok := b.Node().(*tree.Hero)
	if !ok || h.Tag.Get() == "" {
		return
	}
	flight, mode, cur := b.TreeRef().HeroAt(h, b, r, l.Clock)
	switch mode {
	case tree.HeroHide:
		if from <= len(l.Ops) {
			l.Ops = l.Ops[:from]
		}
	case tree.HeroFly:
		if flight == nil || cur.W <= 0 || cur.H <= 0 || flight.FromRect.W <= 0 || flight.FromRect.H <= 0 {
			return
		}
		var ops []Op
		dst := r
		if old := l.heroCache[h.Tag.Get()]; len(old) > 0 {
			ops = append([]Op(nil), old...)
			dst = flight.FromRect
		} else if from < len(l.Ops) {
			ops = append([]Op(nil), l.Ops[from:]...)
		}
		if len(ops) == 0 {
			return
		}
		l.Ops = l.Ops[:from]
		for i := range ops {
			flyOp(&ops[i], dst, cur)
		}
		l.heroOps = append(l.heroOps, ops...)
		l.Animating = true
	case tree.HeroNormal:
		if l.heroCache == nil {
			l.heroCache = map[string][]Op{}
		}
		if from < len(l.Ops) {
			l.heroCache[h.Tag.Get()] = append([]Op(nil), l.Ops[from:]...)
		} else {
			delete(l.heroCache, h.Tag.Get())
		}
	}
}

// flyOp maps one destination op into the flight box. The renderer scales around
// a pivot, so a box-to-box transform is a scale plus the pivot that puts the
// scaled destination box at the flight box. An axis whose size does not change
// is a plain translation, baked into the geometry.
func flyOp(op *Op, dst, cur tree.Rect) {
	sx, sy := cur.W/dst.W, cur.H/dst.H
	dx, dy := float32(cur.X-dst.X), float32(cur.Y-dst.Y)
	op.Clip = [4]float32{float32(cur.X), float32(cur.Y), float32(cur.W), float32(cur.H)}
	op.ClipBox = [4]float32{}
	op.ClipRadius = 0

	scaled := math.Abs(sx-1) >= 1e-4 || math.Abs(sy-1) >= 1e-4
	if !scaled {
		op.Rect[0] += dx
		op.Rect[1] += dy
		op.TextX += dx
		op.TextY += dy
		return
	}
	var pivotX, pivotY float32
	if math.Abs(sx-1) < 1e-4 {
		sx = 1
		op.Rect[0] += dx
		op.TextX += dx
	} else {
		pivotX = float32((cur.X - sx*dst.X) / (1 - sx))
	}
	if math.Abs(sy-1) < 1e-4 {
		sy = 1
		op.Rect[1] += dy
		op.TextY += dy
	} else {
		pivotY = float32((cur.Y - sy*dst.Y) / (1 - sy))
	}
	op.Scale = [3]float32{float32(sx), float32(sy), 1}
	op.PivotX, op.PivotY = pivotX, pivotY
}
